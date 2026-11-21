package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

type steerSDKDirectory struct{ gateway work.IngressGateway }

func (d steerSDKDirectory) ResolveGateway(context.Context, string, string) (work.IngressGateway, error) {
	return d.gateway, nil
}
func (d steerSDKDirectory) ResolveLeader(context.Context, string, string, string) (string, error) {
	return d.gateway.RoleID, nil
}
func (d steerSDKDirectory) ResolveMention(context.Context, work.IngressGateway, work.RawMention) (string, error) {
	return "", fmt.Errorf("no mentions")
}

type steerOuterRelay struct {
	send func(string, agentproto.Command) error
}

func (r *steerOuterRelay) SendCommand(instance string, command agentproto.Command) error {
	return r.send(instance, command)
}

func TestPrivateSteerAllUsesDurableNativeAcceptanceAndEffectiveReport(t *testing.T) {
	a, source := teamPickerFixture(t)
	ctx := t.Context()
	s := a.teamRuntime.store
	ingress, e := workingress.New(s, workingress.Config{Directory: steerSDKDirectory{source.Gateway}, PrivateCommands: teamPrivateCommands{app: a}})
	if e != nil {
		t.Fatal(e)
	}
	receive := func(id, text string) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"text": text})
		if _, e := ingress.Receive(ctx, work.RawIngressEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: id, Tenant: source.Tenant, ChatID: source.ChatID, ChatType: "p2p", MessageID: id, SenderType: "user", SenderIDs: source.SenderIDs, MessageType: "text", RawContent: string(raw), ReceivedAt: time.Now().UTC()}); e != nil {
			t.Fatal(e)
		}
		processed, e := ingress.ProcessPending(ctx, 16)
		if e != nil || len(processed.Failures) > 0 {
			t.Fatalf("actual SDK routing: %+v %v", processed, e)
		}
	}
	receive("original-request", "Original authorized work")
	var front work.WorkContext
	var originalInput work.Inbox
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		front, e = tx.GetContext(nav.FrontContextID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListPendingInputs(source.ScopeID, source.RoleID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			return fmt.Errorf("original inputs=%d", len(inputs))
		}
		originalInput = inputs[0]
		return tx.PutLocation(work.ExecutionLocation{ID: "steer-location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0)
	})
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, WorkerSessionID: "steer-worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "steer-carrier", OriginInstanceID: "steer-instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.PutWorker(work.WorkerSession{ID: owner.WorkerSessionID, ScopeID: front.ScopeID, ContextID: front.ID, RoleID: front.RoleID, Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: carrier.OriginInstanceID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationID: "steer-location", LocationGeneration: 1, Revision: 1}, 0)
	})
	relay := &steerOuterRelay{}
	driver, e := worktransport.New(s, relay)
	if e != nil {
		t.Fatal(e)
	}
	a.teamRuntime.driver = driver
	coordinator := workruntime.New(s, workruntime.Options{Driver: driver})
	driver.BindRegistrar(coordinator)
	if e = coordinator.RegisterCarrier(ctx, carrier); e != nil {
		t.Fatal(e)
	}
	if e = coordinator.HandleHello(ctx, agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: carrier.OriginInstanceID, Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: carrier.Incarnation}); e != nil {
		t.Fatal(e)
	}
	binding := owner
	binding.AttemptID = "steer-attempt"
	binding.RunRef = "steer-run"
	initial := work.ExecutionAttempt{ID: binding.AttemptID, ScopeID: front.ScopeID, ContextID: front.ID, RunRef: work.RunRef{WorkerID: owner.WorkerSessionID, Generation: 1, RunID: binding.RunRef}, InputIDs: []string{originalInput.ID}, CommandID: "initial-command", State: work.AttemptDispatchPending, Revision: 1}
	prompt, e := a.teamRuntime.service.BuildWorkInput(ctx, initial)
	if e != nil {
		t.Fatal(e)
	}
	command := agentproto.Command{CommandID: initial.CommandID, Kind: agentproto.CommandPromptSend, InstructionRevision: "frozen-instructions", Execution: &binding, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: prompt}}}, IssuedAt: time.Now().UTC()}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(owner.WorkerSessionID)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(command)
		body := work.NewContent("initial-native-body", front.ScopeID, "application/json", raw, time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: front.ScopeID, ContextID: front.ID, Actor: work.Actor{Kind: work.ActorRole, ID: front.RoleID}, Kind: "native.test.prompt", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: initial, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
	if e = coordinator.RegisterCommand(ctx, carrier.Incarnation, command); e != nil {
		t.Fatal(e)
	}
	sequence := uint64(0)
	event := func(v agentproto.Event) error {
		sequence++
		v.Seq = sequence
		hash, e := agentproto.RuntimeEventPayloadHash(v)
		if e != nil {
			return e
		}
		v.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: sequence, PayloadHash: hash}
		return coordinator.HandleEvent(ctx, carrier.OriginInstanceID, v)
	}
	if e = event(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: command.CommandID, RequestID: "native-start-rpc", ThreadID: "steer-session", TurnID: "steer-turn", Execution: &binding}); e != nil {
		t.Fatal(e)
	}
	receive("late-request", "Late authorized work")
	var late work.Inbox
	pickerTx(t, a, func(tx *workstore.Tx) error {
		inputs, e := tx.ListPendingInputs(front.ScopeID, front.RoleID)
		if e != nil {
			return e
		}
		if len(inputs) != 1 {
			return fmt.Errorf("late inputs=%d", len(inputs))
		}
		late = inputs[0]
		return nil
	})
	signer, e := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if e != nil {
		t.Fatal(e)
	}
	token, e := signer.Issue(workauth.Claims{Purpose: "worker", RoleID: owner.RoleID, BotGeneration: 1, ScopeID: owner.ScopeID, ContextID: owner.ContextID, WorkerID: owner.WorkerSessionID, SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	sent := false
	relay.send = func(instance string, cmd agentproto.Command) error {
		if instance != carrier.OriginInstanceID {
			return fmt.Errorf("wrong carrier")
		}
		if cmd.Kind == agentproto.CommandTurnSteer {
			sent = true
			pickerTx(t, a, func(tx *workstore.Tx) error {
				admission, e := tx.GetAttemptInputAdmissionByCommand(cmd.CommandID)
				if e != nil {
					return e
				}
				input, e := tx.GetInbox(late.ID)
				if e != nil {
					return e
				}
				if admission.State != work.InputAdmissionSending || input.State != work.InboxClaimed || input.ClaimedAttemptID != initial.ID {
					t.Fatal("native send preceded durable exact-input claim")
				}
				return nil
			})
			if _, e := a.teamRuntime.service.ReportOutcome(ctx, token, binding.RunRef, workservice.ReportOutcomeRequest{Key: "premature", ProcessedInputIDs: []string{originalInput.ID, late.ID}, Summary: "must reject before actual RPC"}); e == nil {
				return fmt.Errorf("unaccepted input was reported processed")
			}
			return event(agentproto.Event{Kind: agentproto.EventNativeTurnInputAccepted, CommandID: cmd.CommandID, RequestID: "actual-steer-rpc", ThreadID: cmd.Target.ThreadID, TurnID: cmd.Target.TurnID, Execution: cmd.Execution, Status: "accepted"})
		}
		if cmd.Kind == agentproto.CommandRuntimeInspect {
			probe := func(id, method string, result any) agentproto.RPCProbe {
				request, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": map[string]any{"threadId": "steer-session"}})
				response, _ := json.Marshal(map[string]any{"id": id, "result": result})
				return agentproto.RPCProbe{Request: request, Response: response}
			}
			result := &agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: cmd.CommandID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, Native: probe("read", "thread/read", map[string]any{"thread": map[string]any{"id": "steer-session", "status": map[string]any{"type": "idle"}}}), Continuation: probe("goal", "thread/goal/get", map[string]any{"goal": nil}), Background: agentproto.BackgroundInspection{CarrierIncarnation: carrier.Incarnation, ProcessGeneration: 1, LocationGeneration: 1, Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"external_fixture":"three domains inspected"}`)}, ObservedAt: time.Now().UTC()}
			v := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: cmd.CommandID, Execution: cmd.Execution, ThreadID: cmd.Target.ThreadID, TurnID: cmd.Target.TurnID, RuntimeInspection: result}
			if e := event(v); e != nil {
				return e
			}
			// Deliver the exact journaled result through the real transport's waiter.
			v.Seq = sequence
			hash, e := agentproto.RuntimeEventPayloadHash(v)
			if e != nil {
				return e
			}
			v.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: sequence, PayloadHash: hash}
			return driver.DeliverInspection(ctx, v)
		}
		return fmt.Errorf("unexpected native command %s", cmd.Kind)
	}
	receive("steer-command-source", "/steerall")
	if n, e := ingress.DispatchPrivateCommands(ctx, 16); e != nil || n != 1 {
		t.Fatalf("actual private dispatcher: %d %v", n, e)
	}
	if !sent {
		t.Fatal("private steer command did not dispatch actual native request")
	}
	if _, e = a.teamRuntime.service.ReportOutcome(ctx, token, binding.RunRef, workservice.ReportOutcomeRequest{Key: "accepted-outcome", ProcessedInputIDs: []string{originalInput.ID, late.ID}, Summary: "both explicitly handled", WorkDisposition: "complete"}); e != nil {
		t.Fatal(e)
	}
	if e = event(agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: command.CommandID, RequestID: "native-start-rpc", ThreadID: "steer-session", TurnID: "steer-turn", Execution: &binding, Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}); e != nil {
		t.Fatal(e)
	}
	if e = coordinator.Reconcile(ctx, initial.ID); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		saved, e := tx.GetAttempt(initial.ID)
		if e != nil {
			return e
		}
		if len(saved.InputIDs) != 1 || saved.InputIDs[0] != originalInput.ID {
			t.Fatal("steer rewrote frozen initial prompt")
		}
		for _, id := range []string{originalInput.ID, late.ID} {
			input, e := tx.GetInbox(id)
			if e != nil {
				return e
			}
			if input.State != work.InboxProcessed {
				t.Fatalf("explicitly reported input not processed: %s", id)
			}
		}
		return nil
	})
}
