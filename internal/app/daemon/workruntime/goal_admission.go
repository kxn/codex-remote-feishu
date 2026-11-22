package workruntime

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func nativeGoalAdmission(tx *workstore.Tx, worker work.WorkerSession) (bool, error) {
	context, err := tx.GetContext(worker.ContextID)
	if err != nil {
		return false, err
	}
	if context.Purpose == work.PurposeControl || context.Purpose == work.PurposeReview {
		return false, nil
	}
	attempt, err := tx.GetOccupiedAttemptForWorker(worker.ID)
	if err == nil {
		stopped, e := tx.PrivateAttemptStopped(attempt)
		if e != nil || stopped {
			return false, e
		}
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return false, err
	}
	gates, err := tx.ListPauseGates(worker.ScopeID)
	if err != nil {
		return false, err
	}
	return work.GatesAllowRunForPurpose(worker.ScopeID, context.TaskID, context.ID, context.Purpose, gates)
}
