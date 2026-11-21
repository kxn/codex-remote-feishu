package workruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReconcileIdleCarriers handles disconnected work that has no outstanding
// Attempt to drive the ordinary reconciliation loop.
func (c *Coordinator) ReconcileIdleCarriers(ctx context.Context, limit int) error {
	if c.options.Driver == nil {
		return fmt.Errorf("native driver unavailable")
	}
	var workers []work.WorkerSession
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		workers, err = tx.ListConnectedIdleRecoveryWorkers(limit)
		return err
	}); err != nil {
		return err
	}
	var result error
	for _, worker := range workers {
		if ctx.Err() != nil {
			return errors.Join(result, ctx.Err())
		}
		result = errors.Join(result, c.reconcileIdleCarrier(ctx, worker))
	}
	return result
}

func (c *Coordinator) reconcileIdleCarrier(ctx context.Context, worker work.WorkerSession) error {
	var request InspectionRequest
	var gate workstore.RuntimeGate
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, err := tx.GetCarrier(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		gate, err = idleRecoveryState(tx, worker, carrier)
		if err != nil {
			return err
		}
		request = InspectionRequest{Carrier: carrier, Binding: carrier.Owner, SessionID: worker.NativeSessionID, LocationID: worker.LocationID, LocationGeneration: worker.LocationGeneration}
		return nil
	}); err != nil {
		return err
	}
	observation, err := c.options.Driver.Inspect(ctx, request)
	if err != nil {
		return err
	}
	if err = observation.ValidateStopped(request); err != nil {
		return err
	}
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := idleRecoveryState(tx, worker, request.Carrier)
		if err != nil {
			return err
		}
		if current.Revision != gate.Revision {
			return workstore.ErrConflict
		}
		worker.State = work.WorkerReady
		worker.Revision++
		worker.UpdatedAt = c.options.Clock()
		if err = tx.PutWorker(worker, worker.Revision-1); err != nil {
			return err
		}
		return tx.ClearRuntimeGate(worker.ID, gate.Reason)
	})
}

func idleRecoveryState(tx *workstore.Tx, expected work.WorkerSession, carrier workstore.CarrierRegistration) (workstore.RuntimeGate, error) {
	current, err := tx.GetWorker(expected.ID)
	if err != nil {
		return workstore.RuntimeGate{}, err
	}
	if current.Revision != expected.Revision || current.State != work.WorkerRecovering || current.Controller != work.ControllerAutomatic || !carrier.Matches(current) {
		return workstore.RuntimeGate{}, workstore.ErrConflict
	}
	gate, err := tx.GetRuntimeGate(current.ID)
	if err != nil {
		return gate, err
	}
	if gate.Reason != "carrier_disconnected_reconcile" || gate.RecoveryReason != "" || gate.AttemptID != "" {
		return gate, workstore.ErrConflict
	}
	connected, err := tx.CarrierConnected(carrier.Incarnation)
	if err != nil {
		return gate, err
	}
	occupied, err := tx.HasAnyCarrierOccupancy(carrier)
	if err != nil {
		return gate, err
	}
	unresolved, err := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
	if err != nil {
		return gate, err
	}
	if !connected || occupied || unresolved != 0 {
		return gate, workstore.ErrConflict
	}
	return gate, nil
}
