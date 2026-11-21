package workruntime

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A fast initial turn cannot release this explicit goal authorization before
// its canonical RPC is sent and answered. No attempt or run is invented.
func pendingSessionGoalIntent(tx *workstore.Tx, attemptID string) (bool, error) {
	attempt, err := tx.GetAttempt(attemptID)
	if err != nil {
		return false, err
	}
	for _, id := range attempt.InputIDs {
		input, err := tx.GetInbox(id)
		if err != nil {
			return false, err
		}
		if input.SessionGoalIntentID == "" {
			continue
		}
		intent, err := tx.GetSessionGoalIntent(input.SessionGoalIntentID)
		if err != nil {
			return false, err
		}
		if intent.CommandID == "" {
			return true, nil
		}
		op, err := tx.GetOperation(intent.CommandID)
		if err != nil {
			return false, err
		}
		if op.State == work.OperationPrepared || op.State == work.OperationSending || op.State == work.OperationUnknown {
			return true, nil
		}
	}
	return false, nil
}

func (c *Coordinator) prepareSessionGoalIntent(tx *workstore.Tx, attemptID, sessionID string) error {
	attempt, err := tx.GetAttempt(attemptID)
	if err != nil {
		return err
	}
	for _, id := range attempt.InputIDs {
		input, err := tx.GetInbox(id)
		if err != nil {
			return err
		}
		if input.SessionGoalIntentID != "" {
			_, err = tx.PrepareSessionGoalsForAttempt(attemptID, sessionID, c.options.Clock())
			return err
		}
	}
	return nil
}
