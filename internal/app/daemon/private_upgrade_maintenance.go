package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (h *privateNativeHost) WithUpgradePause(ctx context.Context, plan privatechat.UpgradeRecoveryPlan, commit func() error) error {
	if h == nil || h.store == nil || h.registry == nil || plan.OldOwner.HostID != h.hostID {
		return executionowner.ErrConflict
	}
	rec, err := h.store.lookup(ctx, plan.OldOwner.InstanceID)
	if err != nil {
		return err
	}
	if rec.Snapshot.Owner != plan.OldOwner || rec.Surface != plan.Owner || rec.SurfaceGeneration != plan.Generation || rec.Snapshot.CWD != plan.CWD || state.NormalizeCodexProfileID(rec.CodexProfileID) != plan.ProfileID {
		return executionowner.ErrConflict
	}
	m, err := h.registry.MaintenanceStatus(ctx, string(plan.OldOwner.Backend))
	if err != nil {
		return err
	}
	if m.ID != plan.MaintenanceID || m.Epoch != plan.MaintenanceEpoch || m.Phase != "draining" {
		return executionowner.ErrConflict
	}
	return h.registry.WithMaintenancePause(ctx, m, privateRegistration(plan.OldOwner), commit)
}

func (h *privateNativeHost) WithUpgradeRecovery(ctx context.Context, plan privatechat.UpgradeRecoveryPlan, commit func() error) error {
	if h == nil || h.store == nil || h.registry == nil || plan.OldOwner.HostID != h.hostID {
		return executionowner.ErrConflict
	}
	rec, err := h.store.lookup(ctx, plan.OldOwner.InstanceID)
	if err != nil {
		return err
	}
	if rec.State != "retired" || rec.Snapshot.Owner != plan.OldOwner || rec.Surface != plan.Owner || rec.SurfaceGeneration != plan.Generation || rec.Snapshot.CWD != plan.CWD || state.NormalizeCodexProfileID(rec.CodexProfileID) != plan.ProfileID {
		return executionowner.ErrConflict
	}
	m, err := h.registry.MaintenanceStatus(ctx, string(plan.OldOwner.Backend))
	if err != nil {
		return err
	}
	if m.ID != plan.MaintenanceID || m.Epoch != plan.MaintenanceEpoch || m.Phase != "restoring" {
		return executionowner.ErrConflict
	}
	return h.registry.WithMaintenanceRecoveryPreparation(ctx, m, privateRegistration(plan.OldOwner), commit)
}

func (h *privateNativeHost) WithReleasedUpgrade(ctx context.Context, plan privatechat.UpgradeRecoveryPlan, newInstance string, commit func() error) error {
	if h == nil || h.registry == nil || plan.OldOwner.HostID != h.hostID {
		return executionowner.ErrConflict
	}
	m, err := h.registry.MaintenanceStatus(ctx, string(plan.OldOwner.Backend))
	if err != nil {
		return err
	}
	if m.ID != plan.MaintenanceID || m.Epoch != plan.MaintenanceEpoch || m.Phase != "released" {
		return executionowner.ErrConflict
	}
	return h.registry.WithReleasedRecovery(ctx, m, privateRegistration(plan.OldOwner), newInstance, commit)
}

