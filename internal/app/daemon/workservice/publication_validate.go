package workservice

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ValidatePublication rechecks the original accepted authority against live
// records without borrowing credentials or the current state of another run.
func ValidatePublication(tx *workstore.Tx, op work.Operation, projection MessageProjection) error {
	if op.Actor.Kind == work.ActorSystem {
		return nil
	}
	authority := projection.Authority
	if op.Actor.Kind != work.ActorRole || authority == nil || op.Actor.ID != projection.RoleID ||
		op.TargetID != projection.ChatID || op.TargetGeneration != projection.BotGeneration {
		// Older role operations do not contain evidence of publication authority.
		return ErrPublicationDenied
	}
	scope, err := tx.GetScope(op.ScopeID)
	if err != nil {
		return publicationRecordError(err)
	}
	binding, err := tx.GetRoleBotBinding(projection.RoleID)
	if err != nil {
		return publicationRecordError(err)
	}
	if binding.IdentityGeneration != projection.BotGeneration || binding.AppID != projection.AppID || binding.GatewayRef != projection.GatewayRef {
		return ErrPublicationDenied
	}
	attempt, err := tx.GetAttempt(authority.AttemptID)
	if err != nil {
		return publicationRecordError(err)
	}
	if attempt.ScopeID != scope.ID || attempt.ContextID != op.ContextID || (attempt.State != work.AttemptRunning && attempt.State != work.AttemptCompleted) {
		return ErrPublicationDenied
	}
	wc, err := tx.GetContext(op.ContextID)
	if err != nil {
		return publicationRecordError(err)
	}
	if wc.ScopeID != scope.ID || wc.RoleID != projection.RoleID || wc.Purpose == work.PurposeReview {
		return ErrPublicationDenied
	}
	if attempt.TaskID != "" {
		task, err := tx.GetTask(attempt.TaskID)
		if err != nil {
			return publicationRecordError(err)
		}
		if task.ScopeID != scope.ID || task.OwnerRoleID != projection.RoleID || task.State == work.TaskCancelled ||
			task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.RequirementsRevision != attempt.FrozenRequirementsRevision {
			return ErrPublicationDenied
		}
	}
	if authority.TargetTask.TaskID != projection.TaskID {
		return ErrPublicationDenied
	}
	if err = validatePublicationTask(tx, scope, authority.TargetTask); err != nil {
		return err
	}
	switch authority.Kind {
	case publicationPrivate:
		if scope.Kind != work.ScopePersonal || scope.RoleID != projection.RoleID {
			return ErrPublicationDenied
		}
		destination, err := tx.GetPersonalContextDestination(scope.ID, op.ContextID, projection.RoleID)
		if err != nil {
			return publicationRecordError(err)
		}
		if destination.Tenant != projection.Tenant || destination.ChatID != projection.ChatID {
			return ErrPublicationDenied
		}
		return nil
	case publicationBootstrap:
		if scope.Kind != work.ScopeBootstrap || scope.RoleID != projection.RoleID || scope.Tenant != projection.Tenant ||
			scope.ChatID != projection.ChatID || scope.BindingGeneration != projection.ChannelGeneration {
			return ErrPublicationDenied
		}
		channel, err := tx.GetProjectChannelBinding(scope.Tenant, scope.ChatID)
		if err == nil && channel.Active {
			return ErrPublicationDenied
		}
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		return nil
	}
	if scope.Kind != work.ScopeProject {
		return ErrPublicationDenied
	}
	project, err := tx.GetProject(scope.ProjectID)
	if err != nil {
		return publicationRecordError(err)
	}
	if project.State == work.ProjectArchiving || project.State == work.ProjectArchived {
		return ErrPublicationDenied
	}
	current, err := publicationMember(tx, scope, projection.RoleID)
	if err != nil {
		return err
	}
	if current != authority.Participant {
		return ErrPublicationDenied
	}
	channel, err := tx.GetProjectChannelBinding(projection.Tenant, projection.ChatID)
	if err != nil {
		return publicationRecordError(err)
	}
	if !channel.Active || channel.Purpose != "main" || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != projection.ChannelGeneration {
		return ErrPublicationDenied
	}
	switch authority.Kind {
	case publicationLeader:
		if project.LeaderRoleID != projection.RoleID || project.LeadershipGeneration() != authority.LeadershipGeneration {
			return ErrPublicationDenied
		}
	case publicationDirected:
		if authority.Content != publicationMessage || projection.Purpose != PublicationReply {
			return ErrPublicationDenied
		}
		_, err = validateDirectedReply(tx, scope, attempt.ID, projection.RoleID, authority.InputID, projection)
		return err
	case publicationDelegated:
		grant, err := tx.GetPublicationGrant(scope.ID, authority.GrantID)
		if err != nil {
			return publicationRecordError(err)
		}
		return validateGrantPublication(tx, scope, grant, *authority, projection)
	default:
		return ErrPublicationDenied
	}
	return nil
}

func validatePublicationTask(tx *workstore.Tx, scope work.WorkScope, previous work.PublicationTaskAuthority) error {
	if previous.TaskID == "" {
		return nil
	}
	task, err := tx.GetTask(previous.TaskID)
	if err != nil {
		return publicationRecordError(err)
	}
	if task.ScopeID != scope.ID || task.State == work.TaskCancelled || taskAuthority(task) != previous {
		return ErrPublicationDenied
	}
	return nil
}
