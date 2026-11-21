package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"time"
)

func (t *Tx) authorizePrivateInputAdmission(source PrivateActionSource, attempt work.ExecutionAttempt, now time.Time) (work.WorkScope, work.SourceRef, error) {
	var empty work.WorkScope
	if source.MessageID != "" {
		return empty, work.SourceRef{}, ErrScopeDenied
	}
	scope, proof, err := t.ValidatePrivateAction(source)
	if err != nil {
		return scope, proof, err
	}
	contextID := ""
	switch {
	case source.CommandID != "":
		command, e := t.GetPrivateCommand(source.CommandID)
		if e != nil {
			return scope, proof, e
		}
		fields := strings.Fields(command.Text)
		if len(fields) != 1 || fields[0] != "/steerall" {
			return scope, proof, ErrScopeDenied
		}
		contextID = command.ContextID
	case source.MenuInteractionID != "":
		menu, e := t.GetMenuInteraction(source.MenuInteractionID)
		if e != nil {
			return scope, proof, e
		}
		if menu.Envelope.EventKey != "steerall" && menu.Envelope.EventKey != "steer_all" {
			return scope, proof, ErrScopeDenied
		}
		contextID = menu.ContextID
	case source.InteractionID != "":
		interaction, e := t.GetHumanInteraction(source.InteractionID)
		if e != nil {
			return scope, proof, e
		}
		if interaction.Envelope.Values["action"] != "private.steerall" {
			return scope, proof, ErrScopeDenied
		}
		viewID, _ := interaction.Envelope.Values["team_view"].(string)
		view, e := t.GetUIView(viewID)
		if e != nil {
			return scope, proof, e
		}
		contextID = view.ContextID
	default:
		return scope, proof, ErrScopeDenied
	}
	front, err := t.GetContext(contextID)
	if err != nil {
		return scope, proof, err
	}
	if front.ScopeID != scope.ID || front.RoleID != scope.RoleID || front.Purpose == work.PurposeReview || front.Purpose == work.PurposeControl {
		return scope, proof, ErrScopeDenied
	}
	if attempt.ScopeID == scope.ID && attempt.ContextID == front.ID {
		return scope, proof, nil
	}
	if front.ProjectViewScopeID != attempt.ScopeID || front.ProjectContextID != attempt.ContextID {
		return scope, proof, ErrScopeDenied
	}
	if err = t.RequireProjectPrincipalAccess(attempt.ScopeID, scope.PrincipalID, now); err != nil {
		return scope, proof, err
	}
	return scope, proof, nil
}
