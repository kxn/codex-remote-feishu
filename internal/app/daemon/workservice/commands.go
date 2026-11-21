package workservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Result struct {
	Status      string `json:"status"`
	OperationID string `json:"operation_id,omitempty"`
	ObjectID    string `json:"object_id,omitempty"`
	Revision    int64  `json:"revision,omitempty"`
	Replay      bool   `json:"replay"`
}

func stableID(prefix string, parts ...string) string {
	data, _ := json.Marshal(parts)
	hash := sha256.Sum256(data)
	return prefix + "_" + hex.EncodeToString(hash[:])
}

func ensureMember(tx *workstore.Tx, scopeID, roleID string) error {
	return requireMember(tx, scopeID, roleID, true)
}

func ensureEnrolledMember(tx *workstore.Tx, scopeID, roleID string) error {
	return requireMember(tx, scopeID, roleID, false)
}

func requireMember(tx *workstore.Tx, scopeID, roleID string, ready bool) error {
	scope, err := tx.GetScope(scopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopePersonal {
		if scope.RoleID != roleID {
			return workauth.ErrUnauthorized
		}
		return nil
	}
	member, err := tx.GetTeamMember(scope.TeamID, roleID)
	if err != nil || !member.Active {
		return workauth.ErrUnauthorized
	}
	projectMember, err := tx.GetProjectMember(scope.ProjectID, roleID)
	if err != nil || projectMember.State == work.ProjectMemberRemoved || ready && projectMember.State != work.ProjectMemberReady {
		return workauth.ErrUnauthorized
	}
	return nil
}

func mayManageTask(tx *workstore.Tx, caller Caller, task work.Task) error {
	if task.ScopeID != caller.Worker.ScopeID {
		return workauth.ErrUnauthorized
	}
	if task.OwnerRoleID == caller.Worker.RoleID {
		return nil
	}
	scope, err := tx.GetScope(task.ScopeID)
	if err != nil {
		return err
	}
	if scope.Kind == work.ScopeProject {
		project, err := tx.GetProject(scope.ProjectID)
		if err == nil && project.LeaderRoleID == caller.Worker.RoleID {
			return nil
		}
	}
	return workauth.ErrUnauthorized
}

// mutate commits the intent and local effects together. Replaying an accepted
// command returns its original receipt; it never reruns its business effects.
func (s *Service) mutate(tx *workstore.Tx, c Caller, kind, key, target string, request any, apply func() (string, int64, error)) (Result, error) {
	context, contextErr := tx.GetContext(c.Worker.ContextID)
	if contextErr != nil {
		return Result{}, contextErr
	}
	if context.Purpose == work.PurposeReview && kind != "outcome.report" && kind != "message.publish" {
		return Result{}, workauth.ErrUnauthorized
	}
	if strings.TrimSpace(key) == "" || len(key) > 256 {
		return Result{}, fmt.Errorf("an idempotency key of at most 256 bytes is required")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	hash := sha256.Sum256(body)
	actor := work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}
	op := work.Operation{ID: stableID("op", c.Worker.ScopeID, string(actor.Kind), actor.ID, kind, key), ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: actor, Kind: kind, TargetID: target, TargetGeneration: 1, IdempotencyKey: key, RequestHash: hex.EncodeToString(hash[:]), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	requestBody := work.NewContent(stableID("command_request", op.ID), op.ScopeID, "application/json", body, s.now())
	if err = tx.PutContent(requestBody); err != nil {
		return Result{}, err
	}
	op.RequestRef = requestBody.ID
	canonical, created, err := tx.PutOperation(op, 0)
	if err != nil {
		return Result{}, err
	}
	if !created {
		if canonical.State != work.OperationConfirmed {
			return Result{Status: "accepted", OperationID: canonical.ID, Replay: true}, nil
		}
		var result Result
		if err = json.Unmarshal([]byte(canonical.ExternalReceipt), &result); err != nil {
			return Result{}, fmt.Errorf("invalid command receipt: %w", err)
		}
		result.Replay = true
		return result, nil
	}
	op.State, op.Revision = work.OperationSending, 2
	if _, _, err = tx.PutOperation(op, 1); err != nil {
		return Result{}, err
	}
	id, revision, err := apply()
	if err != nil {
		return Result{}, err
	}
	result := Result{Status: "completed", OperationID: op.ID, ObjectID: id, Revision: revision}
	receipt, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	op.State, op.ExternalReceipt, op.Revision = work.OperationConfirmed, string(receipt), 3
	_, _, err = tx.PutOperation(op, 2)
	return result, err
}
