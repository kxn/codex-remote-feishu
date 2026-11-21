package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type compactRequest struct {
	Authority          workstore.PrivateActionSource `json:"authority"`
	WorkerID           string                        `json:"worker_id"`
	Generation         int64                         `json:"generation"`
	CarrierIncarnation string                        `json:"carrier_incarnation"`
	NativeSessionID    string                        `json:"native_session_id"`
}

// RequestCompact queues a safe history rebuild for backends without a proven
// native compact RPC. It preserves scoped memory, checkpoints and the original
// native history as evidence; it does not claim native context compression.
func (h *Host) RequestCompact(ctx context.Context, workerID, requestID string, authority workstore.PrivateActionSource) (string, error) {
	if requestID == "" || authority.MessageID != "" {
		return "", fmt.Errorf("compact requires an exact trusted human action")
	}
	id := idFor("compact_rebuild", requestID)
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, err := tx.ValidatePrivateAction(authority)
		if err != nil {
			return err
		}
		worker, err := tx.GetWorker(workerID)
		if err != nil {
			return err
		}
		if worker.ScopeID != scope.ID || worker.RoleID != scope.RoleID || worker.Controller != work.ControllerAutomatic || worker.Backend == work.BackendCodex {
			return fmt.Errorf("safe compact rebuild requires this human's exact non-Codex worker")
		}
		if old, e := tx.GetOperation(id); e == nil {
			if old.Kind != "worker.compact.rebuild" || old.TargetID != worker.ID || old.ScopeID != scope.ID {
				return workstore.ErrIdempotencyConflict
			}
			previous, e := tx.GetContent(scope.ID, old.RequestRef)
			if e != nil {
				return e
			}
			var saved compactRequest
			if e = json.Unmarshal(previous.Data, &saved); e != nil {
				return e
			}
			if saved.Authority != authority {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		request := compactRequest{Authority: authority, WorkerID: worker.ID, Generation: worker.Generation, CarrierIncarnation: worker.CarrierIncarnation, NativeSessionID: worker.NativeSessionID}
		raw, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body := work.NewContent(id+":request", scope.ID, "application/json", raw, h.config.Now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		op := work.Operation{ID: id, ScopeID: scope.ID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Kind: "worker.compact.rebuild", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: id, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationPrepared, Revision: 1, CreatedAt: h.config.Now(), UpdatedAt: h.config.Now()}
		_, _, err = tx.PutOperation(op, 0)
		return err
	})
	return id, err
}

func (h *Host) RunCompactions(ctx context.Context, limit int) error {
	var operations []work.Operation
	if err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		operations, e = tx.ListHostCompactions(h.config.HostID, limit)
		return e
	}); err != nil {
		return err
	}
	var failures []error
	for _, operation := range operations {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := h.advanceCompaction(ctx, operation); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (h *Host) advanceCompaction(ctx context.Context, operation work.Operation) error {
	var request compactRequest
	var worker work.WorkerSession
	var wc work.WorkContext
	ready := false
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		body, e := tx.GetContent(operation.ScopeID, operation.RequestRef)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(body.Data, &request); e != nil {
			return e
		}
		scope, _, e := tx.ValidatePrivateAction(request.Authority)
		if e != nil {
			return e
		}
		worker, e = tx.GetWorker(request.WorkerID)
		if e != nil {
			return e
		}
		if scope.ID != worker.ScopeID || worker.ID != operation.TargetID || request.Generation != operation.TargetGeneration || worker.Controller != work.ControllerAutomatic || (worker.Generation != request.Generation && worker.Generation != request.Generation+1) {
			return workstore.ErrConflict
		}
		wc, e = tx.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		if worker.Generation == request.Generation {
			if worker.CarrierIncarnation != request.CarrierIncarnation {
				return workstore.ErrConflict
			}
			if worker.State != work.WorkerReady && worker.State != work.WorkerRecovering {
				return nil
			}
		}
		ready = true
		if operation.State == work.OperationPrepared {
			before := operation.Revision
			operation.State = work.OperationSending
			operation.Revision++
			operation.UpdatedAt = h.config.Now()
			_, _, e = tx.PutOperation(operation, before)
			return e
		}
		return nil
	})
	if err != nil || !ready {
		return err
	}
	if worker.Generation == request.Generation {
		if err = h.Retire(ctx, worker.ID); err != nil {
			return err
		}
	}
	if err = h.Prepare(ctx, workengine.WorkerNeed{ScopeID: worker.ScopeID, RoleID: worker.RoleID, ContextID: worker.ContextID, TaskID: wc.TaskID}); err != nil {
		return err
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		if current.Generation != request.Generation+1 || current.CarrierIncarnation == request.CarrierIncarnation {
			return workstore.ErrConflict
		}
		op, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		if op.State == work.OperationConfirmed {
			return nil
		}
		launch, e := tx.GetOperation(launchID(current))
		if e != nil {
			return e
		}
		if launch.State != work.OperationConfirmed {
			return fmt.Errorf("replacement carrier launch still requires observation")
		}
		receipt, _ := json.Marshal(map[string]any{"semantics": "verified_fresh_history_rebuild", "previous_incarnation": request.CarrierIncarnation, "previous_session": request.NativeSessionID, "carrier_incarnation": current.CarrierIncarnation, "generation": current.Generation, "checkpoint": wc.Checkpoint})
		before := op.Revision
		op.State = work.OperationConfirmed
		op.Revision++
		op.ExternalReceipt = string(receipt)
		op.UpdatedAt = h.config.Now()
		_, _, e = tx.PutOperation(op, before)
		return e
	})
}
