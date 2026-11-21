package workupgrade

import (
	"context"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexmanaged"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"time"
)

func (c *Coordinator) Run(ctx context.Context, limit int) error {
	if !c.mu.TryLock() {
		return nil
	}
	defer c.mu.Unlock()
	var v Record
	err := c.store.Within(ctx, func(tx *workstore.Tx) error { var e error; v, e = tx.ActiveNativeUpgrade(c.config.HostID); return e })
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if v.Stage == "unknown" {
		return c.observeUnknown(ctx, v)
	}
	if err = c.store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertNativeUpgradeLegacySettled(v.HostID) }); err != nil {
		return err
	}
	err = c.advance(ctx, v)
	if err != nil {
		_ = c.mutate(context.WithoutCancel(ctx), v.ID, func(current *Record) error { current.LastError = err.Error(); return nil })
	}
	return err
}
func (c *Coordinator) advance(ctx context.Context, v Record) error {
	if v.Stage == "draining" {
		if err := c.assertFrozenPool(ctx, v); err != nil {
			return err
		}
		for _, worker := range v.FrozenWorkers {
			unsent, err := c.unsent(ctx, worker)
			if err != nil {
				return err
			}
			if unsent {
				continue
			}
			if err := c.config.Drain(ctx, worker); err != nil {
				return err
			}
			if err := c.verifyExited(ctx, worker.CarrierIncarnation); err != nil {
				return err
			}
		}
		if err := c.mutate(ctx, v.ID, func(r *Record) error {
			if r.Stage != "draining" {
				return workstore.ErrConflict
			}
			r.Stage = "installing_global"
			r.LastError = ""
			return nil
		}); err != nil {
			return err
		}
		v.Stage = "installing_global"
	}
	if v.Stage == "installing_global" {
		if err := c.install(ctx, v.ID, false); err != nil {
			return err
		}
		if err := c.mutate(ctx, v.ID, func(r *Record) error {
			if r.Stage != "installing_global" {
				return workstore.ErrConflict
			}
			r.Stage = "installing_private"
			return nil
		}); err != nil {
			return err
		}
		v.Stage = "installing_private"
	}
	if v.Stage == "installing_private" {
		if err := c.install(ctx, v.ID, true); err != nil {
			return err
		}
		if err := c.mutate(ctx, v.ID, func(r *Record) error {
			if r.Stage != "installing_private" {
				return workstore.ErrConflict
			}
			r.Stage = "verifying"
			return nil
		}); err != nil {
			return err
		}
		v.Stage = "verifying"
	}
	if v.Stage == "verifying" {
		actual := c.config.Inspect(ctx, codexupgrade.InspectOptions{ConfiguredBinary: v.Installation.ConfiguredBinary, NPMCommand: v.Installation.NPMCommand})
		if !actual.Upgradeable() || actual.PackageVersion != v.TargetVersion || actual.PackageRoot != v.Installation.PackageRoot {
			return fmt.Errorf("global Codex installation does not match exact requested target and package path")
		}
		binary, err := codexmanaged.PinInstalledRelease(ctx, filepath.Join(c.config.StateDir, "codex-runtime"), c.privatePrefix(v.ID), v.TargetVersion)
		if err != nil {
			return err
		}
		if err = c.mutate(ctx, v.ID, func(r *Record) error {
			if r.Stage != "verifying" {
				return workstore.ErrConflict
			}
			r.ManagedBinary = binary
			r.ManagedVersion = r.TargetVersion
			r.Stage = "restarting"
			return nil
		}); err != nil {
			return err
		}
		v.Stage = "restarting"
	}
	if v.Stage == "restarting" {
		for _, worker := range v.FrozenWorkers {
			if err := c.config.Restart(ctx, v.ID, worker.ID); err != nil {
				return err
			}
		}
		return c.mutate(ctx, v.ID, func(r *Record) error {
			if r.Stage != "restarting" {
				return workstore.ErrConflict
			}
			r.Stage = "completed"
			r.LastError = ""
			return nil
		})
	}
	return nil
}
func (c *Coordinator) privatePrefix(id string) string {
	return filepath.Join(c.config.StateDir, "codex-runtime", "upgrades", safeID(id))
}
func safeID(id string) string { sum := InstallationFingerprint(structInstallation(id)); return sum }