// WithMaintenanceRecovery keeps authenticated hello ownership stable until
// the registry commits the exact replacement's readiness receipt.
func (h *privateNativeHost) WithMaintenanceRecovery(ctx context.Context, m executionowner.Maintenance, old executionowner.Registration, next *executionowner.Registration, evidence string, commit func() error) error {
	if h == nil || next == nil || commit == nil || evidence != "private-ready:"+m.ID+":"+next.Carrier.Incarnation || old.Owner.Application != executionowner.Private || next.Owner != old.Owner {
		return executionowner.ErrConflict
	}
	h.mu.Lock()
	connection := h.connections[next.Carrier.InstanceID]
	h.mu.Unlock()
	if connection == 0 || h.relay == nil {
		return executionowner.ErrConflict
	}
	return h.relay.WithCurrentConnection(next.Carrier.InstanceID, connection, func() error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.connections[next.Carrier.InstanceID] != connection {
			return executionowner.ErrConflict
		}
		rec, err := h.store.lookup(ctx, next.Carrier.InstanceID)
		if err != nil {
			return err
		}
		if rec.State != "running" || privateRegistration(rec.Snapshot.Owner) != *next || rec.UpgradeRecovery == nil || privateRegistration(rec.UpgradeRecovery.OldOwner) != old || rec.UpgradeRecovery.MaintenanceID != m.ID || rec.UpgradeRecovery.MaintenanceEpoch != m.Epoch {
			return executionowner.ErrConflict
		}
		plan, ok, err := h.journal.UpgradeRecoverySelection(ctx, rec.Surface, rec.SurfaceGeneration, rec.UpgradeRecoveryEffectID)
		if err != nil || !ok || plan != *rec.UpgradeRecovery {
			return executionowner.ErrConflict
		}
		actual, err := hostinventory.CaptureProcess(rec.Process.PID)
		if err != nil || actual != rec.Process {
			return executionowner.ErrConflict
		}
		return commit()
	})
}

// drainPrivateUpgradeCarriers uses the registry's immutable maintenance
// inventory, including carriers that do not currently have an online socket.
// A missing authenticated connection or uncertain native turn remains pending.
func (h *privateNativeHost) drainPrivateUpgradeCarriers(ctx context.Context, m executionowner.Maintenance) error {
	if h == nil || h.registry == nil {
		return executionowner.ErrConflict
	}
	current, err := h.registry.MaintenanceStatus(ctx, m.Backend)
	if err != nil {
		return err
	}
	if current.ID != m.ID || current.Epoch != m.Epoch || current.Phase != "draining" || current.TargetVersion != m.TargetVersion {
		return executionowner.ErrConflict
	}
	for after := ""; ; {
		page, err := h.registry.MaintenanceCarriers(ctx, m.Backend, m.ID, after, 128)
		if err != nil {
			return err
		}
		for _, reg := range page {
			if reg.Owner.Application == executionowner.Private {
				if err = h.drainPrivateUpgradeCarrier(ctx, m, reg); err != nil {
					return err
				}
			}
		}
		if len(page) < 128 {
			return nil
		}
		after = page[len(page)-1].Carrier.Incarnation
	}
}

// drainPrivateUpgradeCarrier advances the same persisted stop and inspection
// chain used by ordinary private shutdown. The backend maintenance admission
// fence must already be committed before this function is called. It waits for
// pre-fence sends without closing the channel needed for stop and inspection.
func (h *privateNativeHost) drainPrivateUpgradeCarrier(ctx context.Context, m executionowner.Maintenance, reg executionowner.Registration) error {
	if h == nil || h.store == nil || h.outbound == nil || h.registry == nil || reg.Owner.Application != executionowner.Private || reg.Carrier.Backend != m.Backend {
		return executionowner.ErrConflict
	}
	current, err := h.registry.MaintenanceStatus(ctx, m.Backend)
	if err != nil {
		return err
	}
	if current.ID != m.ID || current.Epoch != m.Epoch || current.Phase != "draining" || current.TargetVersion != m.TargetVersion {
		return executionowner.ErrConflict
	}
	found := false
	for after := ""; ; {
		page, err := h.registry.MaintenanceCarriers(ctx, m.Backend, m.ID, after, 128)
		if err != nil {
			return err
		}
		for _, frozen := range page {
			if frozen == reg {
				found = true
				break
			}
		}
		if found || len(page) < 128 {
			break
		}
		after = page[len(page)-1].Carrier.Incarnation
	}
	if !found {
		return executionowner.ErrConflict
	}
	rec, err := h.store.lookup(ctx, reg.Carrier.InstanceID)
	if err != nil {
		return err
	}
	if privateRegistration(rec.Snapshot.Owner) != reg {
		return executionowner.ErrConflict
	}
	if err = h.outbound.WaitIdle(ctx, reg); err != nil {
		return err
	}
	stopID := rec.StopEffectID
	if stopID == "" {
		stopID = fmt.Sprintf("maintenance:%s:%d:%s", m.ID, m.Epoch, reg.Carrier.Incarnation)
	}
	return h.advanceCarrierStop(ctx, rec, stopID)
}
