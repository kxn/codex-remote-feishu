package workengine

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestPreparedCommandSurvivesCrashBeforeExternalSend(t *testing.T) {
	s, path := fixture(t, "a")
	enqueue(t, s, "input", "a", "leader", "", "")
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 1, planner, dispatcher)
	candidates, _, _, err := e.snapshot(t.Context())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("snapshot %+v %v", candidates, err)
	}
	plan, err := planner.Plan(t.Context(), candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := e.admit(t.Context(), candidates[0], plan)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := engine(t, reopened, 1, planner, dispatcher)
	pending, err := restored.ReconcilePending(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("restore %+v %v", pending, err)
	}
	if pending[0].Operation.State != work.OperationPrepared || pending[0].Command.CommandID != claimed.Command.CommandID || dispatcher.count() != 0 {
		t.Fatal("prepared command was lost or blindly resent")
	}
	receipt, err := restored.Replay(t.Context(), pending[0].Operation.ID)
	if err != nil || receipt.Err != nil || receipt.State != work.OperationConfirmed {
		t.Fatalf("safe replay %+v %v", receipt, err)
	}
}
func TestReassignedTaskCannotRunBeforeOldExecutionIsQuiescent(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		if err := tx.PutTask(work.Task{ID: "task", ScopeID: "a", Title: "handoff", OwnerRoleID: "expert", AssignmentGeneration: 1, RequirementsRevision: 1, State: work.TaskTodo, Revision: 1}, 0); err != nil {
			return err
		}
		return putWorker(tx, "a", "expert", "task", ":old")
	})
	enqueue(t, s, "old-input", "a", "expert", "task", "")
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 2, planner, dispatcher)
	old, err := e.Tick(t.Context())
	if err != nil || len(old.Dispatched) != 1 {
		t.Fatalf("initial task %+v %v", old, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		task.OwnerRoleID = "leader"
		task.AssignmentGeneration++
		task.Revision++
		if err = tx.PutTask(task, 1); err != nil {
			return err
		}
		return putWorker(tx, "a", "leader", "task", ":new")
	})
	enqueue(t, s, "new-input", "a", "leader", "task", "")
	next, err := e.Tick(t.Context())
	if err != nil || len(next.Dispatched) != 0 || dispatcher.count() != 1 {
		t.Fatalf("old execution and new owner overlapped %+v %v", next, err)
	}
}
func TestSharedLocationResourceRejectsSecondIndependentCarrier(t *testing.T) {
	s, _ := fixture(t, "a", "b")
	transaction(t, s, func(tx *workstore.Tx) error {
		for _, scope := range []string{"a", "b"} {
			location := work.ExecutionLocation{ID: "location:" + scope, HostID: "host", Cwd: "/checkout/" + scope, ResourceKeys: []string{"shared-target-ref"}, Generation: 1, Revision: 1}
			if err := tx.PutLocation(location, 0); err != nil {
				return err
			}
			worker, err := tx.GetWorker("worker:" + scope + ":leader")
			if err != nil {
				return err
			}
			worker.LocationID = location.ID
			worker.LocationGeneration = 1
			worker.Generation++
			worker.Revision++
			if err = tx.PutWorker(worker, 1); err != nil {
				return err
			}
		}
		return nil
	})
	enqueue(t, s, "a-input", "a", "leader", "", "")
	enqueue(t, s, "b-input", "b", "leader", "", "")
	dispatcher := &testDispatcher{}
	r, err := engine(t, s, 2, &testPlanner{}, dispatcher).Tick(t.Context())
	if err != nil || len(r.Dispatched) != 1 {
		t.Fatalf("location overcommit %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		for _, scope := range []string{"a", "b"} {
			operations, err := tx.ListOperations(scope)
			if err != nil {
				return err
			}
			if len(operations) > 1 {
				t.Fatal("failed reservation leaked operation")
			}
		}
		return nil
	})
}
