package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
)

type resourceWriteIntent struct {
	ConnectorIdentity string                         `json:"connector_identity,omitempty"`
	SharedFeishu      bool                           `json:"shared_feishu,omitempty"`
	Kind              string                         `json:"kind"`
	ScopeID           string                         `json:"scope_id"`
	Binding           work.RoleBotBinding            `json:"binding"`
	Resource          work.Resource                  `json:"resource"`
	Create            *CreateResourceDocumentRequest `json:"create,omitempty"`
	Append            *AppendResourceDocumentRequest `json:"append,omitempty"`
	Update            *UpdateResourceDocumentRequest `json:"update,omitempty"`
	Sheet             *WriteResourceSheetRequest     `json:"sheet,omitempty"`
}

func (i resourceWriteIntent) executionConnectorID() string {
	if i.ConnectorIdentity != "" {
		return i.ConnectorIdentity
	}
	// Earlier operations could only execute with the registration connector.
	return i.Resource.ConnectorIdentity
}

func (s *Service) ResourceCreateDocument(ctx context.Context, token, run string, req CreateResourceDocumentRequest) (ResourceWriteResult, error) {
	if strings.TrimSpace(req.Title) == "" || len([]rune(req.Title)) > 200 || strings.TrimSpace(req.Purpose) == "" || strings.TrimSpace(req.CollectionID) == "" {
		return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "bounded title, purpose and explicit collection are required"}
	}
	return s.executeResourceWrite(ctx, token, run, req.Key, "resource.document.create", req.ParentResourceID, req.ExpectedParentRevision, resourceWriteIntent{Create: &req})
}
func (s *Service) ResourceAppendDocument(ctx context.Context, token, run string, req AppendResourceDocumentRequest) (ResourceWriteResult, error) {
	if req.ParentBlockID == "" || req.ObservedDocumentRevision < 1 || len(req.Paragraphs) < 1 || len(req.Paragraphs) > 50 || req.Index != nil && *req.Index < -1 {
		return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "explicit parent block, observed revision and1..50 paragraphs required"}
	}
	for _, text := range req.Paragraphs {
		if text == "" || len([]rune(text)) > 10000 {
			return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "each paragraph must contain1..10000 characters"}
		}
	}
	return s.executeResourceWrite(ctx, token, run, req.Key, "resource.document.append", req.ResourceID, req.ExpectedResourceRevision, resourceWriteIntent{Append: &req})
}
func (s *Service) ResourceUpdateDocument(ctx context.Context, token, run string, req UpdateResourceDocumentRequest) (ResourceWriteResult, error) {
	if req.ObservedDocumentRevision < 1 || len(req.Edits) < 1 || len(req.Edits) > 50 {
		return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "observed revision and1..50 explicit text-run edits required"}
	}
	seen := map[string]bool{}
	for _, edit := range req.Edits {
		key := fmt.Sprintf("%s:%d", edit.BlockID, edit.TextRunIndex)
		if edit.BlockID == "" || edit.TextRunIndex < 0 || len(edit.ExpectedBlockSHA256) != 64 || len([]rune(edit.Text)) > 10000 || seen[key] {
			return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "each edit requires unique block/run, actual full-block SHA256 and bounded text"}
		}
		if _, err := hex.DecodeString(edit.ExpectedBlockSHA256); err != nil {
			return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "block SHA256 must be lowercase hexadecimal"}
		}
		seen[key] = true
	}
	return s.executeResourceWrite(ctx, token, run, req.Key, work.ResourceDocumentUpdateOperation, req.ResourceID, req.ExpectedResourceRevision, resourceWriteIntent{Update: &req})
}
func (s *Service) ResourceWriteSheet(ctx context.Context, token, run string, req WriteResourceSheetRequest) (ResourceWriteResult, error) {
	if req.SheetID == "" || req.Range == "" || len(req.ExpectedDataSHA256) != 64 || len(req.Values) < 1 || len(req.Values) > 5000 {
		return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "explicit sheet/range, current read hash and bounded values required"}
	}
	raw, err := json.Marshal(req.Values)
	if err != nil || len(raw) > 2<<20 {
		return ResourceWriteResult{}, &workcmd.Error{Code: "invalid_argument", Message: "sheet values must be valid JSON no larger than2 MiB"}
	}
	return s.executeResourceWrite(ctx, token, run, req.Key, "resource.sheet.write", req.ResourceID, req.ExpectedResourceRevision, resourceWriteIntent{Sheet: &req})
}
func (s *Service) executeResourceWrite(ctx context.Context, token, run, key, kind, resourceID string, expected int64, intent resourceWriteIntent) (ResourceWriteResult, error) {
	var result ResourceWriteResult
	var op work.Operation
	var connector ResourceWriteConnector
	perform := false
	activeID := ""
	defer func() {
		if activeID != "" {
			s.resourceCalls.end(activeID)
		}
	}()
	if key == "" || len(key) > 256 || resourceID == "" || expected < 1 {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "stable key, registered resource and exact resource revision required"}
	}
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap {
			return workauth.ErrUnauthorized
		}
		semanticRaw, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(semanticRaw)
		requestHash := hex.EncodeToString(hash[:])
		id := stableID("resource-write", scope.ID, c.Worker.RoleID, kind, key)
		if existing, err := tx.GetOperation(id); err == nil {
			if existing.RequestHash != requestHash {
				return workstore.ErrIdempotencyConflict
			}
			result, err = resourceWriteReplay(existing)
			return err
		} else if err != workstore.ErrNotFound {
			return err
		}
		resource, err := tx.GetResource(scope.ID, resourceID)
		if err != nil {
			return err
		}
		if resource.Revision != expected {
			return workstore.ErrConflict
		}
		binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
		if err != nil {
			return err
		}
		bound, err := s.resolveResourceConnector(ctx, binding)
		if err != nil {
			return err
		}
		writer, ok := bound.(ResourceWriteConnector)
		if !ok {
			return &workcmd.Error{Code: "invalid_argument", Message: "this registered connection does not provide document or sheet editing"}
		}
		if !resourceConnectorAllowed(scope, resource, bound) {
			return workauth.ErrUnauthorized
		}
		if intent.Create != nil && !resourceDirectory(resource) {
			return &workcmd.Error{Code: "invalid_argument", Message: "document creation requires a registered existing parent directory"}
		}
		intent.Kind = kind
		intent.ScopeID = scope.ID
		intent.Binding = binding
		intent.Resource = resource
		intent.ConnectorIdentity = bound.Identity()
		_, feishu := bound.(*FeishuResourceConnector)
		intent.SharedFeishu = feishu && scope.Kind == work.ScopeProject
		raw, err := json.Marshal(intent)
		if err != nil {
			return err
		}
		body := work.NewContent(id+":request", scope.ID, "application/json", raw, s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op = work.Operation{ID: id, ScopeID: scope.ID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: kind, TargetID: resourceID, TargetGeneration: binding.IdentityGeneration, IdempotencyKey: key, RequestRef: body.ID, RequestHash: requestHash, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(op, 0); err != nil {
			return err
		}
		op.State = work.OperationSending
		op.Revision++
		if _, _, err = tx.PutOperation(op, 1); err != nil {
			return err
		}
		s.resourceCalls.begin(op.ID)
		activeID = op.ID
		connector = writer
		perform = true
		return nil
	})
	if err != nil || !perform {
		return result, err
	}
	receipt, callErr := applyResourceWrite(ctx, connector, intent, resourceWriteUUID(op.ID))
	// Record partial IDs before any later reads or snapshots. No network operation
	// occurs inside this receipt transaction, and no uncertain write is repeated.
	return s.commitResourceWrite(ctx, op, intent, receipt, callErr)
}
func applyResourceWrite(ctx context.Context, connector ResourceWriteConnector, intent resourceWriteIntent, clientToken string) (ResourceWriteReceipt, error) {
	switch {
	case intent.Create != nil:
		return connector.CreateResourceDocument(ctx, intent.Resource, *intent.Create)
	case intent.Append != nil:
		return connector.AppendResourceDocument(ctx, intent.Resource, *intent.Append, clientToken)
	case intent.Update != nil:
		return connector.UpdateResourceDocument(ctx, intent.Resource, *intent.Update, clientToken)
	case intent.Sheet != nil:
		return connector.WriteResourceSheet(ctx, intent.Resource, *intent.Sheet)
	default:
		return ResourceWriteReceipt{}, fmt.Errorf("resource write has no typed request")
	}
}
func resourceWriteUUID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
func resourceWriteReplay(op work.Operation) (ResourceWriteResult, error) {
	result := ResourceWriteResult{Result: Result{Status: "pending_external", OperationID: op.ID, Replay: true}}
	if op.ExternalReceipt != "" {
		if err := json.Unmarshal([]byte(op.ExternalReceipt), &result); err != nil {
			return result, err
		}
		result.Replay = true
	}
	if op.State == work.OperationSending || op.State == work.OperationUnknown {
		result.Status = "pending_external"
	}
	if resourceRecoverySealed(op) {
		result.Status = "unknown_sealed_by_human"
		for _, step := range op.Steps {
			if step.ID == "human_resource_recovery" {
				result.RecoveryOperationID = step.Receipt
			}
		}
	}
	if op.State == work.OperationTerminal {
		result.Status = "preflight_failed"
	}
	return result, nil
}
func (s *Service) commitResourceWrite(ctx context.Context, original work.Operation, intent resourceWriteIntent, receipt ResourceWriteReceipt, callErr error) (ResourceWriteResult, error) {
	result := ResourceWriteResult{Result: Result{Status: "written", OperationID: original.ID, ObjectID: intent.Resource.ID, Revision: intent.Resource.Revision}, Receipt: receipt}
	if intent.Create != nil {
		result.ObjectID = ""
		result.Revision = 0
		result.Status = "created"
	}
	if callErr != nil {
		if receipt.Attempted {
			result.Status = "pending_external"
		} else {
			result.Status = "preflight_failed"
		}
	}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(original.ID)
		if err != nil {
			return err
		}
		if resourceRecoverySealed(op) {
			raw, e := json.Marshal(struct {
				Receipt ResourceWriteReceipt `json:"actual_late_receipt"`
				Error   string               `json:"error,omitempty"`
			}{receipt, resourceErrorText(callErr)})
			if e != nil {
				return e
			}
			hash, _ := resourceJSONHash(json.RawMessage(raw))
			content := work.NewContent(op.ID+":late:"+hash, op.ScopeID, "application/json", raw, s.now())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			for _, step := range op.Steps {
				if step.ID == "late_original_receipt:"+hash {
					result.Status = "original_result_recorded_after_recovery"
					return nil
				}
			}
			expected := op.Revision
			op.Revision++
			op.UpdatedAt = s.now()
			op.Steps = append(op.Steps, work.OperationStep{ID: "late_original_receipt:" + hash, State: work.OperationConfirmed, Receipt: content.ID})
			if _, _, e = tx.PutOperation(op, expected); e != nil {
				return e
			}
			result.Status = "original_result_recorded_after_recovery"
			return nil
		}
		if op.State == work.OperationConfirmed {
			result, err = resourceWriteReplay(op)
			return err
		}
		if op.State != work.OperationSending && op.State != work.OperationUnknown {
			return workstore.ErrConflict
		}
		if callErr == nil && intent.Create != nil {
			if receipt.ExternalID == "" || receipt.SourceIdentity == "" || receipt.CanonicalURI == "" {
				return fmt.Errorf("document creation returned incomplete identity")
			}
			resource := work.Resource{ScopeID: intent.ScopeID, ConnectorIdentity: intent.executionConnectorID(), SourceIdentity: receipt.SourceIdentity, CanonicalURI: receipt.CanonicalURI, CollectionID: intent.Create.CollectionID, Purpose: intent.Create.Purpose, Revision: 1}
			shared := intent.SharedFeishu && work.IsFeishuResource(resource)
			resource.ID = newRegisteredResourceID(resource, shared)
			if existing, found, e := findRegisteredResource(tx, resource, shared); e != nil {
				return e
			} else if found {
				resource = existing
				if err = tx.AddResourceCollection(resource.ScopeID, resource.ID, intent.Create.CollectionID); err != nil {
					return err
				}
			} else if err = tx.PutResource(resource, 0); err != nil {
				return err
			}
			result.ObjectID = resource.ID
			result.Revision = resource.Revision
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		evidence := work.NewContent(op.ID+":receipt", op.ScopeID, "application/json", raw, s.now())
		if err = tx.PutContent(evidence); err != nil {
			return err
		}
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		op.ExternalReceipt = string(raw)
		state := work.OperationConfirmed
		if callErr != nil {
			op.LastError = callErr.Error()
			if receipt.Attempted {
				state = work.OperationUnknown
			} else {
				state = work.OperationTerminal
			}
		}
		op.State = state
		op.Steps = append(op.Steps, work.OperationStep{ID: "platform_write", State: state, Receipt: evidence.ID})
		_, _, err = tx.PutOperation(op, expected)
		return err
	})
	if err != nil {
		return result, err
	}
	if callErr != nil && !receipt.Attempted {
		return result, callErr
	}
	return result, nil
}

func resourceErrorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}
