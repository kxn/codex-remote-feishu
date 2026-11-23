package workrequests

import (
	"database/sql"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A request has its own surface; it never borrows the business/menu card.
// Unknown sends remain frozen. Confirmed personal cards already prove their
// destination, including after their original turn or input has ended.
func requestPresentationDestination(tx *workstore.Tx, request workstore.NativeRequest, desiredViewID string) (workstore.UIView, bool, error) {
	var view workstore.UIView
	binding, err := tx.GetRoleBotBinding(request.Binding.RoleID)
	if err != nil {
		return view, false, err
	}
	if binding.IdentityGeneration != request.Binding.BotGeneration {
		return view, true, nil
	}
	previous, err := tx.CurrentNativeRequestView(request.ScopeID, request.ID, "request:"+request.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return view, false, err
	}
	known := err == nil
	if known {
		operation, e := tx.GetOperation(previous.OperationID)
		if e != nil {
			return view, false, e
		}
		switch operation.State {
		case work.OperationSending, work.OperationUnknown, work.OperationRetryable:
			return view, true, nil
		}
		if previous.ID == desiredViewID && previous.TargetRevision == request.Revision && previous.Binding == binding {
			return view, true, nil
		}
		scope, e := tx.GetScope(request.ScopeID)
		if e != nil {
			return view, false, e
		}
		if scope.Kind == work.ScopePersonal && operation.State == work.OperationConfirmed && previous.ExternalMessageID != "" && previous.Binding == binding && previous.PrincipalID == scope.PrincipalID && previous.ContextID == request.Binding.ContextID {
			view = workstore.UIView{ScopeID: scope.ID, ContextID: request.Binding.ContextID, Binding: binding, Tenant: previous.Tenant, ChatID: previous.ChatID, PrincipalID: scope.PrincipalID, ReplacesViewID: previous.ID}
			return view, false, nil
		}
	}
	view, err = workui.Destination(tx, request.ScopeID, request.Binding.RoleID, request.Binding.AttemptID)
	if err != nil {
		return view, false, err
	}
	if known {
		operation, e := tx.GetOperation(previous.OperationID)
		if e != nil {
			return view, false, e
		}
		if operation.State == work.OperationConfirmed && previous.ExternalMessageID != "" && previous.ScopeID == view.ScopeID && previous.ContextID == request.Binding.ContextID && previous.Binding == view.Binding && previous.ChatID == view.ChatID && previous.Tenant == view.Tenant && previous.PrincipalID == view.PrincipalID && previous.ChannelGeneration == view.ChannelGeneration {
			view.ReplacesViewID = previous.ID
		}
	}
	return view, false, nil
}
