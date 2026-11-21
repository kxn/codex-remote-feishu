package workservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
)

// Private navigation is a real human action. Its actor is that verified
// principal, not a synthetic project worker pretending to own the private run.
func (s *Service) privateMutate(tx *workstore.Tx, scope work.WorkScope, kind, key, target string, request any, apply func() (string, int64, error)) (Result, error) {
	if strings.TrimSpace(key) == "" || len(key) > 256 {
		return Result{}, fmt.Errorf("private action requires idempotency key")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(body)
	id := stableID("private_op", scope.ID, kind, key)
	content := work.NewContent(id+":request", scope.ID, "application/json", body, s.now())
	if err = tx.PutContent(content); err != nil {
		return Result{}, err
	}
	operation := work.Operation{ID: id, ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: kind, TargetID: target, TargetGeneration: 1, IdempotencyKey: key, RequestHash: hex.EncodeToString(sum[:]), RequestRef: content.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	canonical, created, err := tx.PutOperation(operation, 0)
	if err != nil {
		return Result{}, err
	}
	if !created {
		var result Result
		if canonical.State != work.OperationConfirmed {
			return Result{Status: "accepted", OperationID: canonical.ID, Replay: true}, nil
		}
		if err = json.Unmarshal([]byte(canonical.ExternalReceipt), &result); err != nil {
			return Result{}, err
		}
		result.Replay = true
		return result, nil
	}
	operation.State = work.OperationSending
	operation.Revision++
	if _, _, err = tx.PutOperation(operation, 1); err != nil {
		return Result{}, err
	}
	object, revision, err := apply()
	if err != nil {
		return Result{}, err
	}
	result := Result{Status: "completed", OperationID: id, ObjectID: object, Revision: revision}
	receipt, err := json.Marshal(result)
	if err != nil {
		return Result{}, err
	}
	operation.State = work.OperationConfirmed
	operation.Revision++
	operation.ExternalReceipt = string(receipt)
	_, _, err = tx.PutOperation(operation, 2)
	return result, err
}
