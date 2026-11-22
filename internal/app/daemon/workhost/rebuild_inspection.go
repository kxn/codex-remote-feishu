package workhost

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (h *Host) retiredRebuildAttempt(ctx context.Context, worker work.WorkerSession) (string, error) {
	var attemptID string
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		gate, err := tx.GetRuntimeGate(worker.ID)
		if err != nil {
			return err
		}
		if gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "" {
			return workstore.ErrScopeDenied
		}
		attemptID = gate.AttemptID
		if attemptID != "" {
			return nil
		}
		attemptID, err = tx.CarrierQuiescenceAttempt(worker)
		if err != nil {
			return err
		}
		gate.AttemptID = attemptID
		return tx.PutRuntimeGate(gate)
	})
	return attemptID, err
}

func (h *Host) inspectRebuildingCarrier(ctx context.Context, worker work.WorkerSession, request workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	var connected bool
	if err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		connected, err = tx.CarrierConnected(worker.CarrierIncarnation)
		return err
	}); err != nil {
		return workruntime.NativeObservation{}, err
	}
	if !connected {
		attemptID, err := h.retiredRebuildAttempt(ctx, worker)
		if err == nil {
			return workruntime.VerifyRetiredQuiescence(ctx, h.store, h.config.Paths.StateDir, h.config.HostID, worker, attemptID)
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return workruntime.NativeObservation{}, err
		}
	}
	// A live carrier, or a prepared launch without a previous stop receipt,
	// still needs its real inspection. A failed RPC never proves exit.
	observation, err := h.config.Runtime.Inspect(ctx, request)
	if err == nil {
		return observation, nil
	}
	attemptID, proofErr := h.retiredRebuildAttempt(ctx, worker)
	if proofErr != nil {
		return observation, errors.Join(err, proofErr)
	}
	observation, proofErr = workruntime.VerifyRetiredQuiescence(ctx, h.store, h.config.Paths.StateDir, h.config.HostID, worker, attemptID)
	if proofErr != nil {
		return observation, errors.Join(err, proofErr)
	}
	return observation, nil
}
