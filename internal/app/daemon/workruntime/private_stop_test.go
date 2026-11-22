package workruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workcontrol"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPrivateStopInterruptsExactRunAndReleasesNewInputAfterQuiescence(t *testing.T) {
	f := newFixture(t, work.BackendCodex)
	f.start()
	var stopped work.ExecutionAttempt
	f.tx(func(tx *workstore.Tx) error {
		var err error
		stopped, err = tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		scope, err := tx.GetScope(stopped.ScopeID)
		if err != nil {
			return err
		}
		source := work.SourceRef{Kind: "human_interaction", ID: "stop", Version: "1", EvidenceRef: "stop"}
		if err = tx.RecordPrivateAttemptStop(stopped, scope, source, f.now, f.now); err != nil {
			return err
		}
		return tx.PutInbox(work.Inbox{ID: "new-input", ScopeID: scope.ID, RecipientContextID: stopped.ContextID, RecipientRoleID: scope.RoleID, EventID: "new-event", State: work.InboxPending, Revision: 1}, 0)
	})
	commands := map[agentproto.CommandKind]agentproto.Command{}
	f.driver.sendCommand = func(_ context.Context, instance string, command agentproto.Command) error {
		if instance != f.carrier.OriginInstanceID || command.Execution == nil || command.Execution.AttemptID != stopped.ID || command.Execution.RunRef != stopped.RunRef.RunID {
			t.Fatalf("stop command targeted a different run: %s %+v", instance, command)
		}
		commands[command.Kind] = command
		return nil
	}
	stops := workcontrol.NewStops(f.store, f.driver)
	if err := stops.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[agentproto.CommandThreadGoalClear].Target.ThreadID != "session" || commands[agentproto.CommandTurnInterrupt].Target.ThreadID != "session" || commands[agentproto.CommandTurnInterrupt].Target.TurnID != "turn-1" {
		t.Fatalf("exact Goal and turn stop commands missing: %+v", commands)
	}
	inspected := false
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		inspected = true
		if !request.StopBackground || request.Binding.AttemptID != stopped.ID {
			t.Fatalf("stop inspection missed the exact background owners: %+v", request)
		}
		return stoppedObservation(request), nil
	}
	// A terminal transition can race the human interrupt. The recorded stop
	// still fences automatic continuation and cancels unprocessed old input.
	if err := f.handle(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	if !inspected {
		t.Fatal("the stop did not reconcile native and background cessation")
	}
	if err := stops.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		old, e := tx.GetInbox("input")
		if e != nil {
			return e
		}
		if old.State != work.InboxCancelled {
			t.Fatalf("old input could dispatch again: %+v", old)
		}
		for _, command := range commands {
			op, e := tx.GetOperation(command.CommandID)
			if e != nil {
				return e
			}
			if op.State != work.OperationConfirmed {
				t.Fatalf("native stop lacks quiescence receipt: %+v", op)
			}
		}
		if _, _, e = tx.QueueAutomationFollowup(stopped.ID, work.AutomationWhip, work.SourceRef{}, f.now); !errors.Is(e, workstore.ErrAutomationNotApplicable) {
			t.Fatalf("a stopped attempt could continue: %v", e)
		}
		worker, e := tx.GetWorker(stopped.RunRef.WorkerID)
		if e != nil {
			return e
		}
		next := work.ExecutionAttempt{ID: "new-attempt", ScopeID: stopped.ScopeID, ContextID: stopped.ContextID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: worker.Generation, RunID: "new-run"}, InputIDs: []string{"new-input"}, CommandID: "new-command", State: work.AttemptDispatchPending, Revision: 1}
		op := work.Operation{ID: next.CommandID, ScopeID: next.ScopeID, ContextID: next.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: next.CommandID, RequestHash: "new-prompt", State: work.OperationPrepared, Revision: 1}
		return tx.ClaimAttempt(workstore.ClaimRequest{Attempt: next, ExpectedWorkerRevision: worker.Revision, ControllerGeneration: worker.ControllerGeneration, InstanceID: worker.InstanceID, InstanceGeneration: worker.InstanceGeneration, ProcessGeneration: worker.ProcessGeneration, LocationGeneration: worker.LocationGeneration, StartOperation: op, Now: f.now})
	})
}
