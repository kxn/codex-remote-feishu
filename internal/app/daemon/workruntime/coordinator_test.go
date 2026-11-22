package workruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type testDriver struct {
	mu          sync.Mutex
	inspect     func(InspectionRequest) (NativeObservation, error)
	sendCommand func(context.Context, string, agentproto.Command) error
	sendErr     error
	inspections int
}

func (d *testDriver) SendCommand(ctx context.Context, instance string, command agentproto.Command) error {
	d.mu.Lock()
	hook, err := d.sendCommand, d.sendErr
	d.mu.Unlock()
	if hook != nil {
		return hook(ctx, instance, command)
	}
	return err
}
func (d *testDriver) Inspect(_ context.Context, request InspectionRequest) (NativeObservation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.inspections++
	if d.inspect != nil {
		return d.inspect(request)
	}
	return stoppedObservation(request), nil
}
func stoppedObservation(request InspectionRequest) NativeObservation {
	return NativeObservation{CarrierIncarnation: request.Carrier.Incarnation, InstanceID: request.Carrier.OriginInstanceID, InstanceGeneration: request.Carrier.InstanceGeneration, ProcessGeneration: request.Carrier.ProcessGeneration, LocationGeneration: request.LocationGeneration, SessionID: request.SessionID, TurnID: request.TurnID, NativeStopped: true, ContinuationStopped: true, BackgroundWorkStopped: true, NativeEvidence: json.RawMessage(`{"type":"result","subtype":"success"}`), ContinuationEvidence: json.RawMessage(`{"cleared":true}`), BackgroundEvidence: json.RawMessage(`{"processes":[],"tasks":[],"inspection":"host"}`), ObservedAt: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
}

type fixture struct {
	t           *testing.T
	store       *workstore.Store
	coordinator *Coordinator
	driver      *testDriver
	carrier     workstore.CarrierRegistration
	command     agentproto.Command
	sequence    uint64
	receipts    []agentproto.EventReceipt
	now         time.Time
	path        string
}

func newFixture(t *testing.T, backend work.Backend, review ...bool) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work.db")
	store, err := workstore.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	f := &fixture{t: t, store: store, driver: &testDriver{}, now: time.Date(2026, 11, 21, 8, 0, 0, 0, time.UTC), path: path}
	f.carrier = workstore.CarrierRegistration{Incarnation: "inc-1", OriginInstanceID: "instance-1", Owner: agentproto.ExecutionBinding{Version: 1, ScopeID: "scope", ContextID: "context", RoleID: "role", WorkerSessionID: "worker", SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}, Backend: backend, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1}
	f.coordinator = New(store, Options{Driver: f.driver, Clock: func() time.Time { return f.now }, AuthorizeControl: func(context.Context, work.Actor, string, string) error { return nil }, SendReceipt: func(_ string, receipt agentproto.EventReceipt) error {
		f.receipts = append(f.receipts, receipt)
		return nil
	}})
	var reviewIngress string
	authenticatedPrivate := len(review) > 1 && review[1]
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "principal", DisplayName: "Person", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "work", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		principalID := "principal"
		if len(review) > 0 && review[0] || authenticatedPrivate {
			if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
				return err
			}
			raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "review-event", Tenant: "tenant", ChatID: "private-chat", ChatType: "p2p", MessageID: "review-human-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "review-human"}, MessageType: "text", RawContent: `{"text":"/review"}`, ReceivedAt: f.now}
			if authenticatedPrivate && !review[0] {
				raw.EventID, raw.MessageID, raw.RawContent = "private-human-event", "om_user", `{"text":"work input"}`
			}
			ingress, _, err := tx.AcceptRawIngress(raw, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
			if err != nil {
				return err
			}
			reviewIngress = ingress.ID
			principal, _, err := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
			if err != nil {
				return err
			}
			principalID = principal.ID
		}
		if err := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: principalID, RoleID: "role", Revision: 1}, 0); err != nil {
			return err
		}
		wc := work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Workstream: "main", Revision: 1}
		if len(review) > 0 && review[0] {
			if err := tx.PutContext(work.WorkContext{ID: "review-source", ScopeID: "scope", RoleID: "role", Workstream: "review-source", Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutLocation(work.ExecutionLocation{ID: "location", HostID: "host", Cwd: t.TempDir(), IsolatedContextID: "review-source", Generation: 1, Revision: 1}, 0); err != nil {
				return err
			}
			if err := tx.PutContent(work.NewContent("review-request", "scope", "text/plain", []byte("human requested a bounded read-only review"), f.now)); err != nil {
				return err
			}
			if err := tx.PutPrivateCommand(work.TypedCommandEnvelope{ID: "review-command", IngressID: reviewIngress, ScopeID: "scope", ContextID: "review-source", PrincipalID: principalID, RoleID: "role", GatewayRef: "gateway", Text: "/review", State: "sending", Revision: 1}, 0); err != nil {
				return err
			}
			_, reviewSource, err := tx.ValidatePrivateAction(workstore.PrivateActionSource{PersonalScopeID: "scope", CommandID: "review-command"})
			if err != nil {
				return err
			}
			wc.Purpose = work.PurposeReview
			wc.Review = &work.ReviewSpec{SourceContextID: "review-source", ResourceScopeID: "scope", LocationID: "location", LocationGeneration: 1, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}, Source: reviewSource}
		}
		if len(review) > 2 && review[2] {
			if err := tx.PutContext(work.WorkContext{ID: "control-source", ScopeID: "scope", RoleID: "role", Workstream: "control-source", Revision: 1}, 0); err != nil {
				return err
			}
			wc.Purpose, wc.ControlTargetContextID = work.PurposeControl, "control-source"
		}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		if (len(review) == 0 || !review[0]) && !authenticatedPrivate {
			if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
				return err
			}
		}
		if len(review) == 0 || !review[0] {
			location := work.ExecutionLocation{ID: "location", HostID: "host", Cwd: t.TempDir(), Generation: 1, Revision: 1}
			if len(review) > 2 && review[2] {
				location.IsolatedContextID = wc.ID
			}
			if err := tx.PutLocation(location, 0); err != nil {
				return err
			}
		}
		if err := tx.PutWorker(work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "role", Backend: backend, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "instance-1", InstanceGeneration: 1, ProcessGeneration: 1, CarrierIncarnation: "inc-1", LocationID: "location", LocationGeneration: 1, Revision: 1}, 0); err != nil {
			return err
		}
		input := work.Inbox{ID: "input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "event", State: work.InboxPending, Revision: 1}
		if len(review) > 1 && review[1] {
			if err := tx.PutContent(work.NewContent("source-body", "scope", "text/plain", []byte("work input"), f.now)); err != nil {
				return err
			}
			input.MessageID = "private-human-input"
			message := work.Message{ID: input.MessageID, ScopeID: "scope", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: principalID},
				PlatformKey:  &work.MessageKey{Tenant: "tenant", ChatID: "private-chat", MessageID: "om_user"},
				Observations: []work.Observation{{AppID: "app", IdentityGeneration: 1, ReceivedAt: f.now}}, BodyRef: "source-body", Revision: 1, CreatedAt: f.now,
			}
			inputs := []work.Inbox{input}
			if len(review) > 2 && review[2] {
				original := input
				original.ID, original.RecipientContextID = "original-control-input", "control-source"
				original.EventID = "original-control-event"
				input.ControlOfInputID = original.ID
				inputs = []work.Inbox{original, input}
			}
			if _, _, err := tx.AcceptMessage(message, inputs); err != nil {
				return err
			}
		} else if err := tx.PutInbox(input, 0); err != nil {
			return err
		}
		return tx.PutContent(work.NewContent("report-outcome", "scope", "text/plain", []byte("explicit business result"), f.now))
	})
	if err := f.coordinator.RegisterCarrier(context.Background(), f.carrier); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleHello(context.Background(), f.hello()); err != nil {
		t.Fatal(err)
	}
	binding := f.carrier.Owner
	binding.AttemptID = "attempt"
	binding.RunRef = "run"
	f.command = agentproto.Command{CommandID: "command", Kind: agentproto.CommandPromptSend, Execution: &binding, InstructionRevision: "v1", Target: agentproto.Target{ThreadID: "session", CWD: "/workspace"}, Prompt: agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "work input"}}}}
	// A fourth optional fixture flag selects a genuine new-session prompt.
	if len(review) > 3 && review[3] {
		f.command.Target.ThreadID = ""
		f.command.Target.ExecutionMode = agentproto.PromptExecutionModeStartNew
	}
	f.tx(func(tx *workstore.Tx) error {
		operation, err := f.operation(tx, f.command)
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ContextID: "context", ScopeID: "scope", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "command", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance-1", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: operation, Now: f.now})
	})
	if err := f.coordinator.RegisterCommand(context.Background(), "inc-1", f.command); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) tx(callback func(*workstore.Tx) error) {
	f.t.Helper()
	if err := f.store.Within(context.Background(), callback); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) hello() agentproto.Hello {
	return agentproto.Hello{Protocol: agentproto.WireProtocol, Instance: agentproto.InstanceHello{InstanceID: "instance-1", Backend: agentproto.Backend(f.carrier.Backend), Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: "inc-1"}
}
func (f *fixture) operation(tx *workstore.Tx, command agentproto.Command) (work.Operation, error) {
	raw, err := json.Marshal(command)
	if err != nil {
		return work.Operation{}, err
	}
	hash := sha256.Sum256(raw)
	ref := "request:" + command.CommandID
	if err := tx.PutContent(work.NewContent(ref, "scope", "application/json", raw, f.now)); err != nil {
		return work.Operation{}, err
	}
	return work.Operation{ID: command.CommandID, ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(command.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: command.CommandID, RequestHash: hex.EncodeToString(hash[:]), RequestRef: ref, State: work.OperationPrepared, Revision: 1}, nil
}
func (f *fixture) registerExtra(command agentproto.Command) {
	f.t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		op, err := f.operation(tx, command)
		if err != nil {
			return err
		}
		_, _, err = tx.PutOperation(op, 0)
		return err
	})
	if err := f.coordinator.RegisterCommand(context.Background(), "inc-1", command); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) durable(event agentproto.Event) agentproto.Event {
	f.sequence++
	event.Seq = f.sequence
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		f.t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: f.carrier.Incarnation, Sequence: f.sequence, PayloadHash: hash}
	return event
}
func (f *fixture) handle(event agentproto.Event) error {
	return f.coordinator.HandleEvent(context.Background(), f.carrier.OriginInstanceID, f.durable(event))
}
func (f *fixture) start() {
	f.t.Helper()
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "command", ThreadID: "session", TurnID: "turn-1"}); err != nil {
		f.t.Fatal(err)
	}
}
func (f *fixture) report() {
	f.t.Helper()
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: "attempt", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, ProcessedInputIDs: []string{"input"}, OutcomeRef: "report-outcome", Checkpoint: "explicit checkpoint", Revision: 1}, 0)
	})
}

