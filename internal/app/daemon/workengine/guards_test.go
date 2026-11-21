package workengine

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestSharedCarrierCannotReceiveTwoClaims(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker:a:expert")
		if err != nil {
			return err
		}
		leader, err := tx.GetWorker("worker:a:leader")
		if err != nil {
			return err
		}
		worker.InstanceID = leader.InstanceID
		worker.CarrierIncarnation = leader.CarrierIncarnation
		worker.Generation++
		worker.Revision++
		return tx.PutWorker(worker, 1)
	})
	enqueue(t, s, "leader", "a", "leader", "", "")
	enqueue(t, s, "expert", "a", "expert", "", "")
	dispatcher := &testDispatcher{}
	r, err := engine(t, s, 2, &testPlanner{}, dispatcher).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 || dispatcher.count() != 1 {
		t.Fatalf("shared carrier overcommit %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		inputs, err := tx.ListInbox("a")
		if err != nil {
			return err
		}
		pending := 0
		for _, input := range inputs {
			if input.State == work.InboxPending {
				pending++
			}
		}
		if pending != 1 {
			t.Fatalf("rejected claim consumed inbox: %+v", inputs)
		}
		return nil
	})
}
func TestBusyWorkstreamKeepsLateInputForNextAttempt(t *testing.T) {
	s, _ := fixture(t, "a")
	enqueue(t, s, "first", "a", "leader", "", "")
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 2, planner, dispatcher)
	first, err := e.Tick(t.Context())
	if err != nil || len(first.Dispatched) != 1 {
		t.Fatalf("initial tick %+v %v", first, err)
	}
	enqueue(t, s, "late", "a", "leader", "", "")
	second, err := e.Tick(t.Context())
	if err != nil || len(second.Dispatched) != 0 {
		t.Fatalf("busy run duplicated %+v %v", second, err)
	}
	finish(t, s, first.Dispatched[0].AttemptID)
	third, err := e.Tick(t.Context())
	if err != nil || len(third.Dispatched) != 1 {
		t.Fatalf("late input not resumed %+v %v", third, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(third.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if len(attempt.InputIDs) != 1 || attempt.InputIDs[0] != "late" {
			t.Fatalf("wrong frozen input set %+v", attempt.InputIDs)
		}
		return nil
	})
}

type testProvisioner struct{ needs chan WorkerNeed }

func (p *testProvisioner) Prepare(_ context.Context, need WorkerNeed) error {
	p.needs <- need
	return nil
}
func TestMissingTaskWorkerStaysPendingAndRequestsPreparation(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		return tx.PutTask(work.Task{ID: "task", ScopeID: "a", Title: "independent", OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}, 0)
	})
	enqueue(t, s, "task-input", "a", "expert", "task", "")
	provisioner := &testProvisioner{needs: make(chan WorkerNeed, 4)}
	dispatcher := &testDispatcher{}
	e, err := New(s, Config{MaxConcurrent: 2, Now: func() time.Time { return testNow }, Provisioner: provisioner}, &testPlanner{}, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.Tick(t.Context())
	if err != nil || len(r.Dispatched) != 0 {
		t.Fatalf("missing worker %+v %v", r, err)
	}
	select {
	case need := <-provisioner.needs:
		if need.TaskID != "task" || dispatcher.count() != 0 {
			t.Fatal("task collapsed into role main worker")
		}
	case <-time.After(time.Second):
		t.Fatal("missing task did not receive bounded asynchronous preparation")
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		input, err := tx.GetInbox("task-input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatal("provisioning falsely consumed input")
		}
		return nil
	})
}
func TestPauseIsRecheckedAfterPlanning(t *testing.T) {
	s, _ := fixture(t, "a")
	enqueue(t, s, "input", "a", "leader", "", "")
	planner := &testPlanner{hook: func(Candidate) {
		transaction(t, s, func(tx *workstore.Tx) error {
			return tx.PutPauseGate(work.PauseGate{ID: "pause", ScopeID: "a", Source: work.GateProject, SourceID: "human", State: work.GateClosed, Revision: 1}, 0)
		})
	}}
	dispatcher := &testDispatcher{}
	r, err := engine(t, s, 2, planner, dispatcher).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 0 || dispatcher.count() != 0 {
		t.Fatalf("pause lost between plan and admission %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		operations, err := tx.ListOperations("a")
		if err != nil {
			return err
		}
		if len(operations) != 0 {
			t.Fatal("failed paused admission leaked command")
		}
		return nil
	})
}
func TestQueueCarriesExplicitTaskInsteadOfParsingEventText(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		input := work.Inbox{ID: "event-input", ScopeID: "a", RecipientRoleID: "leader", EventID: "task:missing-child:do-not-parse", State: work.InboxPending, Revision: 1}
		return tx.PutInbox(input, 0)
	})
	r, err := engine(t, s, 1, &testPlanner{}, &testDispatcher{}).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 {
		t.Fatalf("event text accidentally parsed as task %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(r.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if attempt.TaskID != "" {
			t.Fatal("event ID fabricated task linkage")
		}
		return nil
	})
}
