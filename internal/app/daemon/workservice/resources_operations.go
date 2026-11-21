package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

func (s *Service) performResource(ctx context.Context, intent resourceIntent) (Result, error) {

	var resource work.Resource
	var binding work.RoleBotBinding
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		binding, err = tx.GetRoleBotBinding(intent.RoleID)
		if err != nil {
			return err
		}
		if binding.IdentityGeneration != intent.BotGeneration {
			return workauth.ErrUnauthorized
		}
		if intent.Capture != nil {
			resource, err = tx.GetResource(intent.ScopeID, intent.Capture.ResourceID)
			if err != nil {
				return err
			}
			if resource.ConnectorIdentity != intent.ConnectorIdentity {
				return workauth.ErrUnauthorized
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	connector, resolveErr := s.resolveResourceConnector(ctx, binding)
	if resolveErr != nil {
		return Result{}, resolveErr
	}
	if connector.Identity() != intent.ConnectorIdentity {
		return Result{}, workauth.ErrUnauthorized
	}
	// No database transaction spans either remote operation.
	if intent.Register != nil {
		var resolved ResolvedResource
		var err error
		if intent.Register.AsDirectory {
			directory, ok := connector.(ResourceDirectoryResolver)
			if !ok {
				return s.recordResourceFailure(intent, fmt.Errorf("selected connection cannot resolve directories"))
			}
			resolved, err = directory.ResolveDirectory(ctx, intent.Register.SourceURL)
		} else {
			resolved, err = connector.Resolve(ctx, intent.Register.SourceURL)
		}
		if err != nil {
			return s.recordResourceFailure(intent, err)
		}
		if resolved.SourceIdentity == "" || resolved.CanonicalURI == "" {
			return s.recordResourceFailure(intent, fmt.Errorf("connector returned incomplete canonical resource identity"))
		}
		if err := resolved.Source.Validate(); err != nil {
			return s.recordResourceFailure(intent, err)
		}
		return s.commitResourceRegistration(ctx, intent, resolved)
	}
	if intent.Capture != nil {
		captured, err := connector.Capture(ctx, resource, *intent.Capture)
		if err != nil {
			return s.recordResourceFailure(intent, err)
		}
		return s.commitResourceCapture(ctx, intent, resource, captured)
	}
	return s.recordResourceFailure(intent, fmt.Errorf("resource intent has no concrete operation"))
}

func (s *Service) recordResourceFailure(intent resourceIntent, cause error) (Result, error) {
	var timeout net.Error
	uncertain := errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled) || (errors.As(cause, &timeout) && timeout.Timeout())
	// Persist failure even if the caller disconnected. This bounded local commit
	// neither retries the remote operation nor starts another product execution.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(intent.OperationID)
		if err != nil {
			return err
		}
		if operation.State == work.OperationConfirmed || operation.State == work.OperationTerminal {
			return nil
		}
		previous := operation.Revision
		operation.Revision++
		operation.UpdatedAt = s.now()
		operation.LastError = cause.Error()
		if uncertain {
			operation.State = work.OperationUnknown
		} else {
			operation.State = work.OperationTerminal
		}
		_, _, err = tx.PutOperation(operation, previous)
		return err
	})
	if err != nil {
		return Result{}, fmt.Errorf("resource call failed (%v) and receipt persistence failed: %w", cause, err)
	}
	result := Result{Status: "failed", OperationID: intent.OperationID}
	if uncertain {
		result.Status = "pending_external"
		return result, nil
	}
	return result, cause
}

func (s *Service) commitResourceRegistration(ctx context.Context, intent resourceIntent, resolved ResolvedResource) (Result, error) {
	var result Result
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(intent.OperationID)
		if err != nil {
			return err
		}
		if operation.State == work.OperationConfirmed {
			result, err = resourceReceipt(operation)
			result.Replay = true
			return err
		}
		if operation.State != work.OperationSending && operation.State != work.OperationUnknown {
			return &workcmd.Error{Code: "conflict", Message: "resource operation is no longer awaiting a receipt"}
		}
		id := stableID("resource", intent.ScopeID, intent.ConnectorIdentity, resolved.SourceIdentity)
		registered := work.Resource{ID: id, ScopeID: intent.ScopeID, CollectionID: intent.Register.CollectionID, ConnectorIdentity: intent.ConnectorIdentity, SourceIdentity: resolved.SourceIdentity, CanonicalURI: resolved.CanonicalURI, Purpose: intent.Register.Purpose, Revision: 1}
		existing, err := tx.ListResources(intent.ScopeID)
		if err != nil {
			return err
		}
		found := false
		for _, item := range existing {
			if item.ConnectorIdentity == intent.ConnectorIdentity && item.SourceIdentity == resolved.SourceIdentity {
				registered = item
				found = true
				break
			}
		}
		if found {
			if err = tx.AddResourceCollection(intent.ScopeID, registered.ID, intent.Register.CollectionID); err != nil {
				return err
			}
		} else if err = tx.PutResource(registered, 0); err != nil {
			return err
		}
		evidence, err := json.Marshal(resolved)
		if err != nil {
			return err
		}
		body := work.NewContent(stableID("resource-evidence", operation.ID), intent.ScopeID, "application/json", evidence, s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		result = Result{Status: "completed", OperationID: operation.ID, ObjectID: registered.ID, Revision: registered.Revision}
		return s.confirmResourceOperation(tx, operation, result, body.ID)
	})
	return result, err
}

func (s *Service) commitResourceCapture(ctx context.Context, intent resourceIntent, resource work.Resource, captured CapturedResource) (Result, error) {
	var result Result
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(intent.OperationID)
		if err != nil {
			return err
		}
		if operation.State == work.OperationConfirmed {
			result, err = resourceReceipt(operation)
			result.Replay = true
			return err
		}
		if operation.State != work.OperationSending && operation.State != work.OperationUnknown {
			return &workcmd.Error{Code: "conflict", Message: "resource operation is no longer awaiting a receipt"}
		}
		id := stableID("resource-version", operation.ID)
		body := work.NewContent(stableID("resource-body", id), intent.ScopeID, captured.MediaType, captured.Content, s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		source := captured.Source
		source.EvidenceRef = body.ID
		version := work.ResourceVersion{ID: id, ResourceID: resource.ID, ScopeID: intent.ScopeID, Kind: captured.Kind, Source: source, ContentRef: body.ID, ContentHash: body.SHA256, ObservationRange: captured.ObservationRange, Consistency: captured.Consistency, Complete: captured.Complete, Saved: true, StartedAt: captured.StartedAt, CapturedAt: captured.CapturedAt}
		if err = tx.PutResourceVersion(version); err != nil {
			return err
		}
		result = Result{Status: "completed", OperationID: operation.ID, ObjectID: version.ID, Revision: 1}
		return s.confirmResourceOperation(tx, operation, result, body.ID)
	})
	return result, err
}
func (s *Service) confirmResourceOperation(tx *workstore.Tx, operation work.Operation, result Result, evidenceRef string) error {
	receipt, err := json.Marshal(result)
	if err != nil {
		return err
	}
	previous := operation.Revision
	operation.Revision++
	operation.State = work.OperationConfirmed
	operation.ExternalReceipt = string(receipt)
	operation.UpdatedAt = s.now()
	operation.Steps = []work.OperationStep{{ID: "read-back", State: work.OperationConfirmed, Receipt: evidenceRef}}
	_, _, err = tx.PutOperation(operation, previous)
	return err
}
