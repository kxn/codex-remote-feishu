package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
)

func TestDelegateRollbackAndDependencyCycles(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutTask(testTask("parent", "r"), 0); err != nil {
			return err
		}
		bad := testInput("bad", "expert", "")
		bad.RecipientContextID = "c-r"
		err := tx.Delegate(testTask("child", "expert"), bad, work.Dependency{TaskID: "parent", DependsOnTaskID: "child", Condition: work.DependencyResultAvailable})
		if err == nil {
			t.Fatal("bad delegation accepted")
		}
		if _, err = tx.GetTask("child"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("partial child committed: %v", err)
		}
		return nil
	})
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.Delegate(testTask("child", "expert"), testInput("delegate", "expert", ""), work.Dependency{TaskID: "parent", DependsOnTaskID: "child", Condition: work.DependencyResultAvailable}); err != nil {
			return err
		}
		if err := tx.AddDependency(work.Dependency{TaskID: "child", DependsOnTaskID: "parent", Condition: work.DependencyIntegrated}); err == nil {
			t.Fatal("cycle accepted")
		}
		return nil
	})
}
func TestClaimOutcomeAtomicityAndReplay(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error { return tx.PutInbox(testInput("input", "r", ""), 0) })
	claim := testClaim()
	claim.Attempt.InputIDs = append(claim.Attempt.InputIDs, "missing")
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.ClaimAttempt(claim); err == nil {
			t.Fatal("missing input accepted")
		}
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady || worker.Revision != 1 {
			t.Fatal("failed claim consumed worker")
		}
		if _, err = tx.GetOperation("command"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("failed claim leaked start operation: %v", err)
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatal("failed claim consumed input")
		}
		return nil
	})
	claim.Attempt.InputIDs = []string{"input"}
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	requireTx(t, s, func(tx *Tx) error {
		attempt, err := tx.GetAttemptByRunRef("run")
		if err != nil {
			return err
		}
		if attempt.ID != "attempt" {
			t.Fatal("run lookup mismatch")
		}
		return tx.PutInbox(testInput("late", "r", "c-r"), 0)
	})
	markRunning(t, s)
	outcome := testOutcome(claim)
	outcome.ProcessedInputIDs = append(outcome.ProcessedInputIDs, "late")
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); err == nil {
			t.Fatal("late input processed")
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxClaimed {
			t.Fatal("failed outcome lost input")
		}
		return nil
	})
	outcome.ProcessedInputIDs = []string{"input"}
	requireTx(t, s, func(tx *Tx) error { return tx.CommitOutcome(outcome) })
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); err != nil {
			return err
		}
		outcome.Checkpoint = "different"
		if err := tx.CommitOutcome(outcome); !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("changed outcome replay: %v", err)
		}
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady {
			t.Fatal("quiescent worker not released")
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxProcessed {
			t.Fatal("actual input not processed")
		}
		late, err := tx.GetInbox("late")
		if err != nil {
			return err
		}
		if late.State != work.InboxPending {
			t.Fatal("late input lost")
		}
		return nil
	})
}
func TestClaimRejectsGateAndStaleCarrier(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutInbox(testInput("input", "r", ""), 0); err != nil {
			return err
		}
		return tx.PutPauseGate(work.PauseGate{ID: "pause", ScopeID: "s", Source: work.GateProject, SourceID: "project", State: work.GateClosed, Revision: 1}, 0)
	})
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.ClaimAttempt(testClaim()); err == nil {
			t.Fatal("paused scope admitted")
		}
		gate, err := tx.GetPauseGate("pause")
		if err != nil {
			return err
		}
		gate.State = work.GateOpen
		gate.Revision++
		if err = tx.PutPauseGate(gate, 1); err != nil {
			return err
		}
		claim := testClaim()
		claim.ProcessGeneration = 2
		if err = tx.ClaimAttempt(claim); err == nil {
			t.Fatal("stale carrier admitted")
		}
		return nil
	})
}
func TestLocationOccupancyRequiresQuiescentEvidence(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutLocation(work.ExecutionLocation{ID: "location", HostID: "host", Cwd: "/repo", ResourceKeys: []string{"repo:main"}, Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		worker.LocationID = "location"
		worker.LocationGeneration = 1
		worker.Generation = 2
		worker.Revision = 2
		if err = tx.PutWorker(worker, 1); err != nil {
			return err
		}
		return tx.PutInbox(testInput("input", "r", ""), 0)
	})
	claim := testClaim()
	claim.Attempt.RunRef.Generation = 2
	claim.ExpectedWorkerRevision = 2
	claim.LocationGeneration = 1
	claim.StartOperation.TargetGeneration = 2
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	outcome := testOutcome(claim)
	outcome.Quiescent = false
	requireTx(t, s, func(tx *Tx) error { return tx.CommitOutcome(outcome) })
	requireTx(t, s, func(tx *Tx) error {
		worker, err := tx.GetWorker("w")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerRecovering {
			t.Fatal("uncertain process marked ready")
		}
		location, err := tx.GetLocation("location")
		if err != nil {
			return err
		}
		location.Generation++
		location.Revision++
		location.Cwd = "/other"
		if err = tx.PutLocation(location, 1); err == nil {
			t.Fatal("occupied location changed")
		}
		return nil
	})
}
