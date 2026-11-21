package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type ShareProjectDocumentRequest struct {
	ResourceID      string `json:"resource_id"`
	SourceMessageID string `json:"source_message_id,omitempty"`
}

type projectDocumentShare struct {
	Binding       work.RoleBotBinding        `json:"binding"`
	Channel       work.ProjectChannelBinding `json:"channel"`
	ResourceID    string                     `json:"resource_id"`
	DocumentID    feishu.ProjectDocumentID   `json:"document_id"`
	RestoreSource *work.SourceRef            `json:"restore_source,omitempty"`
}

type projectDocumentSharer interface {
	ProjectDocumentGroupPermission(context.Context, feishu.ProjectDocumentID, string) (string, error)
	GrantProjectDocumentGroupView(context.Context, feishu.ProjectDocumentID, string) error
}

type projectDocumentAccessTarget struct {
	intent   projectDocumentShare
	resource work.Resource
	creator  work.Operation
	gateway  projectDocumentSharer
	base     string
}

type projectDocumentPermissionReadError struct{ error }

func (s *Service) ShareProjectDocument(ctx context.Context, token, run string, req ShareProjectDocumentRequest) (Result, error) {
	var target projectDocumentAccessTarget
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, caller Caller) error {
		resource, err := tx.GetResource(caller.Worker.ScopeID, req.ResourceID)
		if err != nil {
			return err
		}
		projection, err := s.messageDestination(tx, caller, "", "")
		if err != nil {
			return err
		}
		target, err = s.projectDocumentAccessTarget(ctx, tx, resource, projection)
		if err != nil {
			return err
		}
		return authorizeProjectDocumentAccessManager(tx, caller, target)
	})
	if err != nil {
		return Result{}, err
	}
	perm, readErr := s.observeProjectDocumentAccess(ctx, target)
	if readErr != nil {
		return Result{Status: "project_access_unavailable", ObjectID: req.ResourceID}, readErr
	}
	var op work.Operation
	var status string
	send := false
	err = s.withWorker(ctx, token, run, func(tx *workstore.Tx, caller Caller) error {
		if err := authorizeProjectDocumentAccessManager(tx, caller, target); err != nil {
			return err
		}
		intent := target.intent
		channel, err := tx.GetProjectChannelBinding(intent.Channel.Tenant, intent.Channel.ChatID)
		binding, bindingErr := tx.GetRoleBotBinding(intent.Binding.RoleID)
		if err != nil || bindingErr != nil || channel != intent.Channel || binding != intent.Binding {
			return workstore.ErrConflict
		}
		op, err = tx.LatestProjectDocumentShare(caller.Worker.ScopeID, target.base)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if op.State == work.OperationUnknown || op.State == work.OperationSending {
			return nil
		}
		if op.State == work.OperationConfirmed && perm != "" {
			return nil
		}
		if op.State == work.OperationConfirmed && req.SourceMessageID == "" {
			status = "project_access_revoked"
			return nil
		}
		if op.ID == "" || op.State == work.OperationConfirmed {
			id := target.base
			if op.State == work.OperationConfirmed {
				message, err := s.authorizeProjectDocumentRestore(tx, caller, channel, req.SourceMessageID)
				if err != nil {
					return err
				}
				if !message.CreatedAt.After(op.UpdatedAt) {
					return workstore.ErrScopeDenied
				}
				id += ":restore:" + stableID("source", message.ID)
				source := humanSource(message)
				intent.RestoreSource = &source
			}
			op, err = s.prepareProjectDocumentShare(tx, target, caller, intent, id)
			if err != nil {
				return err
			}
		}
		if perm != "" {
			return nil
		}
		if op.ID != target.base && intent.RestoreSource == nil {
			body, err := tx.GetContent(op.ScopeID, op.RequestRef)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(body.Data, &intent); err != nil {
				return err
			}
			if intent.RestoreSource == nil || req.SourceMessageID != intent.RestoreSource.ID {
				status = "project_access_revoked"
				return nil
			}
			message, err := s.authorizeProjectDocumentRestore(tx, caller, channel, req.SourceMessageID)
			if err != nil {
				return err
			}
			if humanSource(message) != *intent.RestoreSource {
				return workstore.ErrConflict
			}
		}
		op.State = work.OperationSending
		op.UpdatedAt = s.now()
		op.Revision++
		_, _, err = tx.PutOperation(op, op.Revision-1)
		send = err == nil
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if perm != "" {
		return s.finishProjectDocumentShare(ctx, op, perm, nil)
	}
	if !send {
		if status == "" {
			status = "pending_external"
		}
		return Result{Status: status, ObjectID: req.ResourceID, OperationID: op.ID, Revision: op.Revision}, nil
	}
	intent := target.intent
	callErr := target.gateway.GrantProjectDocumentGroupView(ctx, intent.DocumentID, intent.Channel.ChatID)
	if callErr == nil {
		perm, callErr = s.observeProjectDocumentAccess(ctx, target)
	}
	return s.finishProjectDocumentShare(ctx, op, perm, callErr)
}

func authorizeProjectDocumentAccessManager(tx *workstore.Tx, caller Caller, target projectDocumentAccessTarget) error {
	if caller.Worker.ScopeID != target.resource.ScopeID {
		return workstore.ErrScopeDenied
	}
	if target.creator.Actor == (work.Actor{Kind: work.ActorRole, ID: caller.Worker.RoleID}) {
		return nil
	}
	project, err := tx.GetProject(target.intent.Channel.ProjectID)
	if err != nil {
		return err
	}
	if project.ScopeID != target.resource.ScopeID || project.LeaderRoleID != caller.Worker.RoleID {
		return workstore.ErrScopeDenied
	}
	return nil
}

func (s *Service) authorizeProjectDocumentRestore(tx *workstore.Tx, caller Caller, channel work.ProjectChannelBinding, sourceID string) (work.Message, error) {
	message, _, err := currentHumanInstruction(tx, caller, sourceID)
	if err != nil {
		return work.Message{}, err
	}
	if message.PlatformKey == nil || message.PlatformKey.Tenant != channel.Tenant || message.PlatformKey.ChatID != channel.ChatID {
		return work.Message{}, workstore.ErrScopeDenied
	}
	if err = tx.RequireProjectPrincipalAccess(caller.Worker.ScopeID, message.Source.ID, s.now()); err != nil {
		return work.Message{}, err
	}
	return message, nil
}

func (s *Service) prepareProjectDocumentShare(tx *workstore.Tx, target projectDocumentAccessTarget, caller Caller, intent projectDocumentShare, id string) (work.Operation, error) {
	raw, err := json.Marshal(intent)
	if err != nil {
		return work.Operation{}, err
	}
	resource := target.resource
	body := work.NewContent(id+":request", resource.ScopeID, "application/json", raw, s.now())
	if err = tx.PutContent(body); err != nil {
		return work.Operation{}, err
	}
	op := work.Operation{ID: id, ScopeID: resource.ScopeID, ContextID: caller.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: caller.Worker.RoleID}, Kind: workstore.ProjectDocumentShareKind, TargetID: resource.ID, TargetGeneration: intent.Channel.BindingGeneration, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	canonical, _, err := tx.PutOperation(op, 0)
	return canonical, err
}

func (s *Service) projectDocumentAccessTarget(ctx context.Context, tx *workstore.Tx, resource work.Resource, destination MessageProjection) (projectDocumentAccessTarget, error) {
	var target projectDocumentAccessTarget
	creator, err := tx.ProjectDocumentCreation(resource.ScopeID, resource.ID)
	if err != nil {
		return target, err
	}
	scope, err := tx.GetScope(resource.ScopeID)
	if err != nil || scope.Kind != work.ScopeProject {
		return target, workstore.ErrScopeDenied
	}
	channel, err := tx.GetProjectChannelBinding(destination.Tenant, destination.ChatID)
	if err != nil || !channel.Active || channel.Purpose != "main" || channel.ProjectID != scope.ProjectID || channel.BindingGeneration != destination.ChannelGeneration {
		return target, workstore.ErrScopeDenied
	}
	doc, err := documentResourceID(resource)
	if err != nil {
		return target, err
	}
	body, err := tx.GetContent(resource.ScopeID, creator.RequestRef)
	if err != nil {
		return target, err
	}
	var original resourceWriteIntent
	if err := json.Unmarshal(body.Data, &original); err != nil {
		return target, err
	}
	if original.Binding.RoleID != creator.Actor.ID {
		return target, workstore.ErrScopeDenied
	}
	target = projectDocumentAccessTarget{resource: resource, creator: creator, base: workstore.ProjectDocumentShareID(resource.ScopeID, resource.ID, channel.Tenant, channel.ChatID, channel.BindingGeneration), intent: projectDocumentShare{Binding: original.Binding, Channel: channel, ResourceID: resource.ID, DocumentID: doc}}
	binding, err := tx.GetRoleBotBinding(original.Binding.RoleID)
	if err != nil || binding != original.Binding {
		return target, nil
	}
	connector, err := s.resolveResourceConnector(ctx, binding)
	if err == nil && connector.Identity() == original.executionConnectorID() {
		target.gateway, _ = connector.(projectDocumentSharer)
	}
	return target, nil
}

func (s *Service) observeProjectDocumentAccess(ctx context.Context, target projectDocumentAccessTarget) (string, error) {
	id, err := newID("project-document-access")
	if err != nil {
		return "", err
	}
	started := s.now().UTC()
	op := work.Operation{ID: id, ScopeID: target.resource.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "project-document-access"}, Kind: workstore.ProjectDocumentAccessObservationKind, TargetID: target.base, TargetGeneration: 1, IdempotencyKey: id, RequestHash: stableID("document-access", target.base), State: work.OperationSending, Revision: 1, CreatedAt: started, UpdatedAt: started}
	// The target ID already includes the channel generation. Reserve a separate
	// monotone read generation before I/O without relying on wall clock order.
	if err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		latest, err := tx.LatestProjectDocumentAccessObservation(op.ScopeID, op.TargetID)
		if err == nil {
			op.TargetGeneration = latest.TargetGeneration + 1
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		_, _, err = tx.PutOperation(op, 0)
		return err
	}); err != nil {
		return "", err
	}
	var perm string
	var readErr error
	if target.gateway == nil {
		readErr = fmt.Errorf("the document's original application cannot check access")
	} else {
		perm, readErr = target.gateway.ProjectDocumentGroupPermission(ctx, target.intent.DocumentID, target.intent.Channel.ChatID)
	}
	now := s.now().UTC()
	observation := workstore.ProjectDocumentAccessObservation{Permission: perm, ReadFailed: readErr != nil, ObservedAt: now}
	raw, err := json.Marshal(observation)
	if err != nil {
		return "", err
	}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	latest := false
	err = s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op.State, op.ExternalReceipt, op.Revision, op.UpdatedAt = work.OperationConfirmed, string(raw), 2, now
		if _, _, err := tx.PutOperation(op, 1); err != nil {
			return err
		}
		current, err := tx.LatestProjectDocumentAccessObservation(op.ScopeID, op.TargetID)
		latest = current.ID == op.ID
		return err
	})
	if err != nil {
		return "", err
	}
	if readErr != nil {
		return "", projectDocumentPermissionReadError{readErr}
	}
	if !latest {
		return "", projectDocumentPermissionReadError{fmt.Errorf("a newer read has superseded this document access observation")}
	}
	return perm, nil
}

