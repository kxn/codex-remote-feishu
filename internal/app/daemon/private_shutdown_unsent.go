package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Called only by Shutdown after its caller joined the private send workers.
// drainMu excludes sendEffect while the exact durable state is rechecked.
func (h *privateNativeHost) settleStoppedPreparedPrompts(ctx context.Context, rec privateCarrierRecord) error {
	h.drainMu.Lock()
	defer h.drainMu.Unlock()
	current, err := h.store.lookup(ctx, rec.Snapshot.Owner.InstanceID)
	if err != nil {
		return err
	}
	if current.State != "draining" || current.Snapshot.Owner != rec.Snapshot.Owner || current.Surface != rec.Surface || current.SurfaceGeneration != rec.SurfaceGeneration {
		return privatechat.ErrJournalConflict
	}
	var work []privatechat.PendingEffectWork
	for offset := 0; ; offset += 128 {
		page, e := h.journal.PendingHostEffectsPage(ctx, 128, offset)
		if e != nil {
			return e
		}
		for _, entry := range page {
			if entry.Owner == rec.Surface && entry.Generation == rec.SurfaceGeneration && (entry.Effect.State == "prepared" || entry.Effect.State == "confirmed") {
				work = append(work, entry)
			}
		}
		if len(page) < 128 {
			break
		}
	}
	if len(work) == 0 {
		return nil
	}
	lease, err := h.lease(ctx, current, nil)
	if err != nil {
		return err
	}
	for _, entry := range work {
		event, _, target, e := privatechat.DecodeStoredEffectTarget(entry.Effect.Effect.Body)
		if e != nil {
			return e
		}
		if target == nil || *target != rec.Snapshot.Owner.InstanceID || event.Command == nil || event.Command.Kind != agentproto.CommandPromptSend {
			continue
		}
		if h.processor == nil {
			return fmt.Errorf("private shutdown processor unavailable")
		}
		if entry.Effect.State == "confirmed" {
			var receipt privatechat.KnownUnsentReceipt
			if json.Unmarshal(entry.Effect.Receipt, &receipt) != nil || receipt.Reason != "shutdown_prepared_unstarted" {
				continue
			}
			if _, e = h.processor.ApplyLocalHostReceipt(ctx, rec.Surface, rec.SurfaceGeneration, entry.Effect.Effect.ID, privatechat.ShutdownKnownUnsentReducer{}); e != nil {
				return e
			}
			continue
		}
		recorded, e := lease.RecordStoppedPreparedUnsent(ctx, entry.Effect.Effect.ID)
		if e != nil {
			return e
		}
		if recorded {
			if _, e = h.processor.ApplyLocalHostReceipt(ctx, rec.Surface, rec.SurfaceGeneration, entry.Effect.Effect.ID, privatechat.ShutdownKnownUnsentReducer{}); e != nil {
				return e
			}
		}
	}
	return nil
}
