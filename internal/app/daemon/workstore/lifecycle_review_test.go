package workstore

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestReviewTaskCompletionRequiresSatisfiedDependencies(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		parent := testTask("parent", "r")
		child := testTask("child", "expert")
		if err := tx.PutTask(parent, 0); err != nil {
			return err
		}
		if err := tx.PutTask(child, 0); err != nil {
			return err
		}
		if err := tx.AddDependency(work.Dependency{TaskID: "parent", DependsOnTaskID: "child", Condition: work.DependencyIntegrated}); err != nil {
			return err
		}
		parent.State = work.TaskActive
		parent.Revision++
		if err := tx.PutTask(parent, 1); err != nil {
			return err
		}
		parent.State = work.TaskDone
		parent.Revision++
		if err := tx.PutTask(parent, 2); err == nil {
			t.Error("parent became done although required integrated dependency is unsatisfied")
		}
		return nil
	})
}

func TestReviewBusyWorkerCannotSilentlyReplaceProcessIncarnation(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error { return tx.PutInbox(testInput("input", "r", "c-r"), 0) })
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(testClaim()) })
	markRunning(t, s)
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		oldRevision := worker.Revision
		worker.Revision++
		worker.ProcessGeneration++
		if err := tx.PutWorker(worker, oldRevision); err == nil {
			t.Error("busy worker retained same run generation while switching process generation")
		}
		return nil
	})
}

func TestReviewLateTerminalOutcomeDoesNotReleaseSuccessorWorker(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	first := testClaim()
	requireTx(t, s, func(tx *Tx) error { return tx.PutInbox(testInput("input", "r", "c-r"), 0) })
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(first) })
	markRunning(t, s)
	// A terminal runtime receipt can precede the processing outcome.
	earlyReleaseBlocked := false
	requireTx(t, s, func(tx *Tx) error {
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		attempt.State = work.AttemptCompleted
		attempt.TerminalEvidence = []work.SourceRef{evidenceSource()}
		attempt.Revision++
		if err := tx.PutAttempt(attempt, 2); err != nil {
			return err
		}
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		oldRevision := worker.Revision
		worker.Revision++
		worker.State = work.WorkerReady
		if err := tx.PutWorker(worker, oldRevision); err != nil {
			earlyReleaseBlocked = true
		}
		return nil
	})
	if earlyReleaseBlocked {
		return
	} // The reservation correctly fences Ready early.
	var workerRevision int64
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		workerRevision = worker.Revision
		return tx.PutInbox(testInput("input-2", "r", "c-r"), 0)
	})
	second := testClaim()
	second.Attempt.ID = "attempt-2"
	second.Attempt.RunRef.RunID = "run-2"
	second.Attempt.InputIDs = []string{"input-2"}
	second.Attempt.CommandID = "command-2"
	second.StartOperation = testOperation("command-2")
	second.ExpectedWorkerRevision = workerRevision
	// The carrier reservation may correctly block a successor until the old
	// processing outcome is committed. That is also a safe implementation.
	if err := s.Within(context.Background(), func(tx *Tx) error { return tx.ClaimAttempt(second) }); err != nil {
		return
	}
	oldOutcome := testOutcome(first)
	oldOutcome.ExpectedAttemptRevision = 3
	oldOutcome.Checkpoint = "obsolete checkpoint"
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(oldOutcome); err != nil {
			return nil
		} // Safe rejection is allowed.
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerBusy {
			t.Errorf("old outcome changed worker to %s while successor attempt is active", worker.State)
		}
		context, err := tx.GetContext("c-r")
		if err != nil {
			return err
		}
		if context.Checkpoint == "obsolete checkpoint" {
			t.Error("old terminal outcome overwrote successor context checkpoint")
		}
		return nil
	})
}
