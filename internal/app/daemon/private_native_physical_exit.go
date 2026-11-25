package daemon

import (
	"context"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
)

// verifyPrivatePhysicalExit establishes physical stop only;
// it never infers completion for a command whose outcome is unknown.
func (h *privateNativeHost) verifyPrivatePhysicalExit(ctx context.Context, rec privateCarrierRecord) (int64, error) {
	if rec.Snapshot.Owner.HostID != h.hostID || rec.Process.PID <= 0 {
		return 0, privatechat.ErrJournalConflict
	}
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if err := hostinventory.WaitProcessExit(short, rec.Process); err != nil {
		return 0, err
	}
	var watermark int64
	err := runtimejournal.VerifyPrivatePhysicalExit(ctx, rec.Snapshot, h.stateDir, rec.Process, &watermark)
	if err != nil {
		return 0, err
	}
	if rec.PhysicalExitProof && rec.PhysicalExitWatermark != watermark {
		return 0, privatechat.ErrJournalConflict
	}
	return watermark, nil
}

// recoverPhysicalExitedCarrier reuses the real outbound/registry retirement fence.
// It is called only for an existing durable shutdown intent.
func (h *privateNativeHost) recoverPhysicalExitedCarrier(ctx context.Context, rec privateCarrierRecord) error {
	h.drainMu.Lock()
	current, err := h.store.lookup(ctx, rec.Snapshot.Owner.InstanceID)
	if err == nil && (current.State != "draining" || current.StopEffectID == "" || current.Snapshot.Owner != rec.Snapshot.Owner || current.Surface != rec.Surface || current.SurfaceGeneration != rec.SurfaceGeneration) {
		err = privatechat.ErrJournalConflict
	}
	var watermark int64
	if err == nil {
		watermark, err = h.verifyPrivatePhysicalExit(ctx, current)
	}
	if err == nil {
		err = h.store.sealPhysicalExit(ctx, current.Snapshot.Owner.InstanceID, watermark)
	}
	if err == nil {
		err = h.outbound.Drain(ctx, privateRegistration(current.Snapshot.Owner))
	}
	if err == nil {
		h.markSealed(current.Snapshot.Owner.InstanceID)
	}
	h.drainMu.Unlock()
	if err != nil {
		return err
	}
	return h.retire(ctx, current.Snapshot.Owner.InstanceID)
}
