package workstore

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestCancelledTaskCancelsPendingButWaitsForClaimedOutcome(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutTask(testTask("task", "r"), 0); err != nil {
			return err
		}
		for _, id := range []string{"input", "pending"} {
			input := testInput(id, "r", "c-r")
			input.TaskID = "task"
			if err := tx.PutInbox(input, 0); err != nil {
				return err
			}
		}
		return nil
	})
	claim := testClaim()
	claim.Attempt.TaskID, claim.Attempt.FrozenRequirementsRevision, claim.Attempt.FrozenAssignmentGeneration = "task", 1, 1
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		task.State, task.Revision = work.TaskCancelled, 2
		if err = tx.PutTask(task, 1); err != nil {
			return err
		}
		pending, err := tx.GetInbox("pending")
		if err != nil {
			return err
		}
		if pending.State != work.InboxCancelled || pending.ProcessedAt != nil {
			t.Fatalf("pending cancellation: %+v", pending)
		}
		claimed, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if claimed.State != work.InboxClaimed || claimed.ClaimedAttemptID != "attempt" {
			t.Fatalf("active input ownership lost: %+v", claimed)
		}
		stop, err := tx.AttemptStopRequested(claim.Attempt)
		if err == nil && !stop {
			t.Fatal("task cancellation did not request native stop")
		}
		return err
	})
	outcome := testOutcome(claim)
	outcome.ProcessedInputIDs, outcome.TerminalState = nil, work.AttemptInterrupted
	requireTx(t, s, func(tx *Tx) error { return tx.CommitOutcome(outcome) })
	requireTx(t, s, func(tx *Tx) error {
		input, err := tx.GetInbox("input")
		if err == nil && (input.State != work.InboxCancelled || input.ProcessedAt != nil) {
			t.Fatalf("cancelled task left phantom pending work: %+v", input)
		}
		return err
	})
}

func TestReassignedTaskRetainsPendingAndClaimedMessageProvenance(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	originalAt := time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)
	requireTx(t, s, func(tx *Tx) error {
		if err := tx.PutTask(testTask("task", "r"), 0); err != nil {
			return err
		}
		for _, id := range []string{"input", "pending"} {
			input := testInput(id, "r", "c-r")
			input.TaskID = "task"
			input.ReadAt = &originalAt
			input.AvailableAt = originalAt
			message := work.Message{ID: id + ":message", ScopeID: "s", ConversationID: "chat", Source: work.Actor{Kind: work.ActorPrincipal, ID: "p"}, BodyRef: "body", Revision: 1}
			if _, _, err := tx.AcceptMessage(message, []work.Inbox{input}); err != nil {
				return err
			}
		}
		return nil
	})
	claim := testClaim()
	claim.Attempt.TaskID, claim.Attempt.FrozenRequirementsRevision, claim.Attempt.FrozenAssignmentGeneration = "task", 1, 1
	requireTx(t, s, func(tx *Tx) error { return tx.ClaimAttempt(claim) })
	markRunning(t, s)
	requireTx(t, s, func(tx *Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		task.OwnerRoleID, task.AssignmentGeneration, task.Revision = "expert", 2, 2
		if err = tx.PutTask(task, 1); err != nil {
			return err
		}
		return tx.ReassignPendingTaskInputs(task, time.Now().UTC())
	})
	// Restart/replay must neither duplicate transfers nor steal a claimed input.
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		task, err := tx.GetTask("task")
		if err != nil {
			return err
		}
		if err = tx.ReassignPendingTaskInputs(task, time.Now().UTC()); err != nil {
			return err
		}
		input, err := tx.GetInbox("input")
		if err != nil {
			return err
		}
		if input.State != work.InboxClaimed || input.RecipientRoleID != "r" || input.ClaimedAttemptID != "attempt" {
			t.Fatalf("stole active input: %+v", input)
		}
		pending, err := tx.ListPendingInputs("s", "expert")
		if err == nil && len(pending) != 1 {
			t.Fatalf("want one pending transfer, got %+v", pending)
		}
		return err
	})
	outcome := testOutcome(claim)
	outcome.ProcessedInputIDs = nil
	outcome.TerminalState = work.AttemptInterrupted
	requireTx(t, reopened, func(tx *Tx) error { return tx.CommitOutcome(outcome) })
	requireTx(t, reopened, func(tx *Tx) error {
		if err := tx.CommitOutcome(outcome); err != nil {
			return err
		}
		pending, err := tx.ListPendingInputs("s", "expert")
		if err != nil {
			return err
		}
		if len(pending) != 2 {
			t.Fatalf("pending/claimed input transfer lost or repeated: %+v", pending)
		}
		seen := make(map[string]bool)
		for _, input := range pending {
			if input.RecipientContextID != "" || input.ClaimedAttemptID != "" || input.ProcessedAt != nil || input.ReadAt != nil || !input.AvailableAt.Equal(originalAt) {
				t.Fatalf("transferred stale execution: %+v", input)
			}
			message, err := tx.GetMessage(input.MessageID)
			if err != nil {
				return err
			}
			if message.Source.Kind != work.ActorPrincipal || message.Source.ID != "p" || message.BodyRef != "body" || seen[message.ID] {
				t.Fatalf("message provenance changed: %+v", message)
			}
			seen[message.ID] = true
			if _, err = tx.GetContent("s", input.ID+":provenance"); err != nil {
				return err
			}
		}
		for _, id := range []string{"input", "pending"} {
			original, err := tx.GetInbox(id)
			if err != nil {
				return err
			}
			if original.State != work.InboxCancelled || original.ProcessedAt != nil || original.RecipientRoleID != "r" {
				t.Fatalf("original falsely processed or rewritten: %+v", original)
			}
		}
		return nil
	})
}
