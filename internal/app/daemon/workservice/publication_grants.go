package workservice

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type GrantPublicationRequest struct {
	Key           string             `json:"key"`
	GranteeRoleID string             `json:"grantee_role_id"`
	TaskID        string             `json:"task_id"`
	ContentKind   string             `json:"content_kind"`
	References    []work.DeliveryRef `json:"references,omitempty"`
}

type RevokePublicationRequest struct {
	Key     string `json:"key"`
	GrantID string `json:"grant_id"`
}

func (s *Service) GrantPublication(ctx context.Context, token, runRef string, req GrantPublicationRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, project, err := requirePublicationLeader(tx, c)
		if err != nil {
			return err
		}
		id := stableID("publication-grant", scope.ID, c.Worker.RoleID, req.Key)
		result, err = s.mutate(tx, c, "publication.grant", req.Key, id, req, func() (string, int64, error) {
			grantor, err := publicationMember(tx, scope, c.Worker.RoleID)
			if err != nil {
				return "", 0, err
			}
			grantee, err := publicationMember(tx, scope, req.GranteeRoleID)
			if err != nil {
				return "", 0, err
			}
			task, err := tx.GetTask(req.TaskID)
			if err != nil {
				return "", 0, publicationRecordError(err)
			}
			if task.ScopeID != scope.ID || task.OwnerRoleID != req.GranteeRoleID || task.State == work.TaskCancelled {
				return "", 0, ErrPublicationDenied
			}
			destination, err := s.messageDestination(tx, c, "", task.ID)
			if err != nil {
				return "", 0, err
			}
			grant := work.PublicationGrant{
				ID: id, ScopeID: scope.ID, LeadershipGeneration: project.LeadershipGeneration(),
				Grantor: grantor, Grantee: grantee, Task: taskAuthority(task),
				Tenant: destination.Tenant, ChatID: destination.ChatID,
				ChannelGeneration: destination.ChannelGeneration, RootMessageID: destination.RootMessageID,
				ContentKind: req.ContentKind, References: req.References, Revision: 1,
			}
			if err = grant.Validate(); err != nil {
				return "", 0, publicationArgument(err.Error())
			}
			for _, ref := range grant.References {
				version, err := tx.GetResourceVersion(scope.ID, ref.VersionID)
				if err != nil {
					return "", 0, publicationRecordError(err)
				}
				if version.ResourceID != ref.ResourceID || !version.Saved || !version.Complete || version.ContentRef == "" {
					return "", 0, publicationArgument("Authorize only complete, saved versions of the listed resources.")
				}
			}
			return id, 1, tx.CreatePublicationGrant(grant)
		})
		return err
	})
	return result, err
}

func (s *Service) RevokePublication(ctx context.Context, token, runRef string, req RevokePublicationRequest) (Result, error) {
	var result Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		scope, _, err := requirePublicationLeader(tx, c)
		if err != nil {
			return err
		}
		result, err = s.mutate(tx, c, "publication.revoke", req.Key, req.GrantID, req, func() (string, int64, error) {
			grant, err := tx.RevokePublicationGrant(scope.ID, req.GrantID)
			return grant.ID, grant.Revision, publicationRecordError(err)
		})
		return err
	})
	return result, err
}

func requirePublicationLeader(tx *workstore.Tx, c Caller) (work.WorkScope, work.Project, error) {
	scope, err := tx.GetScope(c.Worker.ScopeID)
	var project work.Project
	if err != nil {
		return scope, project, err
	}
	if scope.Kind != work.ScopeProject {
		return scope, project, ErrPublicationDenied
	}
	project, err = tx.GetProject(scope.ProjectID)
	if err != nil {
		return scope, project, err
	}
	if project.LeaderRoleID != c.Worker.RoleID {
		return scope, project, ErrPublicationDenied
	}
	return scope, project, nil
}

func validateGrant(tx *workstore.Tx, scope work.WorkScope, grant work.PublicationGrant) error {
	if grant.Revoked || grant.ScopeID != scope.ID {
		return ErrPublicationDenied
	}
	project, err := tx.GetProject(scope.ProjectID)
	if err != nil {
		return publicationRecordError(err)
	}
	if project.LeaderRoleID != grant.Grantor.RoleID || project.LeadershipGeneration() != grant.LeadershipGeneration {
		return ErrPublicationDenied
	}
	for _, previous := range []work.PublicationMemberAuthority{grant.Grantor, grant.Grantee} {
		current, err := publicationMember(tx, scope, previous.RoleID)
		if err != nil {
			return err
		}
		if current != previous {
			return ErrPublicationDenied
		}
	}
	if err := validatePublicationTask(tx, scope, grant.Task); err != nil {
		return err
	}
	channel, err := tx.GetProjectChannelBinding(grant.Tenant, grant.ChatID)
	if err != nil {
		return publicationRecordError(err)
	}
	if !channel.Active || channel.Purpose != "main" || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != grant.ChannelGeneration {
		return ErrPublicationDenied
	}
	if grant.RootMessageID != "" {
		conversation, err := tx.GetTaskConversation(scope.ID, grant.Task.TaskID)
		if err != nil {
			return publicationRecordError(err)
		}
		if conversation.RootMessageID != grant.RootMessageID {
			return ErrPublicationDenied
		}
	}
	return nil
}

func validateGrantPublication(tx *workstore.Tx, scope work.WorkScope, grant work.PublicationGrant, authority PublicationAuthority, dest MessageProjection) error {
	if authority.GrantRevision != grant.Revision || authority.Participant != grant.Grantee ||
		authority.TargetTask != grant.Task || authority.InputID != "" || string(authority.Content) != grant.ContentKind ||
		dest.Tenant != grant.Tenant || dest.ChatID != grant.ChatID || dest.ChannelGeneration != grant.ChannelGeneration ||
		(grant.RootMessageID != "" && dest.RootMessageID != grant.RootMessageID) {
		return ErrPublicationDenied
	}
	if authority.Content == publicationDelivery && len(authority.References) == 0 {
		return ErrPublicationDenied
	}
	for _, ref := range authority.References {
		if !slices.Contains(grant.References, ref) {
			return fmt.Errorf("%w: use only the saved versions in the delegation", ErrPublicationDenied)
		}
	}
	return validateGrant(tx, scope, grant)
}

func delegatedDeliveryID(scopeID, grantID string) string {
	return stableID("delivery-delegated", scopeID, grantID)
}

func delegatedPublicationOperationID(scopeID, grantID string, kind publicationKind) string {
	if kind == publicationDelivery {
		return stableID("delivery-notification", delegatedDeliveryID(scopeID, grantID))
	}
	return stableID("publication-delegated", scopeID, grantID)
}

func boundPublicationExists(tx *workstore.Tx, operationID string) (bool, error) {
	_, err := tx.GetOperation(operationID)
	if errors.Is(err, workstore.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
