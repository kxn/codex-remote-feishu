package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type HumanProjectLifecycleRequest struct {
	ProjectLifecycleRequest
	Action string `json:"action"`
}

// ApplyHumanProjectLifecycle is exclusively a trusted human ingress port. The
// authenticated principal is supplied by that ingress, not Agent JSON. It lets a
// real user reopen/resume a stopped project without inventing a running worker.
func (s *Service) ApplyHumanProjectLifecycle(ctx context.Context, principalID string, req HumanProjectLifecycleRequest) (Result, error) {
	var result Result
	if req.Action != "pause" && req.Action != "resume" && req.Action != "archive" && req.Action != "reopen" {
		return result, fmt.Errorf("unknown project lifecycle action")
	}
	if strings.TrimSpace(req.Key) == "" || len(req.Key) > 256 {
		return result, fmt.Errorf("idempotency key required (maximum 256 bytes)")
	}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		return s.applyHumanProjectLifecycleTx(tx, principalID, req, &result)
	})
	return result, err
}

func (s *Service) applyHumanProjectLifecycleTx(tx *workstore.Tx, principalID string, req HumanProjectLifecycleRequest, result *Result) error {
	project, err := tx.GetProject(req.ProjectID)
	if err != nil {
		return err
	}
	message, err := tx.GetMessage(req.SourceMessageID)
	if err != nil {
		return err
	}
	actor := work.Actor{Kind: work.ActorPrincipal, ID: principalID}
	if principalID == "" || message.Source != actor || message.ScopeID != project.ScopeID {
		return workauth.ErrUnauthorized
	}
	if _, err = tx.GetPrincipal(principalID); err != nil {
		return err
	}
	if message.PlatformKey != nil {
		channel, err := tx.GetProjectChannelBinding(message.PlatformKey.Tenant, message.PlatformKey.ChatID)
		if err != nil {
			return err
		}
		if !channel.Active || channel.ProjectID != project.ID {
			return workauth.ErrUnauthorized
		}
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	kind := "project." + req.Action
	opID := stableID("human_project_operation", project.ScopeID, principalID, kind, req.Key)
	body := work.NewContent(stableID("human_project_request", opID), project.ScopeID, "application/json", raw, s.now())
	if err = tx.PutContent(body); err != nil {
		return err
	}
	op := work.Operation{ID: opID, ScopeID: project.ScopeID, Actor: actor, Kind: kind, TargetID: project.ID, TargetGeneration: 1, IdempotencyKey: req.Key, RequestHash: hex.EncodeToString(sum[:]), RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	old, created, err := tx.PutOperation(op, 0)
	if err != nil {
		return err
	}
	if !created {
		if err = json.Unmarshal([]byte(old.ExternalReceipt), result); err != nil {
			return err
		}
		result.Replay = true
		return nil
	}
	if project.Revision != req.ExpectedRevision {
		return workstore.ErrConflict
	}
	op.State = work.OperationSending
	op.Revision++
	if _, _, err = tx.PutOperation(op, 1); err != nil {
		return err
	}
	project, err = applyProjectTransition(tx, project, kind)
	if err != nil {
		return err
	}
	status := "completed"
	if req.Action == "pause" || req.Action == "archive" {
		status = "accepted"
	}
	*result = Result{Status: status, OperationID: op.ID, ObjectID: project.ID, Revision: project.Revision}
	receipt, err := json.Marshal(result)
	if err != nil {
		return err
	}
	op.State = work.OperationConfirmed
	op.Revision++
	op.ExternalReceipt = string(receipt)
	_, _, err = tx.PutOperation(op, 2)
	return err
}
