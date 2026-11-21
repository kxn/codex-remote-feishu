package workruntime

import (
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
	gates, err := tx.ListPauseGates(worker.ScopeID)
	if err != nil {
		return false, err
	}
	return work.GatesAllowRunForPurpose(worker.ScopeID, context.TaskID, context.ID, context.Purpose, gates)
}