func (s *Service) finishProjectDocumentShare(ctx context.Context, original work.Operation, permission string, callErr error) (Result, error) {
	result := Result{Status: "pending_external", ObjectID: original.TargetID, OperationID: original.ID}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(original.ID)
		if err != nil {
			return err
		}
		result.Revision = op.Revision
		if op.State == work.OperationConfirmed {
			result.Status, result.Replay = "project_access_confirmed", true
			return nil
		}
		// A successful read confirms the requested outcome, even if the original
		// grant response was lost. It cannot confirm that the original call ran.
		if permission != "" {
			if op.State == work.OperationPrepared || op.State == work.OperationRetryable {
				op.State = work.OperationSending
				op.Revision++
				op.UpdatedAt = s.now()
				if _, _, err = tx.PutOperation(op, op.Revision-1); err != nil {
					return err
				}
			}
			observed, err := json.Marshal(map[string]any{"permission": permission, "observed_at": s.now(), "source": "feishu.drive.permission_member.list"})
			if err != nil {
				return err
			}
			op.ExternalReceipt = string(observed)
			op.State = work.OperationConfirmed
			op.LastError = ""
			result.Status = "project_access_confirmed"
		} else {
			if op.Revision != original.Revision || op.State != work.OperationSending {
				return workstore.ErrConflict
			}
			op.State = work.OperationUnknown
			op.LastError = "group access has not been observed"
			if callErr != nil {
				op.LastError = callErr.Error()
				var platformErr *feishu.APIError
				if errors.As(callErr, &platformErr) && platformErr.API == "drive.v1.permission_member.create" && platformErr.Code != 0 && platformErr.StatusCode < 500 {
					op.State = work.OperationRetryable
					result.Status = "project_access_retryable"
				}
			}
		}
		op.Revision++
		op.UpdatedAt = s.now()
		result.Revision = op.Revision
		_, _, err = tx.PutOperation(op, op.Revision-1)
		return err
	})
	return result, err
}

func (c *FeishuResourceConnector) ProjectDocumentGroupPermission(ctx context.Context, doc feishu.ProjectDocumentID, chat string) (string, error) {
	members, err := c.bitable.ListPermissionMembers(ctx, string(doc), "docx")
	if err != nil {
		return "", err
	}
	permission := members["openchat:"+chat].Perm
	switch permission {
	case "view", "edit", "full_access":
		return permission, nil
	default:
		return "", nil
	}
}

func (c *FeishuResourceConnector) GrantProjectDocumentGroupView(ctx context.Context, doc feishu.ProjectDocumentID, chat string) error {
	return c.bitable.GrantPermission(ctx, string(doc), "docx", "openchat", chat, "chat", "view")
}
