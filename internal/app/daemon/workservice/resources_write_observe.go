package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
	"strings"
	"time"
)

type ObserveResourceWriteRequest struct {
	Key              string `json:"key"`
	OperationID      string `json:"operation_id"`
	PageToken        string `json:"page_token,omitempty"`
	PageSize         int    `json:"page_size,omitempty"`
	DocumentRevision int    `json:"document_revision,omitempty"`
}
type ResourceWriteObservation struct {
	LateReceiptRefs     []string                    `json:"late_receipt_refs,omitempty"`
	RecoveryOperationID string                      `json:"recovery_operation_id,omitempty"`
	OperationID         string                      `json:"operation_id"`
	OriginalState       work.OperationState         `json:"original_state"`
	ObservationRef      string                      `json:"observation_ref"`
	Document            *ResourceDocumentInspection `json:"document,omitempty"`
	Sheet               *ResourceSheetInspection    `json:"sheet,omitempty"`
}

// ResourceObserveWrite reads the exact returned object/range. Its observation is
// evidence of current platform state, never proof that an uncertain call caused
// that state. In particular it does not repeat a write or erase original unknown.
func (s *Service) ResourceObserveWrite(ctx context.Context, token, run string, req ObserveResourceWriteRequest) (ResourceWriteObservation, error) {
	var result ResourceWriteObservation
	var intent resourceWriteIntent
	var writer ResourceWriteConnector
	var readOp work.Operation
	var original work.Operation
	if req.PageToken != "" && req.DocumentRevision < 1 {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "continuation requires exact document_revision from the previous observation"}
	}
	if req.Key == "" || len(req.Key) > 256 || req.OperationID == "" || req.PageSize < 0 || req.PageSize > 50 {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "stable observation key, original operation and page_size0..50 required"}
	}
	perform := false
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		var err error
		original, err = tx.GetOperation(req.OperationID)
		if err != nil {
			return err
		}
		if original.ScopeID != c.Worker.ScopeID || original.Actor != (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) || !work.ResourceWriteCanBeInspected(original.Kind) {
			return workstore.ErrScopeDenied
		}
		id := stableID("resource-write-observation", original.ID, req.Key)
		requestHash, err := resourceJSONHash(req)
		if err != nil {
			return err
		}
		if existing, e := tx.GetOperation(id); e == nil {
			if existing.RequestHash != requestHash {
				return workstore.ErrIdempotencyConflict
			}
			if existing.State == work.OperationConfirmed {
				return json.Unmarshal([]byte(existing.ExternalReceipt), &result)
			}
			return &workcmd.Error{Code: "blocked", Message: "this observation has already started; use a new read key after checking its state"}
		} else if e != workstore.ErrNotFound {
			return e
		}
		content, err := tx.GetContent(original.ScopeID, original.RequestRef)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(content.Data, &intent); err != nil {
			return err
		}
		if intent.Kind != original.Kind || intent.ScopeID != original.ScopeID {
			return workstore.ErrScopeDenied
		}
		binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
		if err != nil {
			return err
		}
		if binding != intent.Binding {
			return workstore.ErrScopeDenied
		}
		connector, err := s.resolveResourceConnector(ctx, binding)
		if err != nil {
			return err
		}
		if connector.Identity() != intent.executionConnectorID() {
			return workstore.ErrScopeDenied
		}
		var ok bool
		writer, ok = connector.(ResourceWriteConnector)
		if !ok {
			return fmt.Errorf("registered connector cannot read the written document or sheet")
		}
		for _, step := range original.Steps {
			if strings.HasPrefix(step.ID, "late_original_receipt:") {
				result.LateReceiptRefs = append(result.LateReceiptRefs, step.Receipt)
			}
			if step.ID == "human_resource_recovery" {
				result.RecoveryOperationID = step.Receipt
			}
		}
		if intent.Create != nil && result.RecoveryOperationID != "" {
			audit, e := tx.GetOperation(result.RecoveryOperationID)
			if e != nil {
				return e
			}
			body, e := tx.GetContent(original.ScopeID, audit.RequestRef)
			if e != nil {
				return e
			}
			var recovery work.ResourceWriteRecoveryAdoption
			if e = json.Unmarshal(body.Data, &recovery); e != nil {
				return e
			}
			intent.Resource, e = tx.GetResource(original.ScopeID, recovery.ResourceID)
			if e != nil {
				return e
			}
		}
		if intent.Create != nil && result.RecoveryOperationID == "" {
			receipt, e := resourceWriteReplay(original)
			if e != nil {
				return e
			}
			if receipt.Receipt.ExternalID == "" {
				return &workcmd.Error{Code: "blocked", Message: "creation returned no actual object ID; do not recreate or match a title. Ask the human to explicitly choose an existing resource and register that actual URL; the original creation remains unknown"}
			}
			intent.Resource.SourceIdentity = "docx:" + receipt.Receipt.ExternalID
			intent.Resource.CanonicalURI = receipt.Receipt.CanonicalURI
		}
		body, _ := json.Marshal(req)
		request := work.NewContent(id+":request", original.ScopeID, "application/json", body, s.now())
		if err = tx.PutContent(request); err != nil {
			return err
		}
		readOp = work.Operation{ID: id, ScopeID: original.ScopeID, ContextID: c.Worker.ContextID, Actor: original.Actor, Kind: "resource.write.observe", TargetID: original.ID, TargetGeneration: original.TargetGeneration, IdempotencyKey: req.Key, RequestRef: request.ID, RequestHash: requestHash, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, err = tx.PutOperation(readOp, 0); err != nil {
			return err
		}
		readOp.State = work.OperationSending
		readOp.Revision++
		if _, _, err = tx.PutOperation(readOp, 1); err != nil {
			return err
		}
		perform = true
		return nil
	})
	if err != nil || !perform {
		return result, err
	}
	result.OperationID = original.ID
	result.OriginalState = original.State
	if intent.Sheet != nil {
		var observed ResourceSheetInspection
		observed, err = writer.ReadResourceSheet(ctx, intent.Resource, ReadResourceSheetRequest{ResourceID: intent.Resource.ID, SheetID: intent.Sheet.SheetID, Range: intent.Sheet.Range})
		result.Sheet = &observed
	} else {
		var observed ResourceDocumentInspection
		observed, err = writer.InspectResourceDocument(ctx, intent.Resource, InspectResourceDocumentRequest{ResourceID: intent.Resource.ID, PageToken: req.PageToken, PageSize: req.PageSize, DocumentRevision: req.DocumentRevision})
		result.Document = &observed
	}
	callErr := err
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(readOp.ID)
		if e != nil {
			return e
		}
		if op.State != work.OperationSending {
			return workstore.ErrConflict
		}
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		if callErr != nil {
			op.State = work.OperationTerminal
			op.LastError = callErr.Error()
		} else {
			result.ObservationRef = op.ID + ":receipt"
			raw, e := json.Marshal(result)
			if e != nil {
				return e
			}
			content := work.NewContent(result.ObservationRef, original.ScopeID, "application/json", raw, s.now())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			op.State = work.OperationConfirmed
			op.ExternalReceipt = string(raw)
			op.Steps = append(op.Steps, work.OperationStep{ID: "actual_platform_read", State: work.OperationConfirmed, Receipt: content.ID})
		}
		_, _, e = tx.PutOperation(op, expected)
		return e
	})
	if err != nil {
		return result, err
	}
	return result, callErr
}
