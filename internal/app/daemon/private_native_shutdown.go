package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
)

// Shutdown stops private carriers through the durable native stop protocol.
// Relay reception must remain active until this returns. Call only after all
// launch workers stopped. Sending launches remain uncertain until authenticated.
func (h *privateNativeHost) Shutdown(ctx context.Context) error {
	if h == nil || h.store == nil || h.registry == nil || h.outbound == nil || h.journal == nil {
		return fmt.Errorf("private native host unavailable")
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		pending := 0
		for _, state := range []string{"prepared", "aborted", "sending", "running", "draining", "sealed"} {
			records, err := h.store.byState(ctx, state)
			if err != nil {
				return err
			}
			for _, rec := range records {
				if err = ctx.Err(); err != nil {
					return err
				}
				pending++
				if state == "prepared" || state == "aborted" {
					if err = h.abortPrepared(ctx, rec); err == nil {
						pending--
					}
					continue
				}
				if state == "sending" {
					continue
				}
				id := rec.StopEffectID
				if id == "" {
					id = "daemon-shutdown:" + rec.Snapshot.Owner.CarrierIncarnation
				}
				if err = h.advanceCarrierStop(ctx, rec, id); err == nil {
					pending--
				}
			}
		}
		if pending == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (h *privateNativeHost) abortPrepared(ctx context.Context, rec privateCarrierRecord) error {
	instance := rec.Snapshot.Owner.InstanceID
	if rec.State == "prepared" {
		if rec.Process.PID != 0 {
			return executionowner.ErrConflict
		}
		if err := h.store.transition(ctx, instance, "prepared", "aborted", rec.Process); err != nil {
			return err
		}
	}
	reg := privateRegistration(rec.Snapshot.Owner)
	_, err := h.registry.Resolve(ctx, reg.Carrier, reg.Owner.Epoch)
	if err == nil {
		if err = h.outbound.Drain(ctx, reg); err != nil {
			return err
		}
		err = h.outbound.Retire(ctx, reg, "unspawned:"+reg.Carrier.Incarnation)
	} else if errors.Is(err, executionowner.ErrNotFound) {
		err = nil
	} else {
		// Exact idempotent replay if registry retirement already committed.
		err = h.registry.Retire(ctx, reg, "unspawned:"+reg.Carrier.Incarnation)
	}
	if err != nil {
		return err
	}
	return h.store.transition(ctx, instance, "aborted", "retired", rec.Process)
}

// restoreAborted completes the exact unspawned retirement after a crash.
// It runs before exposing senders or restarting launch workers.
func (h *privateNativeHost) restoreAborted(ctx context.Context) error {
	records, err := h.store.byState(ctx, "aborted")
	if err != nil {
		return err
	}
	for _, rec := range records {
		if err = h.abortPrepared(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}
