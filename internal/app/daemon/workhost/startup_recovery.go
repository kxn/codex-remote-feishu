package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReconcileStartupFailures releases failed bootstrap reservations only after a
// durable pre-Hello receipt and independent proof that the exact wrapper quit.
func (h *Host) ReconcileStartupFailures(ctx context.Context) error {
	var candidates []work.WorkerSession
	if err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		candidates, err = tx.NativeStartupCandidates(h.config.HostID)
		return err
	}); err != nil {
		return err
	}
	var result error
	for _, worker := range candidates {
		unlock, acquired := h.preparations.tryAcquire(worker.ContextID)
		if !acquired {
			continue
		}
		err := h.reconcileStartupFailure(ctx, worker)
		unlock()
		result = errors.Join(result, err)
	}
	return result
}

func (h *Host) reconcileStartupFailure(ctx context.Context, worker work.WorkerSession) error {
	var record LaunchRecord
	if err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		record, err = h.readLaunch(tx, worker)
		return err
	}); err != nil {
		if errors.Is(err, workstore.ErrConflict) {
			return nil
		}
		return err
	}
	if record.Snapshot.HostID != h.config.HostID {
		return fmt.Errorf("startup failure carrier belongs to another host")
	}
	receipt, err := runtimejournal.ReadStartupFailure(record.Snapshot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	identity := receipt.Process
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err = hostinventory.WaitProcessExit(wait, hostinventory.ProcessIdentity{Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}); err != nil {
		return err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(worker.ID)
		if err != nil {
			return err
		}
		if current.Revision != worker.Revision || current.CarrierIncarnation != worker.CarrierIncarnation {
			return workstore.ErrConflict
		}
		carrier, err := tx.GetCarrier(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		connected, err := tx.CarrierConnected(carrier.Incarnation)
		if err != nil {
			return err
		}
		occupied, err := tx.HasAnyCarrierOccupancy(carrier)
		if err != nil {
			return err
		}
		if !carrier.Matches(current) || connected || occupied || current.Controller != work.ControllerAutomatic || (current.State != work.WorkerPreparing && current.State != work.WorkerRecovering) {
			return workstore.ErrConflict
		}
		gate, err := tx.GetRuntimeGate(current.ID)
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if err == nil && (gate.RecoveryReason != "" || gate.AttemptID != "" || (gate.Reason != "snapshot_rebuild_required" && gate.Reason != "carrier_disconnected_reconcile")) {
			return workstore.ErrConflict
		}
		hasCommands, err := tx.CarrierHasCommands(carrier.Incarnation)
		if err != nil {
			return err
		}
		if hasCommands {
			return fmt.Errorf("startup failure carrier has already accepted commands")
		}
		launch, err := tx.GetOperation(launchID(current))
		if err != nil {
			return err
		}
		switch launch.State {
		case work.OperationConfirmed:
			var started agentproto.RuntimeProcessIdentity
			if err = json.Unmarshal([]byte(launch.ExternalReceipt), &started); err != nil || started.PID != identity.PID {
				return fmt.Errorf("startup failure does not match the recorded spawn")
			}
		case work.OperationSending, work.OperationUnknown:
		default:
			return workstore.ErrConflict
		}
		if err = tx.PutCarrierProcess(carrier.Incarnation, worker.InstanceID, identity); err != nil {
			return err
		}
		now := h.config.Now()
		body := work.NewContent(idFor("startup_failure", carrier.Incarnation), worker.ScopeID, "application/json", raw, now)
		if err = tx.PutContent(body); err != nil {
			return err
		}
		launch.LastError = "wrapper exited before readiness: " + receipt.Stage
		launch.UpdatedAt = now
		launch.Revision++
		if _, _, err = tx.PutOperation(launch, launch.Revision-1); err != nil {
			return err
		}
		id := rebuildID(worker)
		retryAt := now.Add(10 * time.Second)
		rebuild := work.Operation{ID: id, Kind: "worker.rebuild", ScopeID: worker.ScopeID, ContextID: worker.ContextID, TargetID: worker.ID,
			TargetGeneration: worker.Generation, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, IdempotencyKey: id,
			State: work.OperationConfirmed, RequestHash: body.SHA256, RequestRef: body.ID, ExternalReceipt: body.ID, NextAttemptAt: &retryAt,
			Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, err = tx.PutOperation(rebuild, 0); err != nil {
			return err
		}
		current.State = work.WorkerRecovering
		current.Revision++
		current.UpdatedAt = now
		if err = tx.PutWorker(current, current.Revision-1); err != nil {
			return err
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, Reason: "snapshot_rebuild_required", CreatedAt: now})
	})
}
