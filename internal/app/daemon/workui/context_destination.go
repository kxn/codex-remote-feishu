package workui

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ContextDestination also works before a first native attempt was admitted.
func ContextDestination(tx *workstore.Tx, contextID string) (workstore.UIView, error) {
	wc, err := tx.GetContext(contextID)
	if err != nil {
		return workstore.UIView{}, err
	}
	scope, err := tx.GetScope(wc.ScopeID)
	if err != nil {
		return workstore.UIView{}, err
	}
	if scope.Kind != work.ScopePersonal {
		return Destination(tx, scope.ID, wc.RoleID, "")
	}
	binding, err := tx.GetRoleBotBinding(wc.RoleID)
	if err != nil {
		return workstore.UIView{}, err
	}
	view := workstore.UIView{ScopeID: scope.ID, ContextID: wc.ID, PrincipalID: scope.PrincipalID, Binding: binding}
	return personalDestination(tx, view, scope, work.ExecutionAttempt{ScopeID: scope.ID, ContextID: wc.ID})
}
