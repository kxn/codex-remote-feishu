package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) ValidateControlContext(context work.WorkContext) error {
	if context.Purpose != work.PurposeControl || context.TaskID != "" || context.ProjectViewScopeID != "" {
		return ErrScopeDenied
	}
	scope, err := t.GetScope(context.ScopeID)
	if err != nil {
		return err
	}

	if scope.Kind == work.ScopePersonal {
		if context.RoleID != scope.RoleID || context.ControlTargetContextID == "" {
			return ErrScopeDenied
		}
		target, err := t.GetContext(context.ControlTargetContextID)
		if err != nil {
			return err
		}
		if target.ScopeID != scope.ID || target.RoleID != scope.RoleID || target.Purpose == work.PurposeControl {
			return ErrScopeDenied
		}
		return nil
	}
	if scope.Kind != work.ScopeProject {
		return ErrScopeDenied
	}
	project, err := t.GetProject(scope.ProjectID)
	if err != nil {
		return err
	}
	if project.LeaderRoleID != context.RoleID || (project.State != work.ProjectActive && project.State != work.ProjectDegraded && project.State != work.ProjectPausing && project.State != work.ProjectPaused) {
		return ErrScopeDenied
	}
	member, err := t.GetProjectMember(project.ID, context.RoleID)
	if err != nil {
		return err
	}
	if member.State != work.ProjectMemberReady {
		return ErrScopeDenied
	}
	team, err := t.GetTeamMember(scope.TeamID, context.RoleID)
	if err != nil {
		return err
	}
	if !team.Active {
		return ErrScopeDenied
	}
	return nil
}
func (t *Tx) validateControlInput(input work.Inbox, expected int64) error {
	if input.RecipientContextID == "" {
		if input.ControlOfInputID != "" {
			return ErrScopeDenied
		}
		return nil
	}
	context, err := t.GetContext(input.RecipientContextID)
	if err != nil {
		return err
	}
	if context.Purpose != work.PurposeControl {
		if input.ControlOfInputID != "" {
			return ErrScopeDenied
		}
		return nil
	}
	if expected == 0 || input.State == work.InboxClaimed {
		if err = t.ValidateControlContext(context); err != nil {
			return err
		}
	}
	if input.ControlOfInputID == "" || input.ControlOfInputID == input.ID || input.TaskID != "" || input.MessageID == "" {
		return fmt.Errorf("control input requires original human business input")
	}
	original, err := t.GetInbox(input.ControlOfInputID)
	if err != nil {
		return err
	}
	if context.ControlTargetContextID != "" && original.RecipientContextID != context.ControlTargetContextID {
		return ErrScopeDenied
	}
	if original.ControlOfInputID != "" || original.ScopeID != input.ScopeID || original.MessageID != input.MessageID {
		return ErrScopeDenied
	}
	message, err := t.GetMessage(input.MessageID)
	if err != nil {
		return err
	}
	scope, err := t.GetScope(input.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopePersonal && message.Source.ID != scope.PrincipalID {
		return ErrScopeDenied
	}
	if message.Source.Kind != work.ActorPrincipal {
		return ErrScopeDenied
	}
	return nil
}
