package workui

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func personalDestination(tx *workstore.Tx, view workstore.UIView, scope work.WorkScope, attempt work.ExecutionAttempt) (workstore.UIView, error) {
	destination, err := tx.GetPersonalContextDestination(scope.ID, attempt.ContextID, scope.RoleID)
	if err != nil {
		return view, err
	}
	view.Tenant, view.ChatID = destination.Tenant, destination.ChatID
	return view, nil
}
