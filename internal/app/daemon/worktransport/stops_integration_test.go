package worktransport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (f *transportFixture) nativeEvent(event agentproto.Event) agentproto.Event {
	f.sequence++
	event.Seq = f.sequence
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		f.t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "carrier-1", Sequence: event.Seq, PayloadHash: hash}
	return event
}
func activeStopFixture(t *testing.T) *transportFixture {
	f := newTransportFixture(t)
	binding := f.request.Carrier.Owner
	binding.AttemptID = "attempt"
	binding.RunRef = "run"
	command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, Execution: &binding, Target: agentproto.Target{ThreadID: "session"}}
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutInbox(work.Inbox{ID: "input", ScopeID: "scope", RecipientRoleID: "role", RecipientContextID: "context", EventID: "event", State: work.InboxPending, Revision: 1}, 0); err != nil {
			return err
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		data, _ := json.Marshal(command)
		body := work.NewContent("start-body", "scope", "application/json", data, time.Now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: "start", ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorRole, ID: "role"}, Kind: string(command.Kind), TargetID: "worker", TargetGeneration: 1, IdempotencyKey: "start", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: work.ExecutionAttempt{ID: "attempt", ScopeID: "scope", ContextID: "context", RunRef: work.RunRef{WorkerID: "worker", Generation: 1, RunID: "run"}, State: work.AttemptDispatchPending, InputIDs: []string{"input"}, CommandID: "start", Revision: 1}, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: 1, InstanceID: "instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: time.Now()})
	})
	if err := f.driver.SendCommand(context.Background(), "instance", command); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleEvent(context.Background(), "instance", f.nativeEvent(agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: "start", ThreadID: "session", TurnID: "turn"})); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAdmissionStopCommandsUseDurableDriverAndNeverResendUnknown(t *testing.T) {
	f := activeStopFixture(t)
	stops := workcontrol.NewStops(f.store, f.driver)
	f.tx(func(tx *workstore.Tx) error {
		return tx.PutPauseGate(work.PauseGate{ID: "pause-context", ScopeID: "scope", ContextID: "context", Source: work.GateContext, SourceID: "human-stop", State: work.GateClosed, Revision: 1}, 0)
	})
	calls := 0
	f.relay.send = func(instance string, command agentproto.Command) error {
		calls++
		if instance != "instance" || command.Execution == nil || command.Execution.AttemptID != "attempt" || command.Execution.RunRef != "run" || command.Target.ThreadID != "session" {
			t.Fatal("stop fell back to a different native execution")
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
			if op.State != work.OperationSending || native.Binding != *command.Execution {
				t.Fatal("transport preceded durable stop mapping")
			}
			return nil
		})
		return errors.New("lost native stop response")
	}
	if err := stops.RunOnce(context.Background()); err == nil {
		t.Fatal("lost stop response reported success")
	}
	if calls != 2 {
		t.Fatalf("expected exact Goal clear plus interrupt, got %d", calls)
	}
	if err := stops.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("unknown stop commands were resent")
	}
	f.tx(func(tx *workstore.Tx) error {
		if _, err := tx.GetQuiescenceReceipt("attempt"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("send failure fabricated quiescence")
		}
		occupied, err := tx.HasAnyCarrierOccupancy(f.request.Carrier)
		if err != nil {
			return err
		}
		if !occupied {
			t.Fatal("send failure released native carrier")
		}
		return nil
	})
}

func TestAdmissionStopContextGateDoesNotStopAnotherWorkstream(t *testing.T) {
	f := activeStopFixture(t)
	stops := workcontrol.NewStops(f.store, f.driver)
	f.tx(func(tx *workstore.Tx) error {
		if err := tx.PutContext(work.WorkContext{ID: "other-context", ScopeID: "scope", RoleID: "role", Workstream: "another", Revision: 1}, 0); err != nil {
			return err
		}
		return tx.PutPauseGate(work.PauseGate{ID: "other-pause", ScopeID: "scope", ContextID: "other-context", Source: work.GateContext, SourceID: "human-stop", State: work.GateClosed, Revision: 1}, 0)
	})
	calls := 0
	f.relay.send = func(string, agentproto.Command) error { calls++; return nil }
	if err := stops.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("context pause stopped an unrelated worker")
	}
}
