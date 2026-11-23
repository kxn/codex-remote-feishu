package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workupgrade"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// sharedCodexMaintenance reuses the team install transaction while fencing the
// private and team native carriers under the same backend epoch.
type sharedCodexMaintenance struct {
	app        *App
	registry   *executionowner.Registry
	private    *privateNativeHost
	store      *workstore.Store
	team       *workupgrade.Coordinator
	teamOwners *teamExecutionOwnerAdapter
}

func (s *sharedCodexMaintenance) current(ctx context.Context, record workupgrade.Record) (executionowner.Maintenance, error) {
	if s == nil || s.registry == nil || s.private == nil || s.store == nil || s.team == nil {
		return executionowner.Maintenance{}, executionowner.ErrConflict
	}
	m, err := s.registry.BeginMaintenance(ctx, "codex", record.ID, record.TargetVersion)
	if err != nil {
		return m, err
	}
	return m, nil
}

func (s *sharedCodexMaintenance) BeginAndDrainPrivate(ctx context.Context, record workupgrade.Record) error {
	m, err := s.current(ctx, record)
	if err != nil {
		return err
	}
	if m.Phase == "installing" {
		return s.registry.RecoverMaintenanceInstallUnsent(ctx, m, "team-unsent:"+record.ID, teamInstallUnsent{s})
	}
	if m.Phase == "quiescent" {
		return nil
	}
	if m.Phase != "draining" {
		return fmt.Errorf("maintenance %s in %s requires recovery before installation", m.ID, m.Phase)
	}
	if err = s.pausePrivateUpgradeCarriers(ctx, m); err != nil {
		return err
	}
	return s.private.drainPrivateUpgradeCarriers(ctx, m)
}

func (s *sharedCodexMaintenance) pausePrivateUpgradeCarriers(ctx context.Context, m executionowner.Maintenance) error {
	status, err := s.private.processor.ReadRuntimeStatus(ctx)
	if err != nil {
		return err
	}
	for after := ""; ; {
		page, err := s.registry.MaintenanceCarriers(ctx, m.Backend, m.ID, after, 128)
		if err != nil {
			return err
		}
		for _, item := range page {
			if item.Owner.Application != executionowner.Private {
				continue
			}
			old, exists, err := s.private.journal.UpgradePauseForCarrier(ctx, m.ID, item.Carrier.Incarnation)
			if err != nil {
				return err
			}
			if exists {
				if err = s.private.processor.PauseUpgradeDispatch(ctx, old, s.private); err != nil {
					return err
				}
				continue
			}
			rec, err := s.private.store.lookup(ctx, item.Carrier.InstanceID)
			if err != nil {
				return err
			}
			if privateRegistration(rec.Snapshot.Owner) != item {
				return executionowner.ErrConflict
			}
			var thread string
			var previous state.DispatchMode
			found := false
			for _, surface := range status.Surfaces {
				if surface != nil && surface.SurfaceSessionID == rec.Surface.SurfaceID && surface.AttachedInstanceID == item.Carrier.InstanceID {
					thread, previous, found = surface.SelectedThreadID, surface.DispatchMode, true
					break
				}
			}
			if !found {
				return privatechat.ErrDeferred
			}
			if previous == "" {
				previous = state.DispatchModeNormal
			}
			plan := privatechat.UpgradeRecoveryPlan{MaintenanceID: m.ID, MaintenanceEpoch: m.Epoch, Owner: rec.Surface, Generation: rec.SurfaceGeneration, OldOwner: rec.Snapshot.Owner, ThreadID: thread, CWD: rec.Snapshot.CWD, ProfileID: state.NormalizeCodexProfileID(rec.CodexProfileID), PreviousMode: previous}
			if err = s.private.processor.PauseUpgradeDispatch(ctx, plan, s.private); err != nil {
				return err
			}
		}
		if len(page) < 128 {
			return nil
		}
		after = page[len(page)-1].Carrier.Incarnation
	}
}

type teamMaintenanceSources struct {
	shared *sharedCodexMaintenance
	record workupgrade.Record
}

