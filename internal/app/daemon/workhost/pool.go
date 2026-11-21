package workhost

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrNativePoolCapacity = errors.New("native carrier pool capacity retained by active or unverified carriers")

func (h *Host) ensurePoolCapacity(ctx context.Context, contextID string, purpose work.ContextPurpose) error {
	var available bool
	var candidates []work.WorkerSession
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		available, err = h.poolAvailable(tx, purpose)
		if err != nil || available {
			return err
		}
		candidates, err = tx.NativePoolRetirementCandidates(h.config.HostID, 32)
		return err
	})
	if err != nil || available {
		return err
	}
	if h.config.Runtime == nil || h.config.StopCarrier == nil {
		return ErrNativePoolCapacity
	}
	for _, candidate := range candidates {
		if candidate.ContextID == contextID {
			continue
		}
		unlock, ok := h.preparations.tryAcquire(candidate.ContextID)
		if !ok {
			continue
		}
		var carrier workstore.CarrierRegistration
		err = h.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			candidate, e = tx.GetWorker(candidate.ID)
			if e != nil {
				return e
			}
			carrier, e = tx.GetCarrier(candidate.CarrierIncarnation)
			return e
		})
		if err == nil {
			err = h.rebuildCarrier(ctx, &candidate, carrier)
		}
		unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			continue
		} // Failed inspection leaves its durable occupancy intact.
		err = h.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			available, e = h.poolAvailable(tx, purpose)
			return e
		})
		if err != nil {
			return err
		}
		if available {
			return nil
		}
	}
	return ErrNativePoolCapacity
}

func (h *Host) poolAvailable(tx *workstore.Tx, purpose work.ContextPurpose) (bool, error) {
	count, err := tx.CountNativePoolCarriers(h.config.HostID)
	if err != nil || count >= h.config.MaxLiveCarriers {
		return false, err
	}
	if purpose == work.PurposeControl {
		return true, nil
	}
	count, err = tx.CountBusinessNativePoolCarriers(h.config.HostID)
	return err == nil && count < h.config.MaxLiveCarriers-h.config.ControlReservedCarriers, err
}
