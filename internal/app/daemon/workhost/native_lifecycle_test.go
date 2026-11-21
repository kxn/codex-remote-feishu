package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestBackendSwitchRetiresOldOwnerAndUsesContextCheckpoint(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 1234, nil })
	h.config.Runtime = &rebuildDriver{stopped: true}
	stopAllowed := false
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error {
		if !stopAllowed {
			return errors.New("exact wrapper exit not observed")
		}
		return nil
	}
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	old := seedIdleSession(t, h, store)
	err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		wc, err := tx.GetContext(old.ContextID)
		if err != nil {
			return err
		}
		wc.Checkpoint = "durable pending design decision"
		wc.Revision++
		return tx.PutContext(wc, wc.Revision-1)
	})
	if err != nil {
		t.Fatal(err)
	}
	settings := h.config.Settings
	h.config.Settings = settingsFunc(func(ctx context.Context, s work.WorkScope, r work.Role, wc work.WorkContext) (LaunchSettings, error) {
		v, err := settings.ResolveLaunch(ctx, s, r, wc)
		v.Backend = work.BackendCodex
		v.CodexResume = &agentproto.CodexResumePolicy{ModelProviderID: "openai"}
		return v, err
	})
	if err := h.Prepare(t.Context(), need); err == nil {
		t.Fatal("backend switched before verified old process exit")
	}
	stopAllowed = true
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	current := readyLifecycleCandidate(t, store, old.ContextID)
	if current.Worker.ID == old.ID || current.Worker.Backend != work.BackendCodex || current.Worker.NativeSessionID != "" {
		t.Fatalf("invalid backend replacement: %+v", current.Worker)
	}
	plan, err := h.Plan(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command.Target.EffectivePromptExecutionMode() != agentproto.PromptExecutionModeStartNew || !strings.Contains(plan.Command.Prompt.Inputs[0].Text, "durable pending design decision") {
		t.Fatalf("replacement start lacks checkpoint: %+v", plan.Command)
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker(old.ID)
		if err != nil {
			return err
		}
		carrier, err := tx.GetCarrier(old.CarrierIncarnation)
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRetired || worker.Backend != work.BackendClaude || worker.NativeSessionID != old.NativeSessionID || carrier.Matches(worker) {
			t.Fatalf("old native identity was not fenced and retained: %+v", worker)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPoolEvictionRestoresExactNativeHistoryAfterHostRestart(t *testing.T) {
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { return 1234, nil })
	h.config.MaxLiveCarriers = 2
	h.config.ControlReservedCarriers = 1
	h.config.Runtime = &rebuildDriver{stopped: true}
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error { return nil }
	seedPoolContexts(t, store, "other")
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	old := seedIdleSession(t, h, store)
	if checkpoint := readLifecycleCandidate(t, store, old.ContextID).Context.Checkpoint; !strings.Contains(checkpoint, "completed original prompt") {
		t.Fatalf("empty report did not create a recovery fallback from durable result: %q", checkpoint)
	}
	if err := h.Prepare(t.Context(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "other"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		other, err := tx.CurrentContextWorker("other")
		if err != nil {
			return err
		}
		other.State = work.WorkerReady
		other.Revision++
		return tx.PutWorker(other, other.Revision-1)
	}); err != nil {
		t.Fatal(err)
	}

	restarted, err := New(store, h.service, h.signer, h.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	current := readyLifecycleCandidate(t, store, old.ContextID)
	if current.Worker.ID != old.ID || current.Worker.Generation != old.Generation+1 || current.Worker.CarrierIncarnation == old.CarrierIncarnation {
		t.Fatalf("pool did not replace the carrier: %+v", current.Worker)
	}
	plan, err := restarted.Plan(t.Context(), current)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Command.Target.EffectivePromptExecutionMode() != agentproto.PromptExecutionModeResumeExisting || plan.Command.Target.ThreadID != old.NativeSessionID {
		t.Fatalf("pool replacement forgot the exact original history: %+v", plan.Command.Target)
	}
	assertMissingPoolResumeRebuildsOnce(t, restarted, store, current, need)
}

// Exercise the real scheduler claim and the Coordinator's native binding and
// outcome receipt paths. The driver supplies only the deterministic native
// turn and quiescence frames.
func seedIdleSession(t *testing.T, h *Host, store *workstore.Store) work.WorkerSession {
	t.Helper()
	active := readyLifecycleCandidate(t, store, hostWorker(t, store).ContextID).Worker
	coordinator := workruntime.New(store, workruntime.Options{Driver: h.config.Runtime})
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutInbox(work.Inbox{ID: "native-history-input", ScopeID: active.ScopeID, RecipientRoleID: active.RoleID, RecipientContextID: active.ContextID, EventID: "native-history-event", State: work.InboxPending, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	dispatcher := lifecycleDispatcher{func(ctx context.Context, instance string, command agentproto.Command) error {
		if err := coordinator.RegisterCommand(ctx, active.CarrierIncarnation, command); err != nil {
			return err
		}
		sendEvent := func(event agentproto.Event, seq uint64) error {
			event.CommandID, event.ThreadID, event.TurnID = command.CommandID, "native-history", "completed-turn"
			event.Seq = seq
			hash, err := agentproto.RuntimeEventPayloadHash(event)
			if err != nil {
				return err
			}
			event.Journal = &agentproto.RuntimeEventIdentity{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: active.CarrierIncarnation, Sequence: seq, PayloadHash: hash}
			return coordinator.HandleEvent(ctx, instance, event)
		}
		if err := sendEvent(agentproto.Event{Kind: agentproto.EventTurnStarted}, 1); err != nil {
			return err
		}
		if err := store.Within(ctx, func(tx *workstore.Tx) error {
			body := work.NewContent("native-history-result", active.ScopeID, "text/plain", []byte("completed original prompt"), time.Now().UTC())
			if err := tx.PutContent(body); err != nil {
				return err
			}
			return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: command.Execution.AttemptID,
				RunRef:            work.RunRef{WorkerID: active.ID, Generation: active.Generation, RunID: command.Execution.RunRef},
				ProcessedInputIDs: []string{"native-history-input"}, OutcomeRef: body.ID, Revision: 1}, 0)
		}); err != nil {
			return err
		}
		return sendEvent(agentproto.Event{Kind: agentproto.EventTurnCompleted, Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime}, 2)
	}}
	engine, err := workengine.New(store, workengine.Config{MaxConcurrent: 1}, h, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	tick, err := engine.Tick(t.Context())
	if err != nil || len(tick.Dispatched) != 1 || tick.Dispatched[0].Err != nil {
		t.Fatalf("failed to bind native session through normal dispatch: %+v, %v", tick, err)
	}
	worker := hostWorker(t, store)
	if worker.State != work.WorkerReady || worker.NativeSessionID != "native-history" {
		t.Fatalf("fixture did not reach a confirmed native idle boundary: %+v", worker)
	}
	return worker
}

type lifecycleDispatcher struct {
	send func(context.Context, string, agentproto.Command) error
}

func (d lifecycleDispatcher) Send(ctx context.Context, instance string, command agentproto.Command) (workengine.DispatchReceipt, error) {
	err := d.send(ctx, instance, command)
	return workengine.DispatchReceipt{State: workengine.DispatchDelivered, TransportReceipt: command.CommandID}, err
}

func readyLifecycleCandidate(t *testing.T, store *workstore.Store, id string) workengine.Candidate {
	t.Helper()
	worker := readLifecycleCandidate(t, store, id).Worker
	coordinator := workruntime.New(store, workruntime.Options{})
	hello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: worker.InstanceID, Backend: agentproto.Backend(worker.Backend), Managed: true, Source: "headless"}, WorkerProtocolVersion: agentproto.WorkerProtocolVersion, JournalProtocolVersion: agentproto.JournalProtocolVersion, CarrierIncarnation: worker.CarrierIncarnation}
	if err := coordinator.HandleHello(t.Context(), hello); err != nil {
		t.Fatal(err)
	}
	return readLifecycleCandidate(t, store, id)
}

func readLifecycleCandidate(t *testing.T, store *workstore.Store, contextID string) workengine.Candidate {
	t.Helper()
	var candidate workengine.Candidate
	err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		var err error
		if candidate.Context, err = tx.GetContext(contextID); err != nil {
			return err
		}
		if candidate.Worker, err = tx.CurrentContextWorker(contextID); err != nil {
			return err
		}
		if candidate.Scope, err = tx.GetScope(candidate.Worker.ScopeID); err != nil {
			return err
		}
		if candidate.Role, err = tx.GetRole(candidate.Worker.RoleID); err != nil {
			return err
		}
		candidate.BotBinding, err = tx.GetRoleBotBinding(candidate.Worker.RoleID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestStartupFailureReceiptHelper(t *testing.T) {
	if os.Getenv("RAFT_STARTUP_FAILURE_HELPER") != "1" {
		return
	}
	data, err := os.ReadFile(os.Getenv(config.WorkLaunchSnapshotEnv))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot agentproto.RuntimeLaunchSnapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	process, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	receipt := runtimejournal.StartupFailure{Owner: snapshot.Owner, CarrierIncarnation: snapshot.CarrierIncarnation, Stage: "native_bootstrap", ObservedAt: time.Now().UTC(),
		Process: agentproto.RuntimeProcessIdentity{PID: process.PID, Platform: process.Platform, StartIdentity: process.StartIdentity, HostID: snapshot.HostID}}
	if err = runtimejournal.WriteStartupFailure(snapshot, receipt); err != nil {
		t.Fatal(err)
	}
	if release := os.Getenv("RAFT_STARTUP_FAILURE_RELEASE"); release != "" {
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if _, err = os.Stat(release); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("fixture was not released")
	}
}

func TestHostStartupRecoveryRequiresExactExitAndReleasesPoolAfterRestart(t *testing.T) {
	var child *exec.Cmd
	starts := 0
	release := filepath.Join(t.TempDir(), "release")
	h, store := hostFixture(t, func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		starts++
		if starts > 1 {
			return 1234, nil
		}
		child = exec.Command(os.Args[0], "-test.run=^TestStartupFailureReceiptHelper$")
		child.Env = append(os.Environ(), opts.Env...)
		child.Env = append(child.Env, "RAFT_STARTUP_FAILURE_HELPER=1", "RAFT_STARTUP_FAILURE_RELEASE="+release)
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			return 0, err
		}
		return child.Process.Pid, nil
	})
	now := time.Now().UTC()
	h.config.Now = func() time.Time { return now }
	h.config.MaxLiveCarriers = 2
	h.config.ControlReservedCarriers = 1
	seedPoolContexts(t, store, "other")
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(t.Context(), need); err != nil {
		t.Fatal(err)
	}
	before := hostWorker(t, store)
	t.Cleanup(func() { _ = child.Process.Kill() })
	var record LaunchRecord
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		var err error
		record, err = h.readLaunch(tx, before)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := runtimejournal.ReadStartupFailure(record.Snapshot); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("startup fixture did not persist receipt: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := h.ReconcileStartupFailures(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("receipt released a still-live wrapper: %v", err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(store, h.service, h.signer, h.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.ReconcileStartupFailures(t.Context()); err != nil {
		t.Fatal(err)
	}
	coordinator := workruntime.New(store, workruntime.Options{})
	lateHello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: before.InstanceID, Backend: agentproto.Backend(before.Backend), Managed: true, Source: "headless"}, WorkerProtocolVersion: agentproto.WorkerProtocolVersion, JournalProtocolVersion: agentproto.JournalProtocolVersion, CarrierIncarnation: before.CarrierIncarnation}
	if err = coordinator.HandleHello(t.Context(), lateHello); err == nil {
		t.Fatal("late Hello revived a verified exited carrier")
	}
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		count, err := tx.CountNativePoolCarriers("local")
		if err != nil {
			return err
		}
		launch, err := tx.GetOperation(launchID(before))
		if err != nil {
			return err
		}
		identity, err := tx.GetCarrierProcess(before.CarrierIncarnation)
		if err != nil {
			return err
		}
		if count != 0 || launch.LastError != "wrapper exited before readiness: native_bootstrap" || identity.PID != child.Process.Pid {
			t.Fatalf("startup failure did not release exact reservation: %d %+v %+v", count, launch, identity)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Prepare(t.Context(), need); err != nil || starts != 1 {
		t.Fatalf("backoff: %v starts=%d", err, starts)
	}
	now = now.Add(11 * time.Second)
	if err = restarted.Prepare(t.Context(), need); err != nil || starts != 2 {
		t.Fatalf("retry: %v starts=%d", err, starts)
	}
	after := hostWorker(t, store)
	if after.Generation != before.Generation+1 || after.CarrierIncarnation == before.CarrierIncarnation {
		t.Fatalf("startup recovery reused failed carrier: %+v", after)
	}
}

func TestPreparedBackendSwitchRetriesAfterReplacementCapacityFailure(t *testing.T) {
	starts := 0
	h, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) {
		starts++
		return 1234, nil
	})
	registry := &failingRegistry{fail: true, testRegistry: testRegistry{store: store}}
	h.config.Registry = registry
	h.config.MaxLiveCarriers = 3
	h.config.ControlReservedCarriers = 1
	seedPoolContexts(t, store, "switch-context", "waiting-context")
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "switch-context"}
	other := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: "waiting-context"}
	for _, req := range []workengine.WorkerNeed{need, other} {
		if err := h.Prepare(t.Context(), req); err == nil {
			t.Fatal("fixture should fail after durable preparation, before carrier registration")
		}
	}
	old := readLifecycleCandidate(t, store, need.ContextID).Worker
	waiting := readLifecycleCandidate(t, store, other.ContextID).Worker
	h.config.MaxLiveCarriers = 2
	settings := h.config.Settings
	h.config.Settings = settingsFunc(func(ctx context.Context, s work.WorkScope, r work.Role, wc work.WorkContext) (LaunchSettings, error) {
		v, err := settings.ResolveLaunch(ctx, s, r, wc)
		v.Backend = work.BackendCodex
		v.CodexResume = &agentproto.CodexResumePolicy{ModelProviderID: "openai"}
		return v, err
	})
	registry.fail = false
	if err := h.Prepare(t.Context(), need); !errors.Is(err, ErrNativePoolCapacity) {
		t.Fatalf("expected replacement reservation to fail: %v", err)
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		o, err := tx.GetOperation(launchID(old))
		if err != nil {
			return err
		}
		if o.State != work.OperationPrepared || starts != 0 {
			t.Fatalf("failed replacement discarded the old prepared operation: %+v", o)
		}
		waitingLaunch, err := tx.GetOperation(launchID(waiting))
		if err != nil {
			return err
		}
		return h.supersedeUnsentLaunch(tx, waitingLaunch)
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Prepare(t.Context(), need); err != nil || starts != 1 {
		t.Fatalf("verified unsent reservation did not converge: %v starts=%d", err, starts)
	}
	if current := readLifecycleCandidate(t, store, need.ContextID).Worker; current.Backend != work.BackendCodex || current.ID == old.ID {
		t.Fatalf("worker owner did not switch after capacity returned: %+v", current)
	}
}