func (v teamMaintenanceSources) VerifyMaintenanceSources(ctx context.Context, m executionowner.Maintenance, evidence string) error {
	if m.ID != v.record.ID || m.TargetVersion != v.record.TargetVersion || evidence != "team-upgrade:"+v.record.ID {
		return executionowner.ErrConflict
	}
	if err := v.shared.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, err := tx.GetNativeUpgrade(v.record.ID)
		if err != nil {
			return err
		}
		if actual.Revision != v.record.Revision || actual.Stage != "draining" || actual.HostID != v.record.HostID || actual.TargetVersion != v.record.TargetVersion {
			return workstore.ErrConflict
		}
		return tx.AssertNativeUpgradeLegacySettled(actual.HostID)
	}); err != nil {
		return err
	}
	return v.shared.team.VerifyFrozenPoolExited(ctx, v.record)
}

func (s *sharedCodexMaintenance) BeforeInstall(ctx context.Context, record workupgrade.Record) error {
	m, err := s.current(ctx, record)
	if err != nil {
		return err
	}
	if m.Phase == "installing" {
		if err = s.registry.RecoverMaintenanceInstallUnsent(ctx, m, "team-unsent:"+record.ID, teamInstallUnsent{s}); err != nil {
			return err
		}
		m.Phase = "quiescent"
	}
	if m.Phase == "draining" {
		if err = s.registry.MarkMaintenanceQuiescent(ctx, m, "team-upgrade:"+record.ID, teamMaintenanceSources{s, record}); err != nil {
			return err
		}
		m.Phase = "quiescent"
	}
	if m.Phase != "quiescent" {
		return fmt.Errorf("maintenance %s is %s; inspect prior installation before proceeding", m.ID, m.Phase)
	}
	return s.registry.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch)
}

type teamInstallUnsent struct{ shared *sharedCodexMaintenance }

func (v teamInstallUnsent) WithMaintenanceInstallUnsent(ctx context.Context, m executionowner.Maintenance, evidence string, commit func() error) error {
	if evidence != "team-unsent:"+m.ID {
		return executionowner.ErrConflict
	}
	return v.shared.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, err := tx.GetNativeUpgrade(m.ID)
		if err != nil {
			return err
		}
		if actual.Stage != "draining" || actual.TargetVersion != m.TargetVersion || actual.Global.Started || actual.Private.Started || actual.Global.Result != nil || actual.Private.Result != nil {
			return executionowner.ErrConflict
		}
		return commit()
	})
}

type verifiedTeamInstall struct{ shared *sharedCodexMaintenance }

func (v verifiedTeamInstall) VerifyMaintenanceInstall(ctx context.Context, m executionowner.Maintenance, evidence string) error {
	if evidence != "team-install:"+m.ID {
		return executionowner.ErrConflict
	}
	return v.shared.team.VerifyInstalledVersion(ctx, m.ID, m.TargetVersion)
}

func (s *sharedCodexMaintenance) AfterVerified(ctx context.Context, record workupgrade.Record) error {
	m, err := s.current(ctx, record)
	if err != nil {
		return err
	}
	if err = s.registry.MarkMaintenanceInstalled(ctx, m, "team-install:"+record.ID, verifiedTeamInstall{s}); err != nil {
		return err
	}
	return s.preparePrivateRecoveries(ctx, m)
}

