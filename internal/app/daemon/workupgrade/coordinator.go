package workupgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"path/filepath"
	"sync"
	"time"
)

type Record = workstore.NativeUpgrade
type Request struct {
	ID, TargetVersion, InstallationFingerprint string
	Source                                     workstore.PrivateActionSource
}
type Config struct {
	HostID, StateDir, ConfiguredBinary string
	ResolveConfiguredBinary            func(context.Context) (string, error)
	Drain                              func(context.Context, work.WorkerSession) error
	Restart                            func(context.Context, string, string) error
	Now                                func() time.Time
	Inspect                            func(context.Context, codexupgrade.InspectOptions) codexupgrade.Installation
	Install                            func(context.Context, string, codexupgrade.TrackedInstallOptions) (codexupgrade.TrackedInstallResult, error)
	// Maintenance coordinates the backend-wide private/team fence. It must be
	// supplied by the assembled daemon when both applications share a backend.
	Maintenance MaintenanceLifecycle
}

// MaintenanceLifecycle methods are replayed after crashes and must use the
// exact persisted upgrade identity. None may infer process exit from a socket.
type MaintenanceLifecycle interface {
	BeginAndDrainPrivate(context.Context, Record) error
	BeforeInstall(context.Context, Record) error
	AfterVerified(context.Context, Record) error
	AfterTeamRestart(context.Context, Record) error
}
type Coordinator struct {
	store  *workstore.Store
	config Config
	mu     sync.Mutex
}

// SetMaintenance is called only during daemon assembly, before Run starts.
func (c *Coordinator) SetMaintenance(maintenance MaintenanceLifecycle) {
	c.config.Maintenance = maintenance
}

func New(store *workstore.Store, cfg Config) (*Coordinator, error) {
	if store == nil || cfg.HostID == "" || !filepath.IsAbs(cfg.StateDir) || (cfg.ConfiguredBinary == "" && cfg.ResolveConfiguredBinary == nil) || cfg.Drain == nil || cfg.Restart == nil {
		return nil, fmt.Errorf("native upgrade requires exact host, installation, durable store and carrier lifecycle")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Inspect == nil {
		cfg.Inspect = codexupgrade.Inspect
	}
	if cfg.Install == nil {
		cfg.Install = codexupgrade.InstallTracked
	}
	return &Coordinator{store: store, config: cfg}, nil
}
func InstallationFingerprint(v codexupgrade.Installation) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c *Coordinator) Request(ctx context.Context, req Request) (Record, error) {
	var record Record
	if req.ID == "" || !validTarget(req.TargetVersion) {
		return record, fmt.Errorf("upgrade requires exact version and request identity")
	}
	var prior Record
	var principalID string
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, _, e := tx.ValidatePrivateAction(req.Source)
		if e != nil {
			return e
		}
		principalID = scope.PrincipalID
		old, e := tx.GetNativeUpgrade(req.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		if old.HostID != c.config.HostID || old.TargetVersion != req.TargetVersion || old.InstallationFingerprint != req.InstallationFingerprint || old.PrincipalID != scope.PrincipalID || old.Source.PersonalScopeID != req.Source.PersonalScopeID {
			return workstore.ErrIdempotencyConflict
		}
		prior = old
		return nil
	})
	if err != nil {
		return record, err
	}
	if prior.ID != "" {
		return prior, nil
	}
	boot, bootErr := hostinventory.BootIdentity()
	if bootErr != nil {
		return record, bootErr
	}
	configured := c.config.ConfiguredBinary
	if c.config.ResolveConfiguredBinary != nil {
		configured, err = c.config.ResolveConfiguredBinary(ctx)
		if err != nil {
			return record, err
		}
	}
	installation := c.config.Inspect(ctx, codexupgrade.InspectOptions{ConfiguredBinary: configured})
	if !installation.Upgradeable() {
		return record, fmt.Errorf("installation is not an upgradeable standalone npm CLI: %s", installation.SourceKind)
	}
	if req.InstallationFingerprint != InstallationFingerprint(installation) {
		return record, fmt.Errorf("installation changed since upgrade confirmation")
	}
	err = c.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, _, err := tx.ValidatePrivateAction(req.Source); err != nil {
			return err
		}
		old, err := tx.GetNativeUpgrade(req.ID)
		if err == nil {
			if old.HostID != c.config.HostID || old.TargetVersion != req.TargetVersion || old.InstallationFingerprint != req.InstallationFingerprint || old.PrincipalID != principalID || old.Source.PersonalScopeID != req.Source.PersonalScopeID {
				return workstore.ErrIdempotencyConflict
			}
			record = old
			return nil
		}
		if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if err := tx.AssertNativeUpgradeLegacySettled(c.config.HostID); err != nil {
			return err
		}
		workers, err := tx.NativeUpgradeWorkers(c.config.HostID)
		if err != nil {
			return err
		}
		record = Record{HostBootIdentity: boot, ID: req.ID, HostID: c.config.HostID, TargetVersion: req.TargetVersion, InstallationFingerprint: req.InstallationFingerprint, Source: req.Source, PrincipalID: principalID, Installation: installation, Stage: "draining", FrozenWorkers: workers, Revision: 1, CreatedAt: c.config.Now(), UpdatedAt: c.config.Now()}
		return tx.PutNativeUpgrade(record, 0)
	})
	return record, err
}
func (c *Coordinator) Status(ctx context.Context, id string) (Record, error) {
	var v Record
	err := c.store.Within(ctx, func(tx *workstore.Tx) error { var e error; v, e = tx.GetNativeUpgrade(id); return e })
	return v, err
}
func validTarget(v string) bool { return codexupgrade.ValidTargetVersion(v) }

func (c *Coordinator) mutate(ctx context.Context, id string, fn func(*Record) error) error {
	return c.store.Within(ctx, func(tx *workstore.Tx) error {
		v, err := tx.GetNativeUpgrade(id)
		if err != nil {
			return err
		}
		before := v.Revision
		if err = fn(&v); err != nil {
			return err
		}
		v.Revision++
		v.UpdatedAt = c.config.Now()
		return tx.PutNativeUpgrade(v, before)
	})
}
