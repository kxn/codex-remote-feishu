package workstore

import (
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// These independent regression cases express required safety invariants. A
// failure identifies an observed gap; they do not change author implementation.
func TestReviewClaimFencesSharedCarrierAcrossWorkers(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutInbox(testInput("input", "r", "c-r"), 0); err != nil {
			return err
		}
		if err := tx.PutInbox(testInput("input-2", "expert", "c-expert"), 0); err != nil {
			return err
		}
		return tx.PutWorker(work.WorkerSession{ID: "w-2", ContextID: "c-expert", ScopeID: "s", RoleID: "expert", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "carrier", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0)
	})
	first := testClaim()
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(first) })
	second := testClaim()
	second.Attempt.ID = "attempt-2"
	second.Attempt.ContextID = "c-expert"
	second.Attempt.RunRef = work.RunRef{WorkerID: "w-2", Generation: 1, RunID: "run-2"}
	second.Attempt.InputIDs = []string{"input-2"}
	second.Attempt.CommandID = "command-2"
	second.StartOperation = testOperation("command-2")
	second.StartOperation.Actor.ID = "expert"
	second.StartOperation.ContextID = "c-expert"
	second.StartOperation.TargetID = "w-2"
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.ClaimAttempt(second); err == nil {
			t.Error("two active attempts admitted onto the same carrier/process incarnation")
		}
		return nil
	})
}

func TestReviewClaimDoesNotReferenceAnUncreatedReplayCommand(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	claim := testClaim()
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutInbox(testInput("input", "r", "c-r"), 0); err != nil {
			return err
		}
		old := claim.StartOperation
		old.ID = "original-command"
		_, _, err := tx.PutOperation(old, 0)
		return err
	})
	requireTx(t, s, func(tx *Tx) error {
		err := tx.ClaimAttempt(claim)
		if err != nil {
			return nil
		} // Rejection is safe; a service may resolve the replay.
		attempt, err := tx.GetAttempt(claim.Attempt.ID)
		if err != nil {
			return err
		}
		_, err = tx.GetOperation(attempt.CommandID)
		if errors.Is(err, ErrNotFound) {
			t.Error("accepted attempt references a command operation that idempotency replay never inserted")
			return nil
		}
		return err
	})
}

func TestReviewRemovedHistoricalRequirementDoesNotBlockCompletion(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task := reviewedTask()
		if err := tx.PutTask(task, 0); err != nil {
			return err
		}
		if err := tx.PutEvidence(reviewEvidence()); err != nil {
			return err
		}
		task.Requirements = nil
		task.RequirementsRevision++
		task.Revision++
		if err := tx.PutTask(task, 1); err != nil {
			return err
		}
		task.State = work.TaskActive
		task.Revision++
		if err := tx.PutTask(task, 2); err != nil {
			return err
		}
		task.State = work.TaskDone
		task.Revision++
		if err := tx.PutTask(task, 3); err != nil {
			t.Errorf("historical removed requirement permanently blocks valid completion: %v", err)
		}
		return nil
	})
}
