package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type privateMCPReadFixture struct {
	a           *App
	source      teamHumanSource
	coordinator *workruntime.Coordinator
	relay       *steerOuterRelay
	carrier     workstore.CarrierRegistration
	commands    []agentproto.Command
	sequence    uint64
}

func newPrivateMCPReadFixture(t *testing.T, appIDs ...string) *privateMCPReadFixture {
	t.Helper()
	a, source := teamPickerFixture(t, appIDs...)
	f := &privateMCPReadFixture{a: a, source: source, relay: &steerOuterRelay{}}
	var front work.WorkContext
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		front, err = tx.GetContext(nav.FrontContextID)
		if err != nil {
			return err
		}
		return tx.PutLocation(work.ExecutionLocation{ID: "mcp-location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0)
	})
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, WorkerSessionID: "mcp-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	f.carrier = workstore.CarrierRegistration{Incarnation: "mcp-carrier", OriginInstanceID: "mcp-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: f.carrier.OriginInstanceID, CarrierIncarnation: f.carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "mcp-location", LocationGeneration: 1, NativeSessionID: "mcp-session", NativeNamespace: "codex", Revision: 1}, 0)
	})
	driver, err := worktransport.New(a.teamRuntime.store, f.relay, a.teamRuntime.beginPersonalSourceActivity)
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.driver = driver
	f.coordinator = workruntime.New(a.teamRuntime.store, workruntime.Options{Driver: driver})
	driver.BindRegistrar(f.coordinator)
	if err = f.coordinator.RegisterCarrier(t.Context(), f.carrier); err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.HandleHello(t.Context(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: f.carrier.OriginInstanceID, Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: f.carrier.Incarnation}); err != nil {
		t.Fatal(err)
	}
	f.relay.send = func(_ string, command agentproto.Command) error { f.commands = append(f.commands, command); return nil }
	return f
}

