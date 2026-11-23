package worktransport

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type fakeRelay struct {
	send func(string, agentproto.Command) error
}

func fixtureSourceActivity(store *workstore.Store) func(context.Context, string) (func(), error) {
	var mu sync.Mutex
	active := make(map[string]int)
	return func(ctx context.Context, scope string) (func(), error) {
		mu.Lock()
		defer mu.Unlock()
		if err := store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertPersonalMigrationOpen(scope) }); err != nil {
			return nil, err
		}
		active[scope]++
		var once sync.Once
		return func() { once.Do(func() { mu.Lock(); defer mu.Unlock(); active[scope]-- }) }, nil
	}
}

func TestMigrationNativeSendClassification(t *testing.T) {
	for _, kind := range []agentproto.CommandKind{agentproto.CommandPromptSend, agentproto.CommandThreadGoalClear, agentproto.CommandMCPOAuthLogin, agentproto.CommandRequestRespond, agentproto.CommandProcessChildRestart} {
		if !effectfulNativeCommand(kind) {
			t.Fatalf("effectful %s bypasses source activity", kind)
		}
	}
	for _, kind := range []agentproto.CommandKind{agentproto.CommandRuntimeInspect, agentproto.CommandTurnInterrupt, agentproto.CommandProcessExit, agentproto.CommandThreadGoalGet} {
		if effectfulNativeCommand(kind) {
			t.Fatalf("read/stop %s blocked by personal source fence", kind)
		}
	}
}

func TestDriverRequiresSourceActivityGuard(t *testing.T) {
	f := newTransportFixture(t)
	if _, err := New(f.store, f.relay, nil); err == nil {
		t.Fatal("driver accepted a missing source activity guard")
	}
}

func (r *fakeRelay) SendCommand(instance string, command agentproto.Command) error {
	if r.send != nil {
		return r.send(instance, command)
	}
	return nil
}

type transportFixture struct {
	t           *testing.T
	store       *workstore.Store
	driver      *Driver
	coordinator *workruntime.Coordinator
	relay       *fakeRelay
	request     workruntime.InspectionRequest
	sequence    uint64
	enqueued    int
}

func newTransportFixture(t *testing.T) *transportFixture {
	t.Helper()
	store, err := workstore.Open(context.Background(), filepath.Join(t.TempDir(), "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	f := &transportFixture{t: t, store: store, relay: &fakeRelay{}}
	f.driver, err = New(store, f.relay, fixtureSourceActivity(store))
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator = workruntime.New(store, workruntime.Options{Driver: f.driver, EnqueueReconcile: func(string) { f.enqueued++ }})
	f.driver.BindRegistrar(f.coordinator)
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	carrier := workstore.CarrierRegistration{Incarnation: "carrier-1", OriginInstanceID: "instance", Owner: owner, Backend: work.BackendCodex, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "principal", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: "principal", RoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Workstream: "main", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutLocation(work.ExecutionLocation{ID: "location", HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutWorker(work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "carrier-1", LocationID: "location", LocationGeneration: 1, Revision: 1}, 0)
	})
	if err := f.coordinator.RegisterCarrier(context.Background(), carrier); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleHello(context.Background(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "instance", Managed: true, Source: "headless", Backend: agentproto.BackendCodex}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "carrier-1"}); err != nil {
		t.Fatal(err)
	}
	f.request = workruntime.InspectionRequest{Carrier: carrier, Binding: owner, LocationID: "location", LocationGeneration: 1}
	return f
}

func (f *transportFixture) tx(callback func(*workstore.Tx) error) {
	f.t.Helper()
	if err := f.store.Within(context.Background(), callback); err != nil {
		f.t.Fatal(err)
	}
}
func (f *transportFixture) result(command agentproto.Command) agentproto.Event {
	f.sequence++
	bootstrap := agentproto.RPCProbe{Request: json.RawMessage(`{"id":"initialize","method":"initialize"}`), Response: json.RawMessage(`{"id":"initialize","result":{}}`)}
	result := &agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: command.CommandID, CarrierIncarnation: "carrier-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, Bootstrap: bootstrap, OwnerScope: &agentproto.RuntimeOwnerInspection{Bootstrap: bootstrap, KnownSessionsComplete: true}, Background: agentproto.BackgroundInspection{CarrierIncarnation: "carrier-1", ProcessGeneration: 1, LocationGeneration: 1, Complete: true, NativeTaskSetComplete: true, NativeInputsComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"source":"fake-owned-host-inventory"}`)}, ObservedAt: time.Now().UTC()}
	event := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: command.CommandID, Execution: agentproto.CloneExecutionBinding(command.Execution), ThreadID: command.Target.ThreadID, TurnID: command.Target.TurnID, RuntimeInspection: result, Seq: f.sequence}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		f.t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "carrier-1", Sequence: event.Seq, PayloadHash: hash}
	return event
}