// Hash the identifier rather than interpolating human/UI input into host paths.
func structInstallation(id string) codexupgrade.Installation {
	return codexupgrade.Installation{ConfiguredBinary: id}
}
func (c *Coordinator) install(ctx context.Context, id string, private bool) error {
	v, err := c.Status(ctx, id)
	if err != nil {
		return err
	}
	step := v.Global
	if private {
		step = v.Private
	}
	if step.RecoveredVerified {
		return nil
	}
	if step.Result != nil {
		if step.Result.ExitCode == 0 && step.Result.Scope.Complete && !step.Result.Scope.RootAlive && len(step.Result.Scope.ProcessIDs) == 0 {
			return nil
		}
		return c.unknown(ctx, id, "installer exited unsuccessfully or descendants remain uncertain")
	}
	if step.Started {
		return c.unknown(ctx, id, "installer started without a durable verified exit; do not repeat installation")
	}

	if err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.AssertNativeUpgradeLegacySettled(v.HostID); e != nil {
			return e
		}
		current, e := tx.GetNativeUpgrade(id)
		if e != nil {
			return e
		}
		expectedStage := "installing_global"
		if private {
			expectedStage = "installing_private"
		}
		if current.Stage != expectedStage {
			return workstore.ErrConflict
		}
		step := current.Global
		if private {
			step = current.Private
		}
		if step.Started {
			return workstore.ErrConflict
		}
		before := current.Revision
		current.Revision++
		current.UpdatedAt = c.config.Now()
		if private {
			current.Private.Started = true
		} else {
			current.Global.Started = true
		}
		return tx.PutNativeUpgrade(current, before)
	}); err != nil {
		return err
	}

	opts := codexupgrade.TrackedInstallOptions{NPMCommand: v.Installation.NPMCommand}
	if private {
		opts.Prefix = c.privatePrefix(id)
	}
	opts.OnStarted = func(scope hostinventory.Snapshot) error {
		return c.mutate(context.WithoutCancel(ctx), id, func(r *Record) error {
			if private {
				r.Private.Scope = &scope
			} else {
				r.Global.Scope = &scope
			}
			return nil
		})
	}
	opts.OnExited = func(result codexupgrade.TrackedInstallResult) error {
		return c.mutate(context.WithoutCancel(ctx), id, func(r *Record) error {
			if private {
				r.Private.Result = &result
			} else {
				r.Global.Result = &result
			}
			return nil
		})
	}
	_, err = c.config.Install(ctx, v.TargetVersion, opts)
	if err != nil {
		return c.unknown(context.WithoutCancel(ctx), id, "tracked installer failed or delivery is uncertain: "+err.Error())
	}
	return nil
}
func (c *Coordinator) unknown(ctx context.Context, id, reason string) error {
	err := c.mutate(ctx, id, func(r *Record) error {
		r.ResumeStage = r.Stage
		r.Stage = "unknown"
		r.LastError = reason
		return nil
	})
	if err != nil {
		return err
	}
	return fmt.Errorf("%s", reason)
}
func (c *Coordinator) verifyExited(ctx context.Context, incarnation string) error {
	var identity hostinventory.ProcessIdentity
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, e := tx.GetCarrier(incarnation)
		if e != nil {
			return e
		}
		occupied, e := tx.HasAnyCarrierOccupancy(carrier)
		if e != nil {
			return e
		}
		if occupied {
			return fmt.Errorf("upgrade cannot install while carrier retains occupied execution")
		}
		process, e := tx.GetCarrierProcess(incarnation)
		if e != nil {
			return e
		}
		if process.HostID != c.config.HostID {
			return fmt.Errorf("upgrade process exit belongs to a different host")
		}
		identity = hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity}
		return nil
	})
	if err != nil {
		return err
	}
	wait, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return hostinventory.WaitProcessExit(wait, identity)
}

func (c *Coordinator) unsent(ctx context.Context, worker work.WorkerSession) (bool, error) {
	var unsent bool
	err := c.store.Within(ctx, func(tx *workstore.Tx) error { var e error; unsent, e = tx.WorkerLaunchUnsent(worker); return e })
	return unsent, err
}
func (c *Coordinator) assertFrozenPool(ctx context.Context, v Record) error {
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		workers, e := tx.NativeUpgradeWorkers(v.HostID)
		if e != nil {
			return e
		}
		if len(workers) != len(v.FrozenWorkers) {
			return fmt.Errorf("Codex pool changed during upgrade fence")
		}
		for i, w := range workers {
			old := v.FrozenWorkers[i]
			if w.ID != old.ID || w.Generation != old.Generation || w.CarrierIncarnation != old.CarrierIncarnation {
				return fmt.Errorf("Codex pool identity changed during upgrade fence")
			}
		}
		return nil
	})
}