func TestNativeCompletionUsesReportAndSeparateQuiescence(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	f.start()
	f.report()
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		observation := stoppedObservation(request)
		observation.BackgroundWorkStopped = false
		observation.BackgroundEvidence = json.RawMessage(`{"tasks":["still-running"]}`)
		return observation, nil
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err == nil {
		t.Fatal("uncertain background was released")
	}
	f.tx(func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptCompleted || worker.State != work.WorkerRecovering || !occupied {
			t.Fatalf("terminal outcome falsely released resources: %s %s %v", attempt.State, worker.State, occupied)
		}
		return nil
	})
	f.driver.inspect = nil
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		if input.State != work.InboxProcessed || worker.State != work.WorkerReady || occupied {
			t.Fatal("trusted quiescence failed to release exact resources")
		}
		return nil
	})
}

func TestMissingReportReleasesOnlyAfterProofAndCreatesBoundedRecovery(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	f.start()
	event := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"})
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", event); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", event); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		gate, err := tx.GetRuntimeGate("worker")
		if err != nil {
			return err
		}
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending || worker.State != work.WorkerUnavailable || gate.Reason != "missing_processing_report" || occupied || attempt.State != work.AttemptFailed {
			t.Fatal("missing report consumed input, retained proven-free slot, or allowed infinite retry")
		}
		return nil
	})
	if len(f.receipts) != 3 {
		t.Fatalf("duplicate terminal should get durable ACK again: %d", len(f.receipts))
	}
}