func TestInspectionPersistsAndRegistersBeforeSynchronousRelayResponse(t *testing.T) {
	f := newTransportFixture(t)
	sent := false
	f.relay.send = func(instance string, command agentproto.Command) error {
		sent = true
		if instance != "instance" || command.Kind != agentproto.CommandRuntimeInspect || command.RuntimeInspection.StopBackground != f.request.StopBackground {
			t.Fatal("wrong fixed inspection command")
		}
		f.tx(func(tx *workstore.Tx) error {
			op, err := tx.GetOperation(command.CommandID)
			if err != nil {
				return err
			}
			native, err := tx.GetNativeCommand(command.CommandID)
			if err != nil {
				return err
			}
			content, err := tx.GetContent("scope", op.RequestRef)
			if err != nil {
				return err
			}
			var canonical agentproto.Command
			if err := json.Unmarshal(content.Data, &canonical); err != nil {
				return err
			}
			if op.State != work.OperationSending || native.Binding != *command.Execution || canonical.CommandID != command.CommandID {
				t.Fatal("relay send preceded canonical op or exact native mapping")
			}
			return nil
		})
		event := f.result(command)
		if err := f.driver.DeliverInspection(context.Background(), event); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("uncommitted result was trusted")
		}
		if err := f.coordinator.HandleEvent(context.Background(), instance, event); err != nil {
			return err
		}
		return f.driver.DeliverInspection(context.Background(), event)
	}
	f.request.StopBackground = true
	observation, err := f.driver.Inspect(context.Background(), f.request)
	if err != nil || !sent || !observation.NativeStopped || !observation.ContinuationStopped || !observation.BackgroundWorkStopped {
		t.Fatalf("fast synchronous response lost waiter or owner-only facts: %#v %v", observation, err)
	}
	if f.enqueued != 0 {
		t.Fatal("inspection result recursively scheduled another inspection")
	}
}

func TestInspectionRejectsUnrelatedIdentityAndMutatedCommittedBody(t *testing.T) {
	f := newTransportFixture(t)
	f.relay.send = func(instance string, command agentproto.Command) error {
		event := f.result(command)
		if err := f.coordinator.HandleEvent(context.Background(), "unregistered-instance", event); err == nil {
			t.Fatal("foreign transport instance accepted")
		}
		if err := f.coordinator.HandleEvent(context.Background(), instance, event); err != nil {
			return err
		}
		mutated := event
		copyResult := *event.RuntimeInspection
		mutated.RuntimeInspection = &copyResult
		copyResult.ObservedAt = copyResult.ObservedAt.Add(time.Minute)
		if err := f.driver.DeliverInspection(context.Background(), mutated); err == nil {
			t.Fatal("changed result reused an authentic committed hash")
		}
		mutated = event
		copyResult = *event.RuntimeInspection
		mutated.RuntimeInspection = &copyResult
		copyResult.CarrierIncarnation = "other-incarnation"
		if err := f.driver.DeliverInspection(context.Background(), mutated); err == nil {
			t.Fatal("foreign carrier incarnation accepted")
		}
		mutated = event
		mutated.CommandID = "other-command"
		if err := f.driver.DeliverInspection(context.Background(), mutated); err == nil {
			t.Fatal("foreign command accepted")
		}
		return f.driver.DeliverInspection(context.Background(), event)
	}
	if _, err := f.driver.Inspect(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
}

func TestInspectionTimeoutAndLateResultNeverReleaseBusinessOccupancy(t *testing.T) {
	f := newTransportFixture(t)
	var sent agentproto.Command
	f.relay.send = func(_ string, command agentproto.Command) error { sent = command; return nil }
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutInbox(work.Inbox{ID: "input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "event", State: work.InboxPending, Revision: 1}, 0); err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		op := work.Operation{ID: "start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: "start", TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestHash: "start-hash", State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now().UTC()})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := f.driver.Inspect(ctx, f.request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("inspection timeout=%v", err)
	}
	f.tx(func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(sent.CommandID)
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.request.Carrier, "attempt")
		if err != nil {
			return err
		}
		if op.State != work.OperationUnknown || !occupied {
			t.Fatal("inspection timeout fabricated quiescence or lost uncertainty")
		}
		return nil
	})
	event := f.result(sent)
	if err := f.coordinator.HandleEvent(context.Background(), "instance", event); err != nil {
		t.Fatal(err)
	}
	if err := f.driver.DeliverInspection(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasCarrierOccupancy(f.request.Carrier, "attempt")
		if err != nil {
			return err
		}
		if !occupied {
			t.Fatal("late inspection receipt released a business attempt")
		}
		return nil
	})
}

