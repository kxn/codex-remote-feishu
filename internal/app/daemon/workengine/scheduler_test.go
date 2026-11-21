package workengine

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"sync"
	"testing"
	"time"
)

func TestTwoScopesDispatchConcurrentlyWithoutInferringRunning(t *testing.T) {
	s, _ := fixture(t, "a", "b")
	enqueue(t, s, "a-input", "a", "leader", "", "")
	enqueue(t, s, "b-input", "b", "leader", "", "")
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	dispatcher := &testDispatcher{send: func(_ string, c agentproto.Command) (DispatchReceipt, error) {
		arrived <- struct{}{}
		<-release
		return DispatchReceipt{State: DispatchDelivered, TransportReceipt: "accepted:" + c.CommandID}, nil
	}}
	e := engine(t, s, 2, &testPlanner{}, dispatcher)
	result := make(chan TickResult, 1)
	failed := make(chan error, 1)
	go func() {
		r, err := e.Tick(context.Background())
		if err != nil {
			failed <- err
			return
		}
		result <- r
	}()
	for range 2 {
		select {
		case <-arrived:
		case err := <-failed:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("independent carriers were dispatched serially")
		}
	}
	close(release)
	r := <-result
	if len(r.Dispatched) != 2 {
		t.Fatalf("dispatch count=%d", len(r.Dispatched))
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		for _, dispatch := range r.Dispatched {
			attempt, err := tx.GetAttempt(dispatch.AttemptID)
			if err != nil {
				return err
			}
			if attempt.State != work.AttemptDispatchPending {
				t.Fatal("transport acceptance inferred running")
			}
			operation, err := tx.GetOperation(dispatch.OperationID)
			if err != nil {
				return err
			}
			if operation.State != work.OperationConfirmed || operation.RequestRef == "" {
				t.Fatal("command not durable or receipt missing")
			}
		}
		return nil
	})
}
func TestFairCursorSurvivesReopenAcrossScopesAndRoles(t *testing.T) {
	s, path := fixture(t, "a", "b")
	enqueue(t, s, "a-leader", "a", "leader", "", "")
	enqueue(t, s, "a-expert", "a", "expert", "", "")
	enqueue(t, s, "b-leader", "b", "leader", "", "")
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	e := engine(t, s, 1, planner, dispatcher)
	first, err := e.Tick(t.Context())
	if err != nil || len(first.Dispatched) != 1 {
		t.Fatalf("first tick %+v %v", first, err)
	}
	finish(t, s, first.Dispatched[0].AttemptID)
	reopened, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	e = engine(t, reopened, 1, planner, dispatcher)
	second, err := e.Tick(t.Context())
	if err != nil || len(second.Dispatched) != 1 {
		t.Fatalf("second tick %+v %v", second, err)
	}
	transaction(t, reopened, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(second.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != "b" {
			t.Fatalf("scope fairness lost on restart: %s", attempt.ScopeID)
		}
		return nil
	})
	finish(t, reopened, second.Dispatched[0].AttemptID)
	third, err := e.Tick(t.Context())
	if err != nil || len(third.Dispatched) != 1 {
		t.Fatalf("third tick %+v %v", third, err)
	}
	transaction(t, reopened, func(tx *workstore.Tx) error {
		attempt, err := tx.GetAttempt(third.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		if attempt.ScopeID != "a" {
			t.Fatal("remaining role starved")
		}
		return nil
	})
}
func TestIndependentTasksUseIndependentContextsAndCarriers(t *testing.T) {
	s, _ := fixture(t, "a")
	transaction(t, s, func(tx *workstore.Tx) error {
		for _, id := range []string{"first", "second"} {
			task := work.Task{ID: id, ScopeID: "a", Title: id, OwnerRoleID: "expert", RequirementsRevision: 1, AssignmentGeneration: 1, State: work.TaskTodo, Revision: 1}
			if err := tx.PutTask(task, 0); err != nil {
				return err
			}
			if err := putWorker(tx, "a", "expert", id, ":"+id); err != nil {
				return err
			}
		}
		return nil
	})
	enqueue(t, s, "first-input", "a", "expert", "first", "")
	enqueue(t, s, "second-input", "a", "expert", "second", "")
	e := engine(t, s, 2, &testPlanner{}, &testDispatcher{})
	r, err := e.Tick(t.Context())
	if err != nil || len(r.Dispatched) != 2 {
		t.Fatalf("parallel tasks %+v %v", r, err)
	}
	transaction(t, s, func(tx *workstore.Tx) error {
		a, err := tx.GetAttempt(r.Dispatched[0].AttemptID)
		if err != nil {
			return err
		}
		b, err := tx.GetAttempt(r.Dispatched[1].AttemptID)
		if err != nil {
			return err
		}
		if a.ContextID == b.ContextID || a.TaskID == b.TaskID {
			t.Fatal("independent workstreams collapsed")
		}
		return nil
	})
}
func TestConcurrentEnginesCannotExceedDurableSlots(t *testing.T) {
	s, path := fixture(t, "a", "b")
	enqueue(t, s, "one", "a", "leader", "", "")
	enqueue(t, s, "two", "b", "leader", "", "")
	second, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	planner := &testPlanner{}
	dispatcher := &testDispatcher{}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, handle := range []*workstore.Store{s, second} {
		wg.Add(1)
		go func(handle *workstore.Store) {
			defer wg.Done()
			_, err := engine(t, handle, 1, planner, dispatcher).Tick(context.Background())
			errs <- err
		}(handle)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if dispatcher.count() != 1 {
		t.Fatalf("durable slot overcommitted: sends=%d", dispatcher.count())
	}
}
func TestUnknownDispatchRetainsSlotAndReopensWithoutBlindSend(t *testing.T) {
	s, path := fixture(t, "a", "b")
	enqueue(t, s, "one", "a", "leader", "", "")
	enqueue(t, s, "two", "b", "leader", "", "")
	dispatcher := &testDispatcher{send: func(string, agentproto.Command) (DispatchReceipt, error) {
		return DispatchReceipt{State: DispatchUnknown}, errors.New("connection lost after write")
	}}
	planner := &testPlanner{}
	e := engine(t, s, 1, planner, dispatcher)
	first, err := e.Tick(t.Context())
	if err != nil || len(first.Dispatched) != 1 || first.Dispatched[0].State != work.OperationUnknown {
		t.Fatalf("unknown dispatch %+v %v", first, err)
	}
	reopened, err := workstore.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	e = engine(t, reopened, 1, planner, dispatcher)
	pending, err := e.ReconcilePending(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending %+v %v", pending, err)
	}
	if dispatcher.count() != 1 {
		t.Fatal("reconcile blindly resent command")
	}
	tick, err := e.Tick(t.Context())
	if err != nil || len(tick.Dispatched) != 0 {
		t.Fatalf("unknown freed execution slot %+v %v", tick, err)
	}
	dispatcher.send = func(_ string, c agentproto.Command) (DispatchReceipt, error) {
		return journalReceipt(c, pending[0].Worker.CarrierIncarnation), nil
	}
	replayed, err := e.Replay(t.Context(), pending[0].Operation.ID)
	if err != nil || replayed.Err != nil || replayed.State != work.OperationConfirmed {
		t.Fatalf("same ID replay %+v %v", replayed, err)
	}
	if dispatcher.count() != 2 {
		t.Fatal("explicit replay not sent")
	}
	dispatcher.mu.Lock()
	same := dispatcher.calls[0].command.CommandID == dispatcher.calls[1].command.CommandID
	dispatcher.mu.Unlock()
	if !same {
		t.Fatal("replay manufactured new command")
	}
}