func TestUnknownNativeOwnershipIsDurableAndNeverUsesLatestAttempt(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	unknown := f.durable(agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "session", TurnID: "unproven"})
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", unknown); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		if _, err := tx.GetNativeTurn("inc-1", "session", "unproven"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("latest attempt was used as ownership")
		}
		count, err := tx.CountUnresolvedNativeEvents("inc-1")
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatal("unproven event was not quarantined")
		}
		return nil
	})
	if len(f.receipts) != 1 {
		t.Fatal("quarantined durable evidence was not ACKed")
	}
	if err := f.handle(agentproto.Event{Kind: agentproto.EventNativeTurnBindingObserved, CommandID: "command", ThreadID: "session", TurnID: "unproven"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		turn, err := tx.GetNativeTurn("inc-1", "session", "unproven")
		if err != nil {
			return err
		}
		count, err := tx.CountUnresolvedNativeEvents("inc-1")
		if err != nil {
			return err
		}
		if !turn.Started || turn.Binding.AttemptID != "attempt" || count != 0 {
			t.Fatal("exact protocol response failed to reconcile prior notification")
		}
		return nil
	})
}

func TestHelloRequiresPreregisteredManagedDurableCarrier(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	for _, change := range []func(*agentproto.Hello){func(h *agentproto.Hello) { h.JournalProtocolVersion = 0 }, func(h *agentproto.Hello) { h.CarrierIncarnation = "unknown" }, func(h *agentproto.Hello) { h.Instance.InstanceID = "other" }, func(h *agentproto.Hello) { h.Instance.Managed = false }} {
		hello := f.hello()
		change(&hello)
		if err := f.coordinator.HandleHello(context.Background(), hello); err == nil {
			t.Fatal("unregistered or incapable wrapper admitted")
		}
	}
	if err := f.coordinator.Disconnect(context.Background(), "instance-1"); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		occupied, err := tx.HasCarrierOccupancy(f.carrier, "attempt")
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if !occupied || worker.State != work.WorkerRecovering {
			t.Fatal("disconnect released ownership")
		}
		return nil
	})
}

func TestEmptyOutcomeCheckpointKeepsPriorCheckpoint(t *testing.T) {
	f := newFixture(t, work.BackendClaude)
	f.start()
	f.tx(func(tx *workstore.Tx) error {
		wc, err := tx.GetContext("context")
		if err != nil {
			return err
		}
		wc.Checkpoint = "prior pending decision"
		wc.Revision++
		if err = tx.PutContext(wc, wc.Revision-1); err != nil {
			return err
		}
		return tx.PutProcessingReport(workstore.ProcessingReport{AttemptID: "attempt",
			RunRef:            work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"},
			ProcessedInputIDs: []string{"input"}, OutcomeRef: "report-outcome", Revision: 1}, 0)
	})
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		wc, err := tx.GetContext("context")
		if err != nil {
			return err
		}
		if wc.Checkpoint != "prior pending decision" {
			t.Fatalf("report erased durable recovery context: %q", wc.Checkpoint)
		}
		return nil
	})
}
