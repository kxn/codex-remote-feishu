package workservice

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type PublicationDelegation struct {
	ID          string             `json:"id"`
	TaskID      string             `json:"task_id"`
	ContentKind string             `json:"content_kind"`
	References  []work.DeliveryRef `json:"references,omitempty"`
}

type PublicationCapabilities struct {
	Mode          string                  `json:"mode"`
	ReplyInputIDs []string                `json:"reply_input_ids,omitempty"`
	Delegations   []PublicationDelegation `json:"delegations,omitempty"`
	control       bool
}

type CommandDiscovery struct {
	Commands    []workcmd.Command       `json:"commands"`
	Publication PublicationCapabilities `json:"publication"`
}

func (s *Service) DiscoverCommands(ctx context.Context, token, runRef string) (CommandDiscovery, error) {
	var result CommandDiscovery
	if runRef == "" {
		claims, err := s.signer.Verify(token)
		if err != nil || claims.Purpose != "worker" {
			return result, workauth.ErrUnauthorized
		}
		err = s.store.Within(ctx, func(tx *workstore.Tx) error {
			attempt, err := tx.GetOccupiedAttemptForWorker(claims.WorkerID)
			if errors.Is(err, workstore.ErrNotFound) {
				return workauth.ErrUnauthorized
			}
			runRef = attempt.RunRef.RunID
			return err
		})
		if err != nil {
			return result, err
		}
	}
	apply := func(tx *workstore.Tx, c Caller) error {
		capabilities, err := s.publicationCapabilities(tx, c, false)
		if err != nil {
			return err
		}
		result.Publication = capabilities
		for _, cmd := range workcmd.List() {
			if capabilities.allowsCommand(cmd.Name) {
				result.Commands = append(result.Commands, cmd)
			}
		}
		return nil
	}
	err := s.withMessagePublisher(ctx, token, runRef, "", apply)
	if errors.Is(err, workauth.ErrUnauthorized) {
		var taskID string
		if e := s.withWorkerReceipt(ctx, token, runRef, func(_ *workstore.Tx, c Caller) error {
			taskID = c.Attempt.TaskID
			return nil
		}); e == nil && taskID != "" {
			err = s.withCompletedTaskPublication(ctx, token, runRef, taskID, apply)
		}
	}
	return result, err
}

func (v PublicationCapabilities) allowsCommand(name string) bool {
	switch name {
	case "message.publish":
		if v.Mode != "internal" || len(v.ReplyInputIDs) != 0 {
			return true
		}
		for _, grant := range v.Delegations {
			if grant.ContentKind == string(publicationMessage) {
				return true
			}
		}
	case "delivery.publish":
		if v.control {
			return false
		}
		if v.Mode == publicationLeader || v.Mode == publicationPrivate {
			return true
		}
		for _, grant := range v.Delegations {
			if grant.ContentKind == string(publicationDelivery) {
				return true
			}
		}
	case "media.publish":
		return v.Mode == publicationPrivate && !v.control
	case "publication.grant", "publication.revoke":
		return v.Mode == publicationLeader && !v.control
	default:
		return true
	}
	return false
}

func (s *Service) publicationCapabilities(tx *workstore.Tx, c Caller, planned bool) (PublicationCapabilities, error) {
	result := PublicationCapabilities{Mode: "internal"}
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return result, err
	}
	wc, err := tx.GetContext(c.Worker.ContextID)
	if err != nil {
		return result, err
	}
	result.control = wc.Purpose == work.PurposeControl
	if wc.Purpose == work.PurposeReview {
		return result, nil
	}
	switch scope.Kind {
	case work.ScopePersonal:
		result.Mode = publicationPrivate
	case work.ScopeBootstrap:
		result.Mode = publicationBootstrap
	case work.ScopeProject:
		project, err := tx.GetProject(scope.ProjectID)
		if err != nil {
			return result, err
		}
		if project.LeaderRoleID == c.Worker.RoleID {
			result.Mode = publicationLeader
			return result, nil
		}
		for _, inputID := range c.Attempt.InputIDs {
			input, err := tx.GetInbox(inputID)
			if err != nil {
				return result, err
			}
			if input.DirectedReplyIngressID == "" {
				continue
			}
			used, e := boundPublicationExists(tx, directedPublicationID(scope.ID, c.Worker.RoleID, inputID))
			if e != nil {
				return result, e
			}
			if used {
				continue
			}
			if planned && input.State == work.InboxPending {
				// WorkInput is built before ClaimAttempt. The ingress already
				// authenticated the recipient; actual CLI calls require the claim.
				binding, e := tx.GetRoleBotBinding(c.Worker.RoleID)
				if e != nil {
					return result, e
				}
				if input.ScopeID == scope.ID && input.RecipientRoleID == c.Worker.RoleID && input.DirectedReplyBotGeneration == binding.IdentityGeneration {
					result.ReplyInputIDs = append(result.ReplyInputIDs, inputID)
				}
				continue
			}
			_, err = s.authorizePublication(tx, c, inputID, "", "", publicationMessage, nil)
			if err == nil {
				result.ReplyInputIDs = append(result.ReplyInputIDs, inputID)
			} else if !errors.Is(err, ErrPublicationDenied) && !errors.Is(err, workauth.ErrUnauthorized) {
				return result, err
			}
		}
		grants, err := tx.ListPublicationGrants(scope.ID, c.Worker.RoleID)
		if err != nil {
			return result, err
		}
		for _, grant := range grants {
			if c.Attempt.TaskID != "" && c.Attempt.TaskID != grant.Task.TaskID {
				continue
			}
			used, e := boundPublicationExists(tx, delegatedPublicationOperationID(scope.ID, grant.ID, publicationKind(grant.ContentKind)))
			if e != nil {
				return result, e
			}
			if used {
				continue
			}
			err = validateGrant(tx, scope, grant)
			if err == nil {
				result.Delegations = append(result.Delegations, PublicationDelegation{grant.ID, grant.Task.TaskID, grant.ContentKind, grant.References})
			} else if !errors.Is(err, ErrPublicationDenied) {
				return result, err
			}
		}
	}
	return result, nil
}
