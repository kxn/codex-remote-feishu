package workservice

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/core/workcmd"
	"strings"
	"time"
)

type ReadResourceCommentsRequest struct {
	Key        string `json:"key"`
	ResourceID string `json:"resource_id"`
	CommentID  string `json:"comment_id,omitempty"`
	PageToken  string `json:"page_token,omitempty"`
	PageSize   int    `json:"page_size,omitempty"`
}
type ResourceCommentsObservation struct {
	OperationID    string                             `json:"operation_id"`
	ResourceID     string                             `json:"resource_id"`
	ObservationRef string                             `json:"observation_ref"`
	ObservedAt     time.Time                          `json:"observed_at"`
	Consistency    work.Consistency                   `json:"consistency"`
	Page           feishu.ProjectDocumentCommentsPage `json:"page"`
}
type ResourceCommentConnector interface {
	ReadResourceComments(context.Context, work.Resource, ReadResourceCommentsRequest) (feishu.ProjectDocumentCommentsPage, error)
}

func (c *FeishuResourceConnector) ReadResourceComments(ctx context.Context, resource work.Resource, req ReadResourceCommentsRequest) (feishu.ProjectDocumentCommentsPage, error) {
	parts := strings.SplitN(resource.SourceIdentity, ":", 2)
	if len(parts) != 2 {
		return feishu.ProjectDocumentCommentsPage{}, fmt.Errorf("resource has no actual document identity")
	}
	fileType := parts[0]
	if fileType == "sheets" {
		fileType = "sheet"
	}
	return c.documents.ReadDocumentCommentsPage(ctx, feishu.ProjectDocumentCommentsRequest{FileToken: parts[1], FileType: fileType, CommentID: req.CommentID, PageToken: req.PageToken, PageSize: req.PageSize})
}

// Each explicit read is journaled independently from document revisions. Its
// threads and reply cursors are partial observations, never complete realtime
// synchronization, mapped text anchors, human acceptance, or resolved work.
func (s *Service) ResourceReadComments(ctx context.Context, token, run string, req ReadResourceCommentsRequest) (ResourceCommentsObservation, error) {
	var result ResourceCommentsObservation
	var resource work.Resource
	var reader ResourceCommentConnector
	var operation work.Operation
	perform := false
	if req.Key == "" || len(req.Key) > 256 || req.ResourceID == "" || req.PageSize < 0 || req.PageSize > 50 {
		return result, &workcmd.Error{Code: "invalid_argument", Message: "stable read key, registered resource and page_size0..50 required"}
	}
	err := s.withWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		id := stableID("resource-comments", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		hash, e := resourceJSONHash(req)
		if e != nil {
			return e
		}
		if existing, e := tx.GetOperation(id); e == nil {
			if existing.RequestHash != hash {
				return workstore.ErrIdempotencyConflict
			}
			if existing.State != work.OperationConfirmed {
				return &workcmd.Error{Code: "blocked", Message: "this read has already started or failed; use a new read key to observe again"}
			}
			return json.Unmarshal([]byte(existing.ExternalReceipt), &result)
		} else if e != workstore.ErrNotFound {
			return e
		}
		scope, e := tx.GetScope(c.Worker.ScopeID)
		if e != nil {
			return e
		}
		if scope.Kind == work.ScopeBootstrap {
			return workstore.ErrScopeDenied
		}
		resource, e = tx.GetResource(scope.ID, req.ResourceID)
		if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(c.Worker.RoleID)
		if e != nil {
			return e
		}
		connector, e := s.resolveResourceConnector(ctx, binding)
		if e != nil {
			return e
		}
		if connector.Identity() != resource.ConnectorIdentity {
			return workstore.ErrScopeDenied
		}
		var ok bool
		reader, ok = connector.(ResourceCommentConnector)
		if !ok {
			return fmt.Errorf("registered connector has no document comments API")
		}
		raw, _ := json.Marshal(req)
		body := work.NewContent(id+":request", scope.ID, "application/json", raw, s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		operation = work.Operation{ID: id, ScopeID: scope.ID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "resource.comments.read", TargetID: resource.ID, TargetGeneration: binding.IdentityGeneration, IdempotencyKey: req.Key, RequestRef: body.ID, RequestHash: hash, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, e = tx.PutOperation(operation, 0); e != nil {
			return e
		}
		operation.State = work.OperationSending
		operation.Revision++
		if _, _, e = tx.PutOperation(operation, 1); e != nil {
			return e
		}
		perform = true
		return nil
	})
	if err != nil || !perform {
		return result, err
	}
	page, callErr := reader.ReadResourceComments(ctx, resource, req)
	result = ResourceCommentsObservation{OperationID: operation.ID, ResourceID: resource.ID, ObservedAt: s.now(), Consistency: work.ConsistencyObserved, Page: page}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		if op.Revision != operation.Revision || op.State != work.OperationSending {
			return workstore.ErrConflict
		}
		expected := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		result.ObservationRef = op.ID + ":receipt"
		raw, e := json.Marshal(result)
		if e != nil {
			return e
		}
		body := work.NewContent(result.ObservationRef, op.ScopeID, "application/json", raw, s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		if callErr != nil {
			op.State = work.OperationTerminal
			op.LastError = callErr.Error()
		} else {
			op.State = work.OperationConfirmed
		}
		op.ExternalReceipt = string(raw)
		op.Steps = append(op.Steps, work.OperationStep{ID: "actual_comments_page", State: op.State, Receipt: body.ID})
		_, _, e = tx.PutOperation(op, expected)
		return e
	})
	if err != nil {
		return result, err
	}
	return result, callErr
}
