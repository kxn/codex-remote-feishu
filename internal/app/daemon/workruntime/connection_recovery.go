package workruntime

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RecoverConnectionsOnStartup must run before accepting any new Hello. A
// previous daemon's connected bit is not evidence of a current socket. Expire
// it through ordinary disconnect transitions without claiming the OS exited,
// releasing an attempt, or replaying a native command.
func (c *Coordinator) RecoverConnectionsOnStartup(ctx context.Context) error {
	var instances []string
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		instances, err = tx.ListConnectedCarrierInstances()
		return err
	}); err != nil {
		return err
	}
	for _, instance := range instances {
		if err := c.Disconnect(ctx, instance); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) disconnectCarrier(tx *workstore.Tx, carrier workstore.CarrierRegistration) error {
	if err := tx.SetCarrierConnected(carrier.Incarnation, false); err != nil {
		return err
	}
	if err := tx.InvalidateContinuationGrants(carrier.Incarnation); err != nil {
		return err
	}
	worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
	if err != nil {
		return err
	}
	if !carrier.Matches(worker) {
		return nil
	}
	gate, lookup := tx.GetRuntimeGate(worker.ID)
	if lookup != nil && !errors.Is(lookup, workstore.ErrNotFound) {
		return lookup
	}
	if lookup == nil && gate.Reason == "snapshot_rebuild_required" && gate.RecoveryReason == "" {
		occupied, err := tx.HasAnyCarrierOccupancy(carrier)
		if err != nil {
			return err
		}
		if !occupied {
			return nil
		}
	}
	if worker.State == work.WorkerRetired || worker.State == work.WorkerUnavailable {
		return nil
	}
	if worker.State != work.WorkerRecovering {
		previous := worker.Revision
		worker.Revision++
		worker.State, worker.UpdatedAt = work.WorkerRecovering, c.options.Clock()
		if err := tx.PutWorker(worker, previous); err != nil {
			return err
		}
	}
	// Keep explicit recovery decisions and their exact attempt association. A
	// transport loss must not turn those gates into an automatic idle recovery.
	if lookup == nil && protectedDisconnectGate(gate) {
		return nil
	}
	attemptID := gate.AttemptID
	if attemptID == "" {
		attempt, err := tx.GetOccupiedAttemptForWorker(worker.ID)
		if err == nil {
			attemptID = attempt.ID
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
	}
	return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: attemptID, Reason: "carrier_disconnected_reconcile", CreatedAt: c.options.Clock()})
}

func protectedDisconnectGate(gate workstore.RuntimeGate) bool {
	if gate.RecoveryReason != "" {
		return true
	}
	switch gate.Reason {
	case "native_reconciliation", "carrier_disconnected_reconcile", "native_ownership_unproven", "native_command_rejected", "native_command_unknown":
		return false
	default:
		return true
	}
}
