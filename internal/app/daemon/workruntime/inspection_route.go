package workruntime

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// An exited wrapper cannot answer another RPC. Its committed inspection can
// only be reused after the exact process and both durable write fences prove
// that no execution could have followed it. A transient disconnect is not proof.
func (c *Coordinator) observeAttemptQuiescence(ctx context.Context, request InspectionRequest) (NativeObservation, error) {
	var connected bool
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		connected, err = tx.CarrierConnected(request.Carrier.Incarnation)
		return err
	}); err != nil {
		return NativeObservation{}, err
	}
	if !connected {
		return VerifyRetiredInspection(ctx, c.store, c.options.StateDir, c.options.HostID, request)
	}
	return c.options.Driver.Inspect(ctx, request)
}

// Settling an old attempt releases its resources, not its dead carrier for
// reuse. Preserve the exact receipt association until normal preparation has
// rebuilt the worker for the next real input.
func (c *Coordinator) requireRetiredCarrierRebuild(tx *workstore.Tx, request InspectionRequest) error {
	worker, err := tx.GetWorker(request.Binding.WorkerSessionID)
	if err != nil {
		return err
	}
	if !request.Carrier.Matches(worker) {
		return workstore.ErrConflict
	}
	previous := worker.Revision
	worker.State = work.WorkerRecovering
	worker.Revision++
	worker.UpdatedAt = c.options.Clock()
	if err := tx.PutWorker(worker, previous); err != nil {
		return err
	}
	return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: worker.ID, ScopeID: worker.ScopeID, AttemptID: request.Binding.AttemptID, Reason: "snapshot_rebuild_required", CreatedAt: worker.UpdatedAt})
}

// Keep the final settlement transaction behind the same carrier and write
// boundary; a reconnect or newly registered mutation invalidates retirement.
func validateRetirementCommit(tx *workstore.Tx, request InspectionRequest, observation NativeObservation) error {
	if !observation.RetiredCarrierVerified {
		return nil
	}
	if observation.RetirementEvidence == nil {
		return workstore.ErrConflict
	}
	worker, err := tx.GetWorker(request.Binding.WorkerSessionID)
	if err != nil {
		return err
	}
	if _, err := retiredInspectionState(tx, worker, request); err != nil {
		return err
	}
	return tx.AssertNoEffectfulCommandsAfter(request.Carrier.Incarnation, observation.CanonicalCommandWatermark)
}
