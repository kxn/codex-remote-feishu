package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// TaskAttemptSuperseded compares immutable execution requirements with the
// current task authority. The monotone generations remain a durable stop reason
// after restart; changing a title back cannot reactivate an older execution.
func (t *Tx) TaskAttemptSuperseded(attempt work.ExecutionAttempt) (bool, error) {
	if attempt.TaskID == "" {
		return false, nil
	}
	task, err := t.GetTask(attempt.TaskID)
	if err != nil {
		return false, err
	}
	if task.ScopeID != attempt.ScopeID || attempt.FrozenRequirementsRevision < 1 || attempt.FrozenAssignmentGeneration < 1 {
		return false, ErrScopeDenied
	}
	return task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskCancelled, nil
}

func (t *Tx) AttemptStopRequested(attempt work.ExecutionAttempt) (bool, error) {
	stopped, err := t.PrivateAttemptStopped(attempt)
	if err != nil || stopped {
		return stopped, err
	}
	return t.TaskAttemptSuperseded(attempt)
}

// Cancellation ends pending work without claiming that it was processed.
// Claimed work remains owned by its attempt until its actual stop is settled.
func (t *Tx) cancelPendingTaskInputs(task work.Task) error {
	if task.State != work.TaskCancelled {
		return ErrScopeDenied
	}
	inputs, err := listRecords[work.Inbox](t, "inbox", " WHERE scope_id=? AND task_id=? AND state='pending'", task.ScopeID, task.ID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		expected := input.Revision
		input.Revision++
		input.State = work.InboxCancelled
		input.ClaimedAttemptID = ""
		if err = t.PutInbox(input, expected); err != nil {
			return err
		}
	}
	return nil
}

// ReassignPendingTaskInputs preserves message/attachment provenance without
// mutating a previously frozen recipient. Claimed inputs transfer only after
// their old outcome is durably settled; they are not stolen from a live turn.
func (t *Tx) ReassignPendingTaskInputs(task work.Task, now time.Time) error {
	inputs, err := listRecords[work.Inbox](t, "inbox", " WHERE scope_id=? AND task_id=? AND state='pending' AND role_id<>?", task.ScopeID, task.ID, task.OwnerRoleID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if input.TaskID == task.ID && input.State == work.InboxPending && input.RecipientRoleID != task.OwnerRoleID {
			if err = t.reassignTaskInput(input, task, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tx) reassignTaskInput(input work.Inbox, task work.Task, now time.Time) error {
	if input.TaskID != task.ID || input.ScopeID != task.ScopeID || input.State != work.InboxPending || task.State == work.TaskCancelled {
		return ErrScopeDenied
	}
	identity, _ := json.Marshal([]any{input.ID, task.ID, task.AssignmentGeneration, task.OwnerRoleID})
	hash := sha256.Sum256(identity)
	id := "task_reassigned_" + hex.EncodeToString(hash[:20])
	successor := input
	successor.ID, successor.EventID = id, id
	successor.RecipientRoleID, successor.RecipientContextID = task.OwnerRoleID, ""
	successor.ClaimedAttemptID = ""
	successor.ReadAt = nil
	successor.ProcessedAt = nil
	successor.ProcessingOutcomeRef = ""
	successor.Revision = 1
	if successor.AutomationEpisodeID != "" || successor.SessionGoalIntentID != "" || successor.ControlOfInputID != "" || successor.ShellOperationID != "" {
		return fmt.Errorf("task input contains a nontransferable execution control")
	}
	proof, _ := json.Marshal(struct {
		OriginalInputID, SuccessorInputID, TaskID, PreviousRoleID, RoleID string
		AssignmentGeneration                                              int64
	}{input.ID, id, task.ID, input.RecipientRoleID, task.OwnerRoleID, task.AssignmentGeneration})
	if err := t.PutContent(work.NewContent(id+":provenance", task.ScopeID, "application/json", proof, now)); err != nil {
		return err
	}
	if err := t.PutInbox(successor, 0); err != nil {
		return err
	}
	before := input.Revision
	input.Revision++
	input.State = work.InboxCancelled
	input.ClaimedAttemptID = ""
	return t.PutInbox(input, before)
}
