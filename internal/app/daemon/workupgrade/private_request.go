package workupgrade

import (
	"context"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
)

type PrivateRequest struct {
	ID, TargetVersion, InstallationFingerprint string
	Source                                     workstore.NativeUpgradePrivateSource
}

// PrivateRequestAuthority must bind the request to the exact durable private
// effect and owner while the callback writes the shared install transaction.
type PrivateRequestAuthority interface {
	WithPrivateUpgradeRequest(context.Context, PrivateRequest, func() error) error
}

func (c *Coordinator) RequestPrivate(ctx context.Context, req PrivateRequest, authority PrivateRequestAuthority) (Record, error) {
	var result Record
	if authority == nil || req.ID == "" || !validTarget(req.TargetVersion) || req.Source.Tenant == "" || req.Source.AppID == "" || req.Source.Gateway == "" || req.Source.Surface == "" || req.Source.Chat == "" || req.Source.User == "" || req.Source.Generation <= 0 || req.Source.EffectID == "" {
		return result, fmt.Errorf("private upgrade request lacks durable owner")
	}
	err := authority.WithPrivateUpgradeRequest(ctx, req, func() error {
		return c.store.Within(ctx, func(tx *workstore.Tx) error {
			old, e := tx.GetNativeUpgrade(req.ID)
			if errors.Is(e, workstore.ErrNotFound) {
				return nil
			}
			if e != nil {
				return e
			}
			if old.HostID != c.config.HostID || old.TargetVersion != req.TargetVersion || old.InstallationFingerprint != req.InstallationFingerprint || old.PrivateSource == nil || *old.PrivateSource != req.Source {
				return workstore.ErrIdempotencyConflict
			}
			result = old
			return nil
		})
	})
	if err != nil || result.ID != "" {
		return result, err
	}
	boot, err := hostinventory.BootIdentity()
	if err != nil {
		return result, err
	}
	configured := c.config.ConfiguredBinary
	if c.config.ResolveConfiguredBinary != nil {
		configured, err = c.config.ResolveConfiguredBinary(ctx)
		if err != nil {
			return result, err
		}
	}
	installation := c.config.Inspect(ctx, codexupgrade.InspectOptions{ConfiguredBinary: configured})
	if !installation.Upgradeable() || req.InstallationFingerprint != InstallationFingerprint(installation) {
		return result, fmt.Errorf("installation changed since private confirmation")
	}
	err = authority.WithPrivateUpgradeRequest(ctx, req, func() error {
		return c.store.Within(ctx, func(tx *workstore.Tx) error {
			old, e := tx.GetNativeUpgrade(req.ID)
			if e == nil {
				if old.HostID != c.config.HostID || old.TargetVersion != req.TargetVersion || old.InstallationFingerprint != req.InstallationFingerprint || old.PrivateSource == nil || *old.PrivateSource != req.Source {
					return workstore.ErrIdempotencyConflict
				}
				result = old
				return nil
			}
			if !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if e = tx.AssertNativeUpgradeLegacySettled(c.config.HostID); e != nil {
				return e
			}
			workers, e := tx.NativeUpgradeWorkers(c.config.HostID)
			if e != nil {
				return e
			}
			source := req.Source
			result = Record{HostBootIdentity: boot, ID: req.ID, HostID: c.config.HostID, TargetVersion: req.TargetVersion, InstallationFingerprint: req.InstallationFingerprint, PrivateSource: &source, Installation: installation, Stage: "draining", FrozenWorkers: workers, Revision: 1, CreatedAt: c.config.Now(), UpdatedAt: c.config.Now()}
			return tx.PutNativeUpgrade(result, 0)
		})
	})
	return result, err
}
