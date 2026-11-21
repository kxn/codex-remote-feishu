package workhost

import (
	"context"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Retire stops only this verified, quiescent carrier, retaining a compatible
// native history for the next carrier. Explicit history rebuilds opt out.
func (h *Host) Retire(ctx context.Context, workerID string) error {
	var worker work.WorkerSession
	var carrier workstore.CarrierRegistration
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		worker, e = tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		carrier, e = tx.GetCarrier(worker.CarrierIncarnation)
		return e
	})
	if err != nil {
		return err
	}
	unlock, err := h.preparations.acquire(ctx, worker.ContextID)
	if err != nil {
		return err
	}
	defer unlock()
	// Re-read after acquiring the same context lock used by Prepare.
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		worker, e = tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		carrier, e = tx.GetCarrier(worker.CarrierIncarnation)
		return e
	})
	if err != nil {
		return err
	}
	return h.rebuildCarrier(ctx, &worker, carrier, ctx.Value(freshHistoryKey{}) == nil)
}
