package workhost

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexmanaged"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"os"
)

type upgradeLaunchKey struct{}

func (h *Host) assertUpgradeLaunch(ctx context.Context, tx *workstore.Tx, backend work.Backend) error {
	if backend != work.BackendCodex {
		return nil
	}
	id, _ := ctx.Value(upgradeLaunchKey{}).(string)
	if id != "" {
		v, err := tx.GetNativeUpgrade(id)
		if err != nil {
			return err
		}
		if v.HostID != h.config.HostID || v.Stage != "restarting" || v.ManagedVersion != v.TargetVersion {
			return fmt.Errorf("upgrade launch is outside verified restart stage")
		}
		return nil
	}
	return tx.AssertNativeUpgradeAvailable(backend, h.config.HostID)
}
func (h *Host) PrepareAfterUpgrade(ctx context.Context, upgradeID, workerID string) error {
	var need workengine.WorkerNeed
	var blocked, ready bool
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		v, e := tx.GetNativeUpgrade(upgradeID)
		if e != nil {
			return e
		}
		if v.Stage != "restarting" || v.HostID != h.config.HostID {
			return workstore.ErrScopeDenied
		}
		var frozen *work.WorkerSession
		for i := range v.FrozenWorkers {
			if v.FrozenWorkers[i].ID == workerID {
				frozen = &v.FrozenWorkers[i]
				break
			}
		}
		if frozen == nil {
			return workstore.ErrScopeDenied
		}
		worker, e := tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		if worker.Generation > frozen.Generation {
			if worker.State == work.WorkerReady {
				ready = true
				return nil
			}
			return fmt.Errorf("upgraded carrier has not completed real Hello readiness")
		}
		if worker.Generation != frozen.Generation || worker.CarrierIncarnation != frozen.CarrierIncarnation {
			return workstore.ErrConflict
		}
		if gate, e := tx.GetRuntimeGate(worker.ID); e == nil && (gate.RecoveryReason != "" || gate.Reason != "snapshot_rebuild_required") {
			blocked = true
			return nil
		} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		wc, e := tx.GetContext(worker.ContextID)
		if e != nil {
			return e
		}
		need = workengine.WorkerNeed{ScopeID: worker.ScopeID, RoleID: worker.RoleID, ContextID: worker.ContextID, TaskID: wc.TaskID}
		return nil
	})
	if err != nil || blocked || ready {
		return err
	}
	ctx = context.WithValue(ctx, upgradeLaunchKey{}, upgradeID)
	if err = h.Prepare(ctx, need); err != nil {
		return err
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(workerID)
		if e != nil {
			return e
		}
		if worker.State != work.WorkerReady {
			return fmt.Errorf("upgraded carrier is awaiting real Hello")
		}
		return nil
	})
}

func (h *Host) freezeUpgradeBinary(ctx context.Context, settings *LaunchSettings) error {
	if settings.Backend != work.BackendCodex {
		return nil
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		v, e := tx.ActiveNativeUpgrade(h.config.HostID)
		if errors.Is(e, workstore.ErrNotFound) {
			v, e = tx.LatestCompletedNativeUpgrade(h.config.HostID)
		}
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if v.Stage != "completed" && v.Stage != "restarting" {
			return fmt.Errorf("Codex upgrade admission is closed")
		}

		if h.config.ResolveConfiguredCodexBinary != nil {
			source, err := h.config.ResolveConfiguredCodexBinary(ctx)
			if err != nil {
				return err
			}
			if source != v.Installation.ConfiguredBinary {
				if v.Stage != "completed" {
					return fmt.Errorf("configured Codex source changed during frozen upgrade")
				}
				binary, version, err := codexmanaged.ResolveVerifiedConfiguredBinary(ctx, source, os.Environ())
				if err != nil {
					return err
				}
				settings.NativeBinaryPath = binary
				settings.NativeBinaryVersion = version
				return nil
			}
		}
		settings.NativeBinaryPath = v.ManagedBinary
		settings.NativeBinaryVersion = v.ManagedVersion
		if settings.NativeBinaryPath == "" || settings.NativeBinaryVersion != v.TargetVersion {
			return fmt.Errorf("verified upgrade binary identity is missing")
		}
		return nil
	})
}
