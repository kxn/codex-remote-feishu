package workui

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Destination uses durable project bindings or the admitted personal source.
// It never chooses a chat from the latest visible native session.
func Destination(tx *workstore.Tx, scopeID, roleID, attemptID string) (workstore.UIView, error) {
	view := workstore.UIView{ScopeID: scopeID}
	var err error
	view.Binding, err = tx.GetRoleBotBinding(roleID)
	if err != nil {
		return view, err
	}
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return view, err
	}
	if scope.Kind == work.ScopeBootstrap {
		view.Tenant = scope.Tenant
		view.ChatID = scope.ChatID
		view.ChannelGeneration = scope.BindingGeneration
		view.PrincipalID = scope.PrincipalID
		return view, nil
	}
	if scope.Kind == work.ScopeProject {
		channels, e := tx.ListProjectChannelBindings(scope.ProjectID)
		if e != nil {
			return view, e
		}
		for _, channel := range channels {
			if channel.Active && channel.Purpose == "main" {
				if view.ChatID != "" {
					return view, fmt.Errorf("ambiguous project destination")
				}
				view.ChatID = channel.ChatID
				view.Tenant = channel.Tenant
				view.ChannelGeneration = channel.BindingGeneration
			}
		}
		if view.ChatID == "" {
			return view, fmt.Errorf("project destination unavailable")
		}
		return view, nil
	}
	if scope.Kind != work.ScopePersonal {
		return view, fmt.Errorf("unsupported UI destination")
	}
	view.PrincipalID = scope.PrincipalID
	attempt, err := tx.GetAttempt(attemptID)
	if err != nil {
		return view, err
	}
	if attempt.ScopeID != scopeID {
		return view, workstore.ErrScopeDenied
	}
	view.ContextID = attempt.ContextID
	return personalDestination(tx, view, scope, attempt)
}
