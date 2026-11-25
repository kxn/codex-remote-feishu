package workupgrade

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexmanaged"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"path/filepath"
	"time"
)

// Unknown installation is observed, never resent. Version equality alone is
// insufficient: the precise installer scope must be empty, or the actual host
// must have rebooted since the durable start fence.
func (c *Coordinator) observeUnknown(ctx context.Context, v Record) error {
	if err := c.store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertNativeUpgradeLegacySettled(v.HostID) }); err != nil {
		return err
	}
	if v.ResumeStage == "verifying" || v.ResumeStage == "restarting" {
		return c.mutate(ctx, v.ID, func(r *Record) error { r.Stage = v.ResumeStage; r.ResumeStage = ""; return nil })
	}
	if v.ResumeStage != "installing_global" && v.ResumeStage != "installing_private" {
		return fmt.Errorf("unknown upgrade has no provable installation phase")
	}
	step := v.Global
	private := v.ResumeStage == "installing_private"
	if private {
		step = v.Private
	}
	evidence := ""
	if step.Scope != nil {
		scope, err := hostinventory.Restore(*step.Scope)
		if err == nil {
			defer scope.Close()
			wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			if step.Scope.Platform == "linux" {
				if v.HostBootIdentity == "" {
					err = fmt.Errorf("installer boot identity unavailable")
				} else {
					err = scope.ObserveRootExitWithCarrier(wait, hostinventory.ProcessIdentity{Platform: step.Scope.Platform, StartIdentity: v.HostBootIdentity + ":" + step.Scope.RootIdentity})
				}
			} else {
				err = scope.ObserveRootExit(wait)
			}
			cancel()
			if err == nil {
				snapshot := scope.Inspect()
				if snapshot.Complete && !snapshot.RootAlive && len(snapshot.ProcessIDs) == 0 {
					evidence = "exact installer root exit and empty persisted process scope"
				}
			}
		}
	}
	if evidence == "" {
		boot, err := hostinventory.BootIdentity()
		if err != nil {
			return err
		}
		if v.HostBootIdentity != "" && boot != v.HostBootIdentity {
			evidence = "actual host boot identity changed; prior installer processes cannot survive"
		} else {
			return fmt.Errorf("installer exit or descendants remain uncertain; no installation was repeated")
		}
	}
	if private {
		if _, err := codexmanaged.PinInstalledRelease(ctx, filepath.Join(c.config.StateDir, "codex-runtime"), c.privatePrefix(v.ID), v.TargetVersion); err != nil {
			return err
		}
	} else {
		actual := c.config.Inspect(ctx, codexupgrade.InspectOptions{ConfiguredBinary: v.Installation.ConfiguredBinary, NPMCommand: v.Installation.NPMCommand})
		if !actual.Upgradeable() || actual.PackageVersion != v.TargetVersion || actual.PackageRoot != v.Installation.PackageRoot {
			return fmt.Errorf("stopped installer did not leave verified target package; explicit repair required")
		}
	}
	return c.mutate(ctx, v.ID, func(r *Record) error {
		if private {
			r.Private.RecoveredVerified = true
			r.Private.RecoveryEvidence = evidence
		} else {
			r.Global.RecoveredVerified = true
			r.Global.RecoveryEvidence = evidence
		}
		r.Stage = v.ResumeStage
		r.ResumeStage = ""
		r.LastError = ""
		return nil
	})
}
