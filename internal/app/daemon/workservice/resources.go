package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type RegisterResourceRequest struct {
	AsDirectory  bool   `json:"as_directory,omitempty"`
	Key          string `json:"key"`
	CollectionID string `json:"collection_id"`
	SourceURL    string `json:"source_url"`
	Purpose      string `json:"purpose"`
}
type ReadResourceVersionRequest struct {
	ResourceID string `json:"resource_id"`
	VersionID  string `json:"version_id"`
	Offset     int    `json:"offset,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}
type ResourceVersionView struct {
	Version work.ResourceVersion `json:"version"`
	Content *ContentPage         `json:"content,omitempty"`
}
type ReconcileResourceRequest struct {
	OperationID string `json:"operation_id"`
}

type resourceIntent struct {
	Binding           *work.RoleBotBinding     `json:"binding,omitempty"`
	ScopeID           string                   `json:"scope_id"`
	RoleID            string                   `json:"role_id"`
	ContextID         string                   `json:"context_id"`
	AttemptID         string                   `json:"attempt_id"`
	BotGeneration     int64                    `json:"bot_generation"`
	ConnectorIdentity string                   `json:"connector_identity"`
	OperationID       string                   `json:"operation_id"`
	Register          *RegisterResourceRequest `json:"register,omitempty"`
	Capture           *CaptureResourceRequest  `json:"capture,omitempty"`
}

func (s *Service) ResourceRegister(ctx context.Context, token, runRef string, req RegisterResourceRequest) (Result, error) {
	if strings.TrimSpace(req.SourceURL) == "" || strings.TrimSpace(req.CollectionID) == "" || strings.TrimSpace(req.Purpose) == "" {
		return Result{}, &workcmd.Error{Code: "invalid_argument", Message: "source_url, collection_id and purpose are required"}
	}
	intent, replay, err := s.prepareResource(ctx, token, runRef, "resource.register", req.Key, req, &req, nil)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	return s.performResource(ctx, intent)
}
func (s *Service) ResourceCapture(ctx context.Context, token, runRef string, req CaptureResourceRequest) (Result, error) {
	if req.ResourceID == "" {
		return Result{}, &workcmd.Error{Code: "invalid_argument", Message: "resource_id is required"}
	}
	intent, replay, err := s.prepareResource(ctx, token, runRef, "resource.capture", req.Key, req, nil, &req)
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	return s.performResource(ctx, intent)
}
func (s *Service) ResourceReadVersion(ctx context.Context, token, runRef string, req ReadResourceVersionRequest) (ResourceVersionView, error) {
	var result ResourceVersionView
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		resource, err := tx.GetResource(c.Worker.ScopeID, req.ResourceID)
		if err != nil {
			return err
		}
		version, err := tx.GetResourceVersion(c.Worker.ScopeID, req.VersionID)
		if err != nil {
			return err
		}
		if version.ResourceID != resource.ID {
			return workauth.ErrUnauthorized
		}
		result.Version = version
		if version.ContentRef != "" {
			body, err := tx.GetContent(c.Worker.ScopeID, version.ContentRef)
			if err != nil {
				return err
			}
			if version.FileName != "" && !strings.HasPrefix(body.MediaType, "text/") {
				return nil
			}
			page, err := knowledgeContentPage(body, req.Offset, req.Limit)
			if err != nil {
				return err
			}
			result.Content = &page
		}
		return nil
	})
	return result, err
}

func (s *Service) ResourceReconcile(ctx context.Context, token, runRef string, req ReconcileResourceRequest) (Result, error) {
	var intent resourceIntent
	var replay *Result
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		operation, err := tx.GetOperation(req.OperationID)
		if err != nil {
			return err
		}
		if operation.ScopeID != c.Worker.ScopeID || operation.Actor != (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) || (operation.Kind != "resource.register" && operation.Kind != "resource.capture") {
			return workauth.ErrUnauthorized
		}
		if operation.State == work.OperationConfirmed {
			result, err := resourceReceipt(operation)
			if err != nil {
				return err
			}
			result.Replay = true
			replay = &result
			return nil
		}
		// An in-flight call is not restarted. Recovery explicitly marks unknown only
		// after reconciling the daemon's ownership; these connector operations read.
		if operation.State != work.OperationUnknown && operation.State != work.OperationRetryable {
			return &workcmd.Error{Code: "blocked", Message: "operation must be unknown or retryable before explicit reconciliation"}
		}
		body, err := tx.GetContent(c.Worker.ScopeID, operation.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &intent); err != nil {
			return err
		}
		if intent.ScopeID != operation.ScopeID || intent.OperationID != operation.ID || intent.RoleID != operation.Actor.ID {
			return workauth.ErrUnauthorized
		}
		binding, err := tx.GetRoleBotBinding(intent.RoleID)
		if err != nil {
			return err
		}
		connector, resolveErr := s.resolveResourceConnector(ctx, binding)
		if resolveErr != nil {
			return resolveErr
		}
		if connector.Identity() != intent.ConnectorIdentity || binding.IdentityGeneration != intent.BotGeneration || intent.Binding != nil && binding != *intent.Binding {
			return workauth.ErrUnauthorized
		}
		if operation.State == work.OperationUnknown {
			operation.State = work.OperationRetryable
			operation.Revision++
			if _, _, err = tx.PutOperation(operation, operation.Revision-1); err != nil {
				return err
			}
		}
		operation.State = work.OperationSending
		operation.Revision++
		_, _, err = tx.PutOperation(operation, operation.Revision-1)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	return s.performResource(ctx, intent)
}

func (s *Service) prepareResource(ctx context.Context, token, runRef, kind, key string, request any, register *RegisterResourceRequest, capture *CaptureResourceRequest) (resourceIntent, *Result, error) {
	var intent resourceIntent
	var replay *Result
	if strings.TrimSpace(key) == "" || len(key) > 256 {
		return intent, nil, &workcmd.Error{Code: "invalid_argument", Message: "a stable key of at most 256 bytes is required"}
	}
	err := s.withWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		binding, bindingErr := tx.GetRoleBotBinding(c.Worker.RoleID)
		if bindingErr != nil {
			return bindingErr
		}
		connector, resolveErr := s.resolveResourceConnector(ctx, binding)
		if resolveErr != nil {
			return resolveErr
		}
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if capture != nil {
			resource, err := tx.GetResource(c.Worker.ScopeID, capture.ResourceID)
			if err != nil {
				return err
			}
			if !resourceConnectorAllowed(scope, resource, connector) {
				return workauth.ErrUnauthorized
			}
		}
		data, err := json.Marshal(request)
		if err != nil {
			return err
		}
		id := stableID("op", c.Worker.ScopeID, c.Worker.RoleID, kind, key)
		intent = resourceIntent{Binding: &binding, ScopeID: c.Worker.ScopeID, RoleID: c.Worker.RoleID, ContextID: c.Worker.ContextID, AttemptID: c.Attempt.ID, BotGeneration: c.Claims.BotGeneration, ConnectorIdentity: connector.Identity(), OperationID: id, Register: register, Capture: capture}
		requestRef := stableID("resource-intent", id)
		if _, e := tx.GetOperation(id); errors.Is(e, workstore.ErrNotFound) {
			payload, e := json.Marshal(intent)
			if e != nil {
				return e
			}
			if e = tx.PutContent(work.NewContent(requestRef, intent.ScopeID, "application/json", payload, s.now())); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		operation := work.Operation{ID: id, ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: kind, TargetID: stableID("resource-request", id), TargetGeneration: 1, IdempotencyKey: key, RequestRef: requestRef, RequestHash: stableID("resource-hash", connector.Identity(), string(data)), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		canonical, created, err := tx.PutOperation(operation, 0)
		if err != nil {
			return err
		}
		if !created {
			result := Result{Status: "pending_external", OperationID: canonical.ID, Replay: true}
			if canonical.State == work.OperationConfirmed {
				result, err = resourceReceipt(canonical)
				result.Replay = true
			} else if canonical.State == work.OperationTerminal {
				err = &workcmd.Error{Code: "blocked", Message: "resource operation failed; inspect its recorded error before submitting a new intent"}
			}
			replay = &result
			return err
		}

		operation.State = work.OperationSending
		operation.Revision++
		_, _, err = tx.PutOperation(operation, 1)
		return err
	})
	return intent, replay, err
}
func resourceReceipt(operation work.Operation) (Result, error) {
	var result Result
	err := json.Unmarshal([]byte(operation.ExternalReceipt), &result)
	if err != nil {
		return result, fmt.Errorf("invalid resource operation receipt: %w", err)
	}
	return result, nil
}
