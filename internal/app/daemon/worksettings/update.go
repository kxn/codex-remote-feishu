package worksettings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type updateIntent struct {
	Request   UpdateRequest                     `json:"request"`
	Binding   work.RoleBotBinding               `json:"binding"`
	Desired   state.BotCapabilitySettingsRecord `json:"desired"`
	Evidence  Evidence                          `json:"evidence"`
	Authority workstore.PrivateActionSource     `json:"authority"`
}

func stableID(prefix string, parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return prefix + "_" + hex.EncodeToString(sum[:])
}
func requestHash(request UpdateRequest, source workstore.PrivateActionSource) (string, error) {
	raw, err := json.Marshal(struct {
		Request UpdateRequest
		Source  workstore.PrivateActionSource
	}{request, source})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func replayResult(op work.Operation, hash, principal string) (Result, error) {
	if op.RequestHash != hash || op.Actor != (work.Actor{Kind: work.ActorPrincipal, ID: principal}) {
		return Result{}, workstore.ErrIdempotencyConflict
	}
	result := Result{Status: "accepted", OperationID: op.ID, Replay: true}
	if op.State == work.OperationConfirmed {
		if err := json.Unmarshal([]byte(op.ExternalReceipt), &result); err != nil {
			return result, err
		}
		result.Replay = true
	} else if op.State == work.OperationTerminal {
		result.Status = "failed"
		result.Evidence = op.LastError
	}
	return result, nil
}
func (s *Service) UpdateBot(ctx context.Context, principal string, source workstore.PrivateActionSource, request UpdateRequest) (Result, error) {
	if strings.TrimSpace(request.Key) == "" || len(request.Key) > 256 || request.ExpectedFingerprint == "" {
		return Result{}, fmt.Errorf("settings require an idempotency key and exact expected fingerprint")
	}
	hash, err := requestHash(request, source)
	if err != nil {
		return Result{}, err
	}
	var scope work.WorkScope
	var binding work.RoleBotBinding
	var ref work.SourceRef
	var result Result
	replay := false
	opID := ""
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		scope, ref, e = tx.ValidatePrivateAction(source)
		if e != nil {
			return e
		}
		if scope.PrincipalID != principal || scope.RoleID != request.RoleID {
			return workstore.ErrScopeDenied
		}
		binding, e = tx.GetRoleBotBinding(request.RoleID)
		if e != nil {
			return e
		}
		opID = stableID("settings", scope.ID, principal, request.Key)
		if op, e := tx.GetOperation(opID); e == nil {
			result, e = replayResult(op, hash, principal)
			replay = true
			return e
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		_, binding, ref, e = validateSource(tx, principal, request.RoleID, source)
		return e
	})
	if err != nil || replay {
		return result, err
	}
	if err = s.authorize(ctx, principal, binding, ref); err != nil {
		return result, err
	}
	snapshot, err := s.port.Snapshot(ctx, binding)
	if err != nil {
		return result, err
	}
	if err = s.catalog(ctx, source, binding, &snapshot, request.CatalogCursor); err != nil {
		return result, err
	}
	if snapshot.Fingerprint != request.ExpectedFingerprint {
		return result, ErrConflict
	}
	desired, err := nextSettings(snapshot, request)
	if err != nil {
		return result, err
	}
	intent := updateIntent{Request: request, Binding: binding, Desired: desired, Evidence: Evidence{PrincipalID: principal, Source: ref, OperationID: opID, AcceptedAt: s.now()}, Authority: source}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		_, fresh, actual, e := validateSource(tx, principal, request.RoleID, source)
		if e != nil {
			return e
		}
		if fresh != binding || actual.Kind != ref.Kind || actual.ID != ref.ID || actual.EvidenceRef != ref.EvidenceRef {
			return workstore.ErrScopeDenied
		}
		if op, e := tx.GetOperation(opID); e == nil {
			result, e = replayResult(op, hash, principal)
			replay = true
			return e
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		raw, e := json.Marshal(intent)
		if e != nil {
			return e
		}
		body := work.NewContent(opID+":request", scope.ID, "application/json", raw, s.now())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: opID, ScopeID: scope.ID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: principal}, Kind: "bot.settings.update", TargetID: request.RoleID, TargetGeneration: binding.IdentityGeneration, IdempotencyKey: request.Key, RequestHash: hash, RequestRef: body.ID, State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
		if _, _, e = tx.PutOperation(op, 0); e != nil {
			return e
		}
		op.State = work.OperationSending
		op.Revision++
		_, _, e = tx.PutOperation(op, 1)
		return e
	})
	if err != nil || replay {
		return result, err
	}
	applied, applyErr := s.port.ApplyBot(ctx, binding, request.ExpectedFingerprint, desired, intent.Evidence)
	return s.recordResult(opID, applied, applyErr)
}
func (s *Service) recordResult(id string, applied Applied, cause error) (Result, error) {
	result := Result{Status: "completed", OperationID: id, DesiredRevision: applied.DesiredRevision, Fingerprint: applied.Fingerprint, Evidence: applied.Evidence}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.State == work.OperationConfirmed {
			return json.Unmarshal([]byte(op.ExternalReceipt), &result)
		}
		old := op.Revision
		op.Revision++
		op.UpdatedAt = s.now()
		if cause != nil {
			result.Status = "pending_external"
			op.State = work.OperationUnknown
			op.LastError = cause.Error()
			if errors.Is(cause, ErrConflict) || errors.Is(cause, ErrUnavailable) {
				op.State = work.OperationTerminal
				result.Status = "failed"
			}
		} else {
			receipt, e := json.Marshal(result)
			if e != nil {
				return e
			}
			op.State = work.OperationConfirmed
			op.ExternalReceipt = string(receipt)
		}
		_, _, err = tx.PutOperation(op, old)
		return err
	})
	return result, errors.Join(cause, err)
}
func (s *Service) Reconcile(ctx context.Context, operationID string) (Result, error) {
	var op work.Operation
	var intent updateIntent
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		op, err = tx.GetOperation(operationID)
		if err != nil {
			return err
		}
		if op.Kind != "bot.settings.update" {
			return fmt.Errorf("not a Bot settings operation")
		}
		body, err := tx.GetContent(op.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		return json.Unmarshal(body.Data, &intent)
	})
	if err != nil {
		return Result{}, err
	}
	if op.State == work.OperationConfirmed {
		return replayResult(op, op.RequestHash, op.Actor.ID)
	}
	if op.State != work.OperationUnknown && op.State != work.OperationSending {
		return Result{}, fmt.Errorf("operation has no uncertain settings effect")
	}
	snapshot, err := s.port.Snapshot(ctx, intent.Binding)
	if err != nil {
		return Result{}, err
	}
	current, desired := snapshot.Settings, intent.Desired
	current.UpdatedBy = ""
	current.UpdatedAt = time.Time{}
	desired.UpdatedBy = ""
	desired.UpdatedAt = time.Time{}
	if !reflect.DeepEqual(current, desired) {
		return Result{Status: "blocked", OperationID: operationID, Evidence: "Current desired settings differ; original write remains unattributed and was not repeated."}, nil
	}
	return s.recordResult(operationID, Applied{Fingerprint: snapshot.Fingerprint, Evidence: "Observed the exact accepted desired settings; no native restart or automatic reapplication occurred."}, nil)
}