func (s *sharedCodexMaintenance) preparePrivateRecoveries(ctx context.Context, m executionowner.Maintenance) error {
	for after := ""; ; {
		page, e := s.registry.MaintenanceRecoveries(ctx, m, after, 128)
		if e != nil {
			return e
		}
		for _, item := range page {
			if item.Old.Owner.Application != executionowner.Private {
				continue
			}
			rec, e := s.private.store.lookup(ctx, item.Old.Carrier.InstanceID)
			if e != nil {
				return e
			}
			if privateRegistration(rec.Snapshot.Owner) != item.Old {
				return executionowner.ErrConflict
			}
			stored, exists, e := s.private.journal.UpgradeRecoveryForCarrier(ctx, m.ID, item.Old.Carrier.Incarnation)
			if e != nil {
				return e
			}
			if exists {
				if _, e = s.private.processor.PrepareUpgradeRecovery(ctx, stored, s.private); e != nil {
					return e
				}
				continue
			}
			plan, paused, e := s.private.journal.UpgradePauseForCarrier(ctx, m.ID, item.Old.Carrier.Incarnation)
			if e != nil {
				return e
			}
			if !paused {
				return executionowner.ErrConflict
			}
			if s.app == nil || s.app.applications == nil || s.app.applications.ingress == nil {
				return privatechat.ErrDeferred
			}
			if e = s.app.applications.ingress.refreshPrivateThreadPolicy(ctx, s.private, s.private.processor, plan.Owner, plan.Generation); e != nil {
				return e
			}
			if _, e = s.private.processor.PrepareUpgradeRecovery(ctx, plan, s.private); e != nil {
				return e
			}
		}
		if len(page) < 128 {
			return nil
		}
		after = page[len(page)-1].Old.Carrier.Incarnation
	}
}

func (s *sharedCodexMaintenance) AfterTeamRestart(ctx context.Context, record workupgrade.Record) error {
	if s.teamOwners == nil {
		return executionowner.ErrConflict
	}
	m, err := s.current(ctx, record)
	if err != nil {
		return err
	}
	for after := ""; ; {
		page, err := s.registry.MaintenanceRecoveries(ctx, m, after, 128)
		if err != nil {
			return err
		}
		for _, item := range page {
			if item.Disposition != "" {
				continue
			}
			if item.Replacement == nil {
				return privatechat.ErrDeferred
			}
			var verifier executionowner.MaintenanceRecoveryVerifier
			prefix := "team-ready:"
			switch item.Old.Owner.Application {
			case executionowner.Team:
				verifier = s.teamOwners
			case executionowner.Private:
				verifier, prefix = s.private, "private-ready:"
			default:
				return executionowner.ErrConflict
			}
			evidence := prefix + m.ID + ":" + item.Replacement.Carrier.Incarnation
			if err = s.registry.MarkMaintenanceRecovered(ctx, m, item.Old, item.Replacement, evidence, verifier); err != nil {
				return err
			}
		}
		if len(page) < 128 {
			break
		}
		after = page[len(page)-1].Old.Carrier.Incarnation
	}
	if err = s.registry.ReleaseMaintenance(ctx, m, "team-release:"+m.ID, sharedRelease{s}); err != nil {
		return err
	}
	for after := ""; ; {
		page, err := s.registry.MaintenanceRecoveries(ctx, m, after, 128)
		if err != nil {
			return err
		}
		for _, item := range page {
			if item.Old.Owner.Application != executionowner.Private {
				continue
			}
			if item.Replacement == nil || item.Disposition != "rebuilt" {
				return privatechat.ErrDeferred
			}
			plan, exists, err := s.private.journal.UpgradeRecoveryForCarrier(ctx, m.ID, item.Old.Carrier.Incarnation)
			if err != nil {
				return err
			}
			if !exists {
				return executionowner.ErrConflict
			}
			if _, err = s.private.processor.ResumeUpgradeRecovery(ctx, plan, item.Replacement.Carrier.InstanceID, s.private); err != nil {
				return err
			}
		}
		if len(page) < 128 {
			return nil
		}
		after = page[len(page)-1].Old.Carrier.Incarnation
	}
}

type sharedRelease struct{ shared *sharedCodexMaintenance }

func (v sharedRelease) WithMaintenanceRelease(ctx context.Context, m executionowner.Maintenance, evidence string, commit func() error) error {
	if evidence != "team-release:"+m.ID {
		return executionowner.ErrConflict
	}
	return v.shared.store.Within(ctx, func(tx *workstore.Tx) error {
		actual, err := tx.GetNativeUpgrade(m.ID)
		if err != nil {
			return err
		}
		if actual.Stage != "restarting" || actual.TargetVersion != m.TargetVersion || actual.ManagedVersion != m.TargetVersion {
			return workstore.ErrConflict
		}
		if err = tx.AssertNativeUpgradeLegacySettled(actual.HostID); err != nil {
			return err
		}
		return commit()
	})
}