func (f *privateMCPReadFixture) queue(t *testing.T, published ...bool) string {
	t.Helper()
	id, err := f.a.handleTeamMCP(t.Context(), f.source, control.Action{Kind: control.ActionMCPOAuthCommand, Text: "/mcp status"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 0 {
		t.Fatal("MCP read escaped durable queue onto command ingress")
	}
	if len(published) > 0 && !published[0] {
		return id
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		view.ExternalMessageID = "actual-mcp-card"
		view.Revision++
		if err = tx.PutUIView(view, view.Revision-1); err != nil {
			return err
		}
		op, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationSending
		if _, _, err = tx.PutOperation(op, before); err != nil {
			return err
		}
		before = op.Revision
		op.Revision++
		op.State = work.OperationConfirmed
		op.ExternalReceipt = view.ExternalMessageID
		_, _, err = tx.PutOperation(op, before)
		return err
	})
	return id
}

func TestPrivateMCPReadPendingPublicationCannotStarveNewerRead(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	f.queue(t, false)
	now := time.Now().UTC()
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		original, err := tx.GetOperation(teamID("private-mcp-read", f.source.Key))
		if err != nil {
			return err
		}
		for i := range 32 {
			old := original
			old.ID = fmt.Sprintf("old-unpublished-mcp-%02d", i)
			old.IdempotencyKey = old.ID
			old.State = work.OperationSending
			old.Steps = append(append([]work.OperationStep(nil), original.Steps...), work.OperationStep{ID: "dispatch_started", State: work.OperationConfirmed, Receipt: now.Add(-time.Minute).Format(time.RFC3339Nano)})
			old.CreatedAt, old.UpdatedAt = now.Add(-time.Hour), now.Add(-time.Hour)
			if _, _, err = tx.PutOperation(old, 0); err != nil {
				return err
			}
		}
		return nil
	})
	if err := f.a.advanceTeamMCPReads(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 0 {
		t.Fatal("old publication wait incorrectly sent another native read")
	}
	if err := f.a.advanceTeamMCPReads(t.Context(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 || f.commands[0].CommandID != teamID("private-mcp-read", f.source.Key) {
		t.Fatal("32 old unconfirmed menu publications starved a new MCP read")
	}
}

func TestPrivateMCPReadRejectsSameGenerationSessionOrCarrierChange(t *testing.T) {
	op := work.Operation{TargetID: "worker", TargetGeneration: 1, ContextID: "context"}
	worker := work.WorkerSession{ID: "worker", Generation: 1, ContextID: "context", State: work.WorkerReady, NativeSessionID: "original-session", CarrierIncarnation: "original-carrier"}
	command := agentproto.Command{Target: agentproto.Target{ThreadID: "original-session"}}
	if !teamMCPReadOwnsSession(op, worker, command, "original-carrier") {
		t.Fatal("fixture does not own original session")
	}
	worker.NativeSessionID = "different-session"
	if teamMCPReadOwnsSession(op, worker, command, "original-carrier") {
		t.Fatal("same-generation session change overwrote original MCP result")
	}
	worker.NativeSessionID, worker.CarrierIncarnation = "original-session", "different-carrier"
	if teamMCPReadOwnsSession(op, worker, command, "original-carrier") {
		t.Fatal("same-generation carrier change overwrote original MCP result")
	}
}

func (f *privateMCPReadFixture) respond(t *testing.T) {
	t.Helper()
	if len(f.commands) != 1 {
		t.Fatalf("commands=%d", len(f.commands))
	}
	command := f.commands[0]
	f.sequence++
	event := agentproto.Event{Kind: agentproto.EventRuntimeMCPResult, CommandID: command.CommandID, RequestID: "actual-mcp-rpc", Execution: agentproto.CloneExecutionBinding(command.Execution), ThreadID: command.Target.ThreadID, RuntimeMCP: &agentproto.RuntimeMCPResult{Version: 1, Mode: "status", Supported: true, Servers: []agentproto.RuntimeMCPServer{{Name: "actual-native-docs", Status: "connected"}}}, Seq: f.sequence}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: f.carrier.Incarnation, Sequence: f.sequence, PayloadHash: hash}
	if err = f.coordinator.HandleEvent(t.Context(), "wrong-instance", event); err == nil {
		t.Fatal("MCP result accepted another transport")
	}
	if err = f.coordinator.HandleEvent(t.Context(), f.carrier.OriginInstanceID, event); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateMCPReadDoesNotBlockIngressAndReplacesOnlyOwnedPendingCard(t *testing.T) {
	f := newPrivateMCPReadFixture(t)
	pending := f.queue(t)
	now := time.Now().UTC()
	if err := f.a.advanceTeamMCPReads(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	if len(f.commands) != 1 {
		pickerTx(t, f.a, func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(teamID("private-mcp-read", f.source.Key))
			t.Errorf("read not sent from native lane: %+v", op)
			return err
		})
		t.FailNow()
	}
	// No reply has arrived. A full scheduler pass still returns immediately.
	done := make(chan error, 1)
	go func() { done <- f.a.advanceTeamMCPReads(context.Background(), now.Add(time.Second)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native command lane blocked waiting for MCP RPC")
	}
	f.respond(t)
	if err := f.a.advanceTeamMCPReads(t.Context(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	id := teamID("private-mcp-read", f.source.Key)
	result := goalUICardBody(t, f.a, f.source.ScopeID, workuiMCPResultID(id))
	if !strings.Contains(result, "actual-native-docs") {
		t.Fatalf("actual MCP response missing: %s", result)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(workuiMCPResultID(id))
		if view.ReplacesViewID != pending {
			t.Fatal("MCP result did not replace pending card")
		}
		return err
	})
}

func workuiMCPResultID(id string) string { return workui.ID(id, "result") }

func TestPrivateMCPReadFailureTimeoutAndNewCommandNeverOverwriteNewerMenu(t *testing.T) {
	for _, mode := range []string{"timeout", "send-error", "new-command", "worker-rebuild"} {
		t.Run(mode, func(t *testing.T) {
			f := newPrivateMCPReadFixture(t)
			pending := f.queue(t)
			now := time.Now().UTC()
			if mode == "send-error" {
				f.relay.send = func(string, agentproto.Command) error { return fmt.Errorf("fixture transport unavailable") }
			}
			if err := f.a.advanceTeamMCPReads(t.Context(), now); err != nil {
				t.Fatal(err)
			}
			if mode == "new-command" {
				source := f.source
				source.Key = "newer-status-command"
				if _, err := f.a.queuePrivatePanel(t.Context(), source, "status", ""); err != nil {
					t.Fatal(err)
				}
				f.respond(t)
			}
			if mode == "worker-rebuild" {
				f.respond(t)
				pickerTx(t, f.a, func(tx *workstore.Tx) error {
					worker, err := tx.GetWorker("mcp-worker")
					if err != nil {
						return err
					}
					previous := worker.Revision
					worker.Revision++
					worker.Generation++
					worker.NativeSessionID = "replacement-native-session"
					return tx.PutWorker(worker, previous)
				})
			}
			if err := f.a.advanceTeamMCPReads(t.Context(), now.Add(31*time.Second)); err != nil {
				t.Fatal(err)
			}
			id := teamID("private-mcp-read", f.source.Key)
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				op, err := tx.GetOperation(id)
				if err != nil {
					return err
				}
				seen := false
				for _, step := range op.Steps {
					if step.ID == "ui_result" {
						seen = true
						if (mode == "new-command" || mode == "worker-rebuild") && step.Receipt != "stale" {
							t.Fatal("late MCP reply overwrote newer command")
						}
					}
				}
				if !seen {
					t.Fatal("MCP timeout/failure never produced terminal UI feedback")
				}
				if mode == "new-command" || mode == "worker-rebuild" {
					if _, err = tx.GetUIView(workuiMCPResultID(id)); err != workstore.ErrNotFound {
						t.Fatal("stale result created replacement")
					}
				}
				if mode != "new-command" && mode != "worker-rebuild" {
					view, err := tx.GetUIView(workuiMCPResultID(id))
					if err != nil {
						return err
					}
					if view.ReplacesViewID != pending {
						t.Fatal("timeout feedback lost current menu")
					}
				}
				return nil
			})
		})
	}
}
