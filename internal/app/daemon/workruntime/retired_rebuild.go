package workruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// VerifyRetiredRebuild verifies the exact owner-wide inspection saved by the
// host's completed rebuild, plus canonical/native write fences and OS exit. It
// does not invent an attempt for a carrier which never ran one.
func VerifyRetiredRebuild(ctx context.Context, store *workstore.Store, stateDir, hostID string, worker work.WorkerSession, operationID string) error {
	var observed NativeObservation
	var carrier workstore.CarrierRegistration
	check := func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(worker.ID)
		if err != nil {
			return err
		}
		if current.Revision != worker.Revision || current.State != work.WorkerRecovering || current.Controller != work.ControllerAutomatic {
			return workstore.ErrConflict
		}
		carrier, err = tx.GetCarrier(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		if !carrier.Matches(current) {
			return workstore.ErrConflict
		}
		gate, err := tx.GetRuntimeGate(worker.ID)
		if err != nil {
			return err
		}
		if gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "" {
			return workstore.ErrConflict
		}
		occupied, err := tx.HasAnyCarrierOccupancy(carrier)
		if err != nil {
			return err
		}
		if occupied {
			return workstore.ErrConflict
		}
		count, err := tx.CountControllerOccupancy(worker.ID)
		if err != nil {
			return err
		}
		if count != 0 {
			return workstore.ErrConflict
		}
		unresolved, err := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
		if err != nil {
			return err
		}
		if unresolved != 0 {
			return workstore.ErrConflict
		}
		op, err := tx.GetOperation(operationID)
		if err != nil {
			return err
		}
		if op.Kind != "worker.rebuild" || op.ScopeID != worker.ScopeID || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation || op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
			return workstore.ErrConflict
		}
		body, err := tx.GetContent(worker.ScopeID, op.ExternalReceipt)
		if err != nil {
			return err
		}
		if json.Unmarshal(body.Data, &observed) != nil {
			return workstore.ErrConflict
		}
		req := InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		if err = observed.ValidateStopped(req); err != nil {
			return err
		}
		committed, err := tx.LatestCommittedInspection(carrier.Incarnation, carrier.Owner, req.SessionID, req.TurnID)
		if err != nil {
			return err
		}
		if committed.Command.CommandID != observed.InspectionCommandID || committed.Source != observed.Source || committed.CanonicalWatermark != observed.CanonicalCommandWatermark || committed.Event.RuntimeInspection == nil || committed.Event.RuntimeInspection.NativeWriteWatermark != observed.NativeWriteWatermark {
			return fmt.Errorf("rebuild inspection evidence changed")
		}
		actual, err := ObserveRuntimeInspection(req, *committed.Event.RuntimeInspection)
		if err != nil {
			return err
		}
		if err = actual.ValidateStopped(req); err != nil {
			return err
		}
		return tx.AssertNoEffectfulCommandsAfter(carrier.Incarnation, observed.CanonicalCommandWatermark)
	}
	if err := store.Within(ctx, check); err != nil {
		return err
	}
	if _, err := verifyRetiredCarrierFence(ctx, store, stateDir, hostID, worker, observed); err != nil {
		return err
	}
	return store.Within(ctx, check)
}
