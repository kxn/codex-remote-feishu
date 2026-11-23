package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) validatePrivateShellInput(input work.Inbox, expected int64) error {
	old := work.Inbox{}
	if expected > 0 {
		var err error
		old, err = t.GetInbox(input.ID)
		if err != nil {
			return err
		}
	}
	if old.ShellOperationID == "" && input.ShellOperationID == "" {
		return nil
	}
	id := input.ShellOperationID
	if id == "" {
		id = old.ShellOperationID
	}
	op, err := t.GetPrivateShellOperation(id)
	if err != nil {
		return err
	}
	if op.ScopeID != input.ScopeID || op.ContextID != input.RecipientContextID || input.ClaimedAttemptID != "" || input.State == work.InboxClaimed {
		return ErrScopeDenied
	}
	found := false
	for _, item := range op.InputIDs {
		found = found || item == input.ID
	}
	if !found {
		return ErrScopeDenied
	}
	if old.ShellOperationID != "" && old.ShellOperationID != input.ShellOperationID {
		if input.ShellOperationID != "" || op.State != "rejected" || (input.State != work.InboxPending && input.State != work.InboxCancelled) {
			return ErrScopeDenied
		}
	}
	if old.ShellOperationID == "" && input.ShellOperationID != "" && (expected == 0 || old.State != work.InboxPending || input.State != work.InboxPending || op.State != "sending") {
		return ErrScopeDenied
	}
	if input.State == work.InboxProcessed && (op.State != "completed" || op.ReceiptRef == "" || input.ProcessingOutcomeRef != op.ReceiptRef) {
		return ErrScopeDenied
	}
	return nil
}

func (t *Tx) privateShellSendAllowed(v PrivateShellOperation) error {
	attempt, err := t.GetAttempt(v.FrozenExecution.AttemptID)
	if err != nil {
		return err
	}
	stopped, err := t.PrivateAttemptStopped(attempt)
	if err != nil {
		return err
	}
	if stopped {
		return ErrScopeDenied
	}
	if err = t.AssertMaintenanceResourceAvailable("worker:" + v.Execution.WorkerSessionID); err != nil {
		return err
	}
	gates, err := t.ListPauseGates(v.ScopeID)
	if err != nil {
		return err
	}
	wc, err := t.GetContext(v.ContextID)
	if err != nil {
		return err
	}
	allowed, err := work.GatesAllowRunForPurpose(v.ScopeID, "", v.ContextID, wc.Purpose, gates)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrScopeDenied
	}
	if _, err = t.GetRuntimeGate(v.Execution.WorkerSessionID); err == nil {
		return ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}