func TestIdleReconnectRunsOwnerInspectionBeforeRestoringReady(t *testing.T) {
	f := newTransportFixture(t)
	if err := f.coordinator.Disconnect(t.Context(), "instance"); err != nil {
		t.Fatal(err)
	}
	// A fresh coordinator rediscovers the gate. There was no active attempt to
	// drive the ordinary reconciliation loop.
	f.coordinator = workruntime.New(f.store, workruntime.Options{Driver: f.driver})
	f.driver.BindRegistrar(f.coordinator)
	hello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "instance", Managed: true, Source: "headless", Backend: agentproto.BackendCodex}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "carrier-1"}
	if err := f.coordinator.HandleHello(t.Context(), hello); err != nil {
		t.Fatal(err)
	}
	safe := false
	calls := 0
	f.relay.send = func(instance string, command agentproto.Command) error {
		calls++
		if instance != "instance" || command.Kind != agentproto.CommandRuntimeInspect || command.Execution == nil || *command.Execution != f.request.Binding {
			t.Fatalf("invalid owner inspection command: %+v", command)
		}
		event := f.result(command)
		if !safe {
			event.RuntimeInspection.Background.PendingTaskIDs = []string{"still-running"}
			hash, err := agentproto.RuntimeEventPayloadHash(event)
			if err != nil {
				t.Fatal(err)
			}
			event.Journal.PayloadHash = hash
		}
		if err := f.coordinator.HandleEvent(t.Context(), instance, event); err != nil {
			return err
		}
		return f.driver.DeliverInspection(t.Context(), event)
	}
	if err := f.coordinator.ReconcileIdleCarriers(t.Context(), 8); err == nil {
		t.Fatal("incomplete quiescence was accepted")
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRecovering {
			t.Fatalf("restored before complete quiescence: %s", worker.State)
		}
		return nil
	})
	safe = true
	if err := f.coordinator.ReconcileIdleCarriers(t.Context(), 8); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady || worker.Generation != 1 || worker.CarrierIncarnation != "carrier-1" || calls != 2 {
			t.Fatalf("idle worker did not recover exact carrier: %+v, inspections=%d", worker, calls)
		}
		if _, err = tx.GetRuntimeGate(worker.ID); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("idle gate retained after exact proof: %v", err)
		}
		return nil
	})
}

func TestRejectedResumeInspectionUsesOwnerInventoryWithoutProbingMissingSession(t *testing.T) {
	f := newTransportFixture(t)
	request := f.request
	request.ResumeRejected = true
	request.SubjectCommandID = "rejected-resume"
	request.SessionID = "missing-history"
	request.Binding.AttemptID, request.Binding.RunRef = "attempt", "run"
	f.relay.send = func(instance string, command agentproto.Command) error {
		if command.Execution == nil || *command.Execution != f.request.Binding || command.Target.ThreadID != "missing-history" || command.Target.TurnID != "" {
			t.Fatalf("rejected native resume did not inspect the physical owner's complete inventory: %+v", command)
		}
		event := f.result(command)
		if err := f.coordinator.HandleEvent(t.Context(), instance, event); err != nil {
			return err
		}
		return f.driver.DeliverInspection(t.Context(), event)
	}
	proof, err := f.driver.Inspect(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err = proof.ValidateStopped(request); err != nil {
		t.Fatal(err)
	}
}
