package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestRetiredControlContextRebuildUsesExactCommittedReceipt(t *testing.T) {
	for _, kind := range []string{"exact-gate", "legacy-empty-gate", "still-alive", "late-native-write", "multi-turn-carrier"} {
		t.Run(kind, func(t *testing.T) {
			var pipes []*retiredNativePipe
			h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) {
				pipe := startRetiredNativePipe(t)
				pipes = append(pipes, pipe)
				return pipe.cmd.Process.Pid, nil
			})
			settings := h.config.Settings
			h.config.Settings = settingsFunc(func(ctx context.Context, scope work.WorkScope, role work.Role, wc work.WorkContext) (LaunchSettings, error) {
				value, err := settings.ResolveLaunch(ctx, scope, role, wc)
				value.Backend = work.BackendCodex
				value.CodexResume = &agentproto.CodexResumePolicy{ModelProviderID: "openai"}
				if wc.Purpose == work.PurposeControl {
					value.Location.IsolatedContextID = wc.ID
				}
				return value, err
			})
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				if err := tx.PutContext(work.WorkContext{ID: "business-front", ScopeID: "scope", RoleID: "role", Workstream: "business", Revision: 1}, 0); err != nil {
					return err
				}
				context := work.WorkContext{ID: "management-front", ScopeID: "scope", RoleID: "role", Workstream: "management", Purpose: work.PurposeControl, ControlTargetContextID: "business-front", Revision: 1}
				if kind == "multi-turn-carrier" {
					context.Purpose, context.ControlTargetContextID = work.PurposeBusiness, ""
				}
				return tx.PutContext(context, 0)
			}); err != nil {
				t.Fatal(err)
			}
			driver := &retiredRebuildDriver{store: store}
			h.config.Runtime = driver
			stops := 0
			h.config.StopCarrier = func(ctx context.Context, worker work.WorkerSession) error {
				stops++
				var process agentproto.RuntimeProcessIdentity
				if err := store.Within(ctx, func(tx *workstore.Tx) error {
					var err error
					process, err = tx.GetCarrierProcess(worker.CarrierIncarnation)
					return err
				}); err != nil {
					return err
				}
				return hostinventory.WaitProcessExit(ctx, hostinventory.ProcessIdentity{PID: process.PID, Platform: process.Platform, StartIdentity: process.StartIdentity})
			}
			need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "management-front"}
			if err := h.Prepare(t.Context(), need); err != nil {
				t.Fatal(err)
			}
			bindJournal := func() work.WorkerSession {
				t.Helper()
				worker := readyLifecycleCandidate(t, store, need.ContextID).Worker
				var record LaunchRecord
				if err := store.Within(t.Context(), func(tx *workstore.Tx) error { var err error; record, err = h.readLaunch(tx, worker); return err }); err != nil {
					t.Fatal(err)
				}
				journal, err := runtimejournal.Open(t.Context(), record.Snapshot.JournalPath, h.config.Paths.StateDir, record.Snapshot.CWD, record.Snapshot.CarrierIncarnation)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { journal.Close() })
				if err := journal.BindLaunchSnapshot(record.Snapshot); err != nil {
					t.Fatal(err)
				}
				driver.journal, driver.pipe, driver.offline = journal, pipes[len(pipes)-1], false
				identity, err := hostinventory.CaptureProcess(driver.pipe.cmd.Process.Pid)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
					return tx.PutCarrierProcess(worker.CarrierIncarnation, worker.InstanceID, agentproto.RuntimeProcessIdentity{HostID: "local", PID: identity.PID, Platform: identity.Platform, StartIdentity: identity.StartIdentity})
				}); err != nil {
					t.Fatal(err)
				}
				return worker
			}
			old := bindJournal()
			coordinator := workruntime.New(store, workruntime.Options{Driver: driver, StateDir: h.config.Paths.StateDir, HostID: "local"})
			enqueue := func(number int) {
				t.Helper()
				if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
					id := fmt.Sprintf("human-management-%d", number)
					body := work.NewContent(id+":body", "scope", "text/plain", []byte("please inspect the current work status"), time.Now().UTC())
					if err := tx.PutContent(body); err != nil {
						return err
					}
					message := work.Message{ID: id, ScopeID: "scope", ConversationID: "private-chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: id}, Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: time.Now().UTC()}}, BodyRef: body.ID, Revision: 1, CreatedAt: time.Now().UTC()}
					original := work.Inbox{ID: id + ":original", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "business-front", MessageID: id, EventID: id, State: work.InboxPending, Revision: 1}
					copy := original
					copy.ID = id + ":control"
					copy.RecipientContextID = need.ContextID
					copy.ControlOfInputID = original.ID
					if kind == "multi-turn-carrier" {
						copy.ControlOfInputID = ""
					}
					copy.EventID = id + ":control"
					_, _, err := tx.AcceptMessage(message, []work.Inbox{original, copy})
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			var latestAttempt string
			dispatcher := lifecycleDispatcher{send: func(ctx context.Context, instance string, command agentproto.Command) error {
				if command.Execution.ContextID != need.ContextID {
					return errors.New("management input escaped its original context")
				}
				var worker work.WorkerSession
				if err := store.Within(ctx, func(tx *workstore.Tx) error {
					var err error
					worker, err = tx.GetWorker(command.Execution.WorkerSessionID)
					return err
				}); err != nil {
					return err
				}
				if err := coordinator.RegisterCommand(ctx, worker.CarrierIncarnation, command); err != nil {
					return err
				}
				if _, _, err := driver.journal.PrepareCommand(command); err != nil {
					return err
				}
				call := func(method string) (json.RawMessage, error) {
					frame, _ := json.Marshal(map[string]any{"id": command.CommandID + ":" + method, "method": method, "params": map[string]any{"threadId": worker.NativeSessionID}})
					if _, err := driver.journal.PrepareFrame(command.CommandID, frame); err != nil {
						return nil, err
					}
					return driver.pipe.call(frame)
				}
				method := "thread/start"
				if command.Target.EffectivePromptExecutionMode() == agentproto.PromptExecutionModeResumeExisting {
					method = "thread/resume"
				}
				if _, err := call(method); err != nil {
					return err
				}
				response, err := call("turn/start")
				if err != nil {
					return err
				}
				var native struct {
					Result struct {
						Turn struct{ ID, Status, Text string } `json:"turn"`
					} `json:"result"`
				}
				if err := json.Unmarshal(response, &native); err != nil {
					return err
				}
				sendEvent := func(event agentproto.Event) error {
					event.CommandID, event.ThreadID, event.TurnID = command.CommandID, "native-history", native.Result.Turn.ID
					stable, err := driver.journal.PersistEvents([]agentproto.Event{event})
					if err != nil {
						return err
					}
					return coordinator.HandleEvent(ctx, instance, stable[0])
				}
				if err := sendEvent(agentproto.Event{Kind: agentproto.EventTurnStarted}); err != nil {
					return err
				}
				if err := store.Within(ctx, func(tx *workstore.Tx) error {
					attempt, err := tx.GetAttempt(command.Execution.AttemptID)
					if err != nil {
						return err
					}
					latestAttempt = attempt.ID
					body := work.NewContent(command.CommandID+":answer", attempt.ScopeID, "text/plain", []byte(native.Result.Turn.Text), time.Now().UTC())
					if err := tx.PutContent(body); err != nil {
						return err
					}
					return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: attempt.ID, RunRef: attempt.RunRef, ProcessedInputIDs: attempt.InputIDs, OutcomeRef: body.ID, Revision: 1}, 0)
				}); err != nil {
					return err
				}
				return sendEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, Status: native.Result.Turn.Status, TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime})
			}}
			engine, err := workengine.New(store, workengine.Config{MaxConcurrent: 1}, h, dispatcher)
			if err != nil {
				t.Fatal(err)
			}
			dispatch := func() {
				t.Helper()
				result, err := engine.Tick(t.Context())
				if err != nil || len(result.Dispatched) != 1 || result.Dispatched[0].Err != nil {
					t.Fatalf("real temporary management pipeline failed: %+v %v", result, err)
				}
			}
			// Two settled turns use one carrier/session. Only the receipt after the last
			// effectful canonical command may prove its current stopped boundary.
			enqueue(1)
			dispatch()
			firstAttempt := latestAttempt
			if kind == "multi-turn-carrier" {
				enqueue(2)
				dispatch()
				if latestAttempt == firstAttempt {
					t.Fatal("fixture did not create two distinct settled attempts")
				}
			}
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				worker, err := tx.GetWorker(old.ID)
				if err != nil {
					return err
				}
				selected, err := tx.CarrierQuiescenceAttempt(worker)
				if err != nil {
					return err
				}
				if selected != latestAttempt {
					t.Fatalf("receipt chosen by historical activity instead of canonical fence: %s != %s", selected, latestAttempt)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			enqueue(3)
			if kind == "late-native-write" {
				var command agentproto.Command
				if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
					attempt, err := tx.GetAttempt(latestAttempt)
					if err != nil {
						return err
					}
					command, err = tx.NativeCommandPayload(attempt.CommandID, old.CarrierIncarnation)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				command.CommandID = "late-unverified-native-write"
				if _, _, err := driver.journal.PrepareCommand(command); err != nil {
					t.Fatal(err)
				}
				if _, err := driver.journal.PrepareFrame(command.CommandID, []byte(`{"id":"late","method":"turn/start","params":{}}`)); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "still-alive" {
				if err := driver.pipe.input.Close(); err != nil {
					t.Fatal(err)
				}
				if err := driver.pipe.cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			if err := driver.journal.Close(); err != nil {
				t.Fatal(err)
			}
			driver.offline = true
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				worker, err := tx.GetWorker(old.ID)
				if err != nil {
					return err
				}
				before := worker.Revision
				worker.Revision++
				worker.State = work.WorkerRecovering
				if err := tx.PutWorker(worker, before); err != nil {
					return err
				}
				if err := tx.SetCarrierConnected(worker.CarrierIncarnation, false); err != nil {
					return err
				}
				attempt := ""
				if kind != "legacy-empty-gate" {
					attempt = latestAttempt
				}
				return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attempt, Reason: "snapshot_rebuild_required", CreatedAt: time.Now().UTC()})
			}); err != nil {
				t.Fatal(err)
			}
			beforeInspections := driver.inspections
			err = h.Prepare(t.Context(), need)
			if driver.inspections != beforeInspections {
				t.Fatal("disconnected carrier received an unnecessary native RPC")
			}
			if kind == "still-alive" || kind == "late-native-write" {
				if err == nil || len(pipes) != 1 || stops != 0 {
					t.Fatalf("unsafe carrier rebuilt: err=%v starts=%d stops=%d", err, len(pipes), stops)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(pipes) != 2 || stops != 1 {
				t.Fatalf("exact retired carrier did not rebuild once: starts=%d stops=%d", len(pipes), stops)
			}
			current := bindJournal()
			if current.ContextID != old.ContextID || current.Generation != old.Generation+1 || current.CarrierIncarnation == old.CarrierIncarnation {
				t.Fatalf("rebuild retargeted the private management context: %+v", current)
			}
			dispatch()
			if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
				input, err := tx.GetInbox("human-management-3:control")
				if err != nil {
					return err
				}
				if input.State != work.InboxProcessed {
					t.Fatalf("next real human management input remained blocked: %+v", input)
				}
				original, err := tx.GetInbox("human-management-3:original")
				if err != nil {
					return err
				}
				if original.State != work.InboxPending {
					t.Fatal("management rebuild consumed original business input")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
