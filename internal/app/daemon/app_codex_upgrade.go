package daemon

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	codexupgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/codexupgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type codexUpgradeBusyReason struct {
	InstanceID string
	SurfaceID  string
	Reason     string
}

type codexUpgradeCheckResult struct {
	Installation   codexupgrade.Installation
	CurrentVersion string
	LatestVersion  string
	BusyReasons    []codexUpgradeBusyReason
	HasUpdate      bool
	CanUpgrade     bool
}

type codexUpgradeStartRequest struct {
	SurfaceSessionID string
	ActorUserID      string
	TargetVersion    string
	OnComplete       func(error)
}

func (a *App) inspectStandaloneCodexInstallation(ctx context.Context) (codexupgrade.Installation, error) {
	loaded, err := a.loadAdminConfig()
	if err != nil {
		return codexupgrade.Installation{}, err
	}
	configured, _ := resolvedCodexRealBinarySetting(loaded)
	inspect := a.codexUpgradeRuntime.Inspect
	if inspect == nil {
		inspect = func(ctx context.Context, opts codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
			return codexupgrade.Inspect(ctx, opts), nil
		}
	}
	return inspect(ctx, codexupgrade.InspectOptions{
		ConfiguredBinary: configured,
	})
}

func (a *App) checkStandaloneCodexUpgrade(ctx context.Context, initiatorSurfaceID string) (codexUpgradeCheckResult, error) {
	installation, err := a.inspectStandaloneCodexInstallation(ctx)
	if err != nil {
		return codexUpgradeCheckResult{}, err
	}
	result := codexUpgradeCheckResult{
		Installation:   installation,
		CurrentVersion: installation.CurrentVersion(),
	}
	if installation.Upgradeable() {
		lookup := a.codexUpgradeRuntime.LatestLookup
		if lookup == nil {
			lookup = func(ctx context.Context) (string, error) {
				return codexupgrade.LookupLatestVersion(ctx, codexupgrade.LatestVersionOptions{})
			}
		}
		latest, err := lookup(ctx)
		if err != nil {
			return result, err
		}
		result.LatestVersion = latest
		result.HasUpdate = strings.TrimSpace(latest) != "" && strings.TrimSpace(latest) != strings.TrimSpace(result.CurrentVersion)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.codexUpgradeRuntime.Active != nil {
		result.BusyReasons = []codexUpgradeBusyReason{{
			SurfaceID: a.codexUpgradeRuntime.Active.InitiatorSurface,
			Reason:    "upgrade_running",
		}}
		return result, nil
	}
	result.BusyReasons = a.codexUpgradeBusyReasonsLocked(strings.TrimSpace(initiatorSurfaceID))
	result.CanUpgrade = installation.Upgradeable() && result.HasUpdate && len(result.BusyReasons) == 0
	return result, nil
}

func (a *App) startStandaloneCodexUpgrade(ctx context.Context, req codexUpgradeStartRequest) error {
	check, err := a.checkStandaloneCodexUpgrade(ctx, req.SurfaceSessionID)
	if err != nil {
		return err
	}
	if !check.Installation.Upgradeable() {
		if check.Installation.BundleBacked() {
			return fmt.Errorf("current codex binary is backed by a vscode bundle")
		}
		if strings.TrimSpace(check.Installation.Problem) != "" {
			return fmt.Errorf("current codex binary is not upgradeable: %s", check.Installation.Problem)
		}
		return fmt.Errorf("current codex binary is not upgradeable")
	}
	targetVersion := firstNonEmpty(strings.TrimSpace(req.TargetVersion), strings.TrimSpace(check.LatestVersion))
	if targetVersion == "" {
		return fmt.Errorf("missing target version for codex upgrade")
	}
	if !check.HasUpdate || targetVersion == check.CurrentVersion {
		return fmt.Errorf("current codex is already on %s", firstNonEmpty(check.CurrentVersion, "the latest version"))
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.codexUpgradeRuntime.Active != nil {
		return fmt.Errorf("another codex upgrade transaction is already running")
	}
	busyReasons := a.codexUpgradeBusyReasonsLocked(strings.TrimSpace(req.SurfaceSessionID))
	if len(busyReasons) != 0 {
		return fmt.Errorf("other instances are still busy: %s", formatCodexUpgradeBusyReasons(busyReasons))
	}

	tx := &codexupgraderuntime.Transaction{
		ID:                 a.nextCodexUpgradeIDLocked(),
		Install:            check.Installation,
		CurrentVersion:     check.CurrentVersion,
		TargetVersion:      targetVersion,
		InitiatorSurface:   strings.TrimSpace(req.SurfaceSessionID),
		InitiatorUserID:    strings.TrimSpace(req.ActorUserID),
		RestartInstanceIDs: a.affectedOnlineInstanceIDsLocked(),
		PausedSurfaceIDs:   map[string]bool{},
		StartedAt:          time.Now().UTC(),
	}
	a.pauseCodexUpgradeSurfacesLocked(tx)
	a.codexUpgradeRuntime.Active = tx

	go a.runStandaloneCodexUpgrade(tx, req.OnComplete)
	return nil
}

func (a *App) runStandaloneCodexUpgrade(tx *codexupgraderuntime.Transaction, onComplete func(error)) {
	ctx, cancel := context.WithTimeout(context.Background(), codexUpgradeInstallTimeout)
	defer cancel()

	var runErr error
	installFn := a.codexUpgradeRuntime.Install
	if installFn == nil {
		installFn = func(ctx context.Context, installation codexupgrade.Installation, version string) error {
			return codexupgrade.InstallGlobal(ctx, version, codexupgrade.InstallOptions{
				NPMCommand: installation.NPMCommand,
			})
		}
	}
	if err := installFn(ctx, tx.Install, tx.TargetVersion); err != nil {
		runErr = err
	} else {
		inspect := a.codexUpgradeRuntime.Inspect
		if inspect == nil {
			inspect = func(ctx context.Context, opts codexupgrade.InspectOptions) (codexupgrade.Installation, error) {
				return codexupgrade.Inspect(ctx, opts), nil
			}
		}
		verified, err := inspect(ctx, codexupgrade.InspectOptions{
			ConfiguredBinary: tx.Install.ConfiguredBinary,
			NPMCommand:       tx.Install.NPMCommand,
		})
		if err != nil {
			runErr = fmt.Errorf("verify codex installation after upgrade: %w", err)
		} else if !verified.Upgradeable() {
			runErr = fmt.Errorf("codex upgrade finished but runtime binary is no longer upgradeable")
		} else if verified.CurrentVersion() != tx.TargetVersion {
			runErr = fmt.Errorf("codex upgrade finished with version %s, want %s", firstNonEmpty(verified.CurrentVersion(), "unknown"), tx.TargetVersion)
		}
	}
	if runErr == nil {
		for _, instanceID := range tx.RestartInstanceIDs {
			restartCtx, restartCancel := context.WithTimeout(ctx, childRestartOutcomeTimeout)
			err := a.restartRelayChildCodexAndWait(restartCtx, instanceID)
			restartCancel()
			if err != nil {
				runErr = fmt.Errorf("restart child codex for %s: %w", instanceID, err)
				break
			}
		}
	}

	a.mu.Lock()
	events := a.finishStandaloneCodexUpgradeLocked(tx)
	if len(events) != 0 {
		a.handleUIEventsLocked(context.Background(), events)
	}
	a.mu.Unlock()

	if onComplete != nil {
		onComplete(runErr)
	}
}

func (a *App) finishStandaloneCodexUpgradeLocked(tx *codexupgraderuntime.Transaction) []eventcontract.Event {
	if tx == nil {
		return nil
	}
	active := a.codexUpgradeRuntime.Active
	if active == nil || active.ID != tx.ID {
		return nil
	}
	a.codexUpgradeRuntime.Active = nil
	surfaceIDs := make([]string, 0, len(active.PausedSurfaceIDs))
	for surfaceID := range active.PausedSurfaceIDs {
		surfaceIDs = append(surfaceIDs, surfaceID)
	}
	sort.Strings(surfaceIDs)
	events := make([]eventcontract.Event, 0, len(surfaceIDs))
	for _, surfaceID := range surfaceIDs {
		events = append(events, a.service.ResumeSurfaceDispatch(surfaceID, nil)...)
	}
	return events
}

func (a *App) pauseCodexUpgradeSurfacesLocked(tx *codexupgraderuntime.Transaction) {
	if tx == nil {
		return
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil {
			continue
		}
		surfaceID := strings.TrimSpace(surface.SurfaceSessionID)
		if surfaceID == "" || surfaceID == tx.InitiatorSurface || !a.standaloneCodexUpgradeAffectsSurfaceLocked(surface) {
			continue
		}
		a.service.PauseSurfaceDispatch(surfaceID)
		tx.PausedSurfaceIDs[surfaceID] = true
	}
}

func (a *App) nextCodexUpgradeIDLocked() string {
	a.codexUpgradeRuntime.NextSeq++
	return fmt.Sprintf("codex-upgrade-%d", a.codexUpgradeRuntime.NextSeq)
}

func (a *App) affectedOnlineInstanceIDsLocked() []string {
	instances := a.service.Instances()
	values := make([]string, 0, len(instances))
	for _, inst := range instances {
		if inst == nil || !inst.Online || !standaloneCodexUpgradeAffectsInstance(inst) {
			continue
		}
		values = append(values, inst.InstanceID)
	}
	sort.Strings(values)
	return values
}

func (a *App) codexUpgradeBusyReasonsLocked(initiatorSurfaceID string) []codexUpgradeBusyReason {
	var reasons []codexUpgradeBusyReason
	for _, inst := range a.service.Instances() {
		if inst == nil || !inst.Online || !standaloneCodexUpgradeAffectsInstance(inst) {
			continue
		}
		if strings.TrimSpace(inst.ActiveTurnID) != "" {
			reasons = append(reasons, codexUpgradeBusyReason{
				InstanceID: inst.InstanceID,
				Reason:     "active_turn",
			})
		}
	}
	for _, pending := range a.service.PendingRemoteTurns() {
		if !a.standaloneCodexUpgradeAffectsInstanceIDLocked(pending.InstanceID) {
			continue
		}
		reasons = append(reasons, codexUpgradeBusyReason{
			InstanceID: pending.InstanceID,
			SurfaceID:  pending.SurfaceSessionID,
			Reason:     "pending_remote_dispatch",
		})
	}
	for _, surface := range a.service.Surfaces() {
		if surface == nil || !a.standaloneCodexUpgradeAffectsSurfaceLocked(surface) {
			continue
		}
		if reason := codexUpgradeSurfaceBusyReason(surface, initiatorSurfaceID); reason != "" {
			reasons = append(reasons, codexUpgradeBusyReason{
				InstanceID: strings.TrimSpace(surface.AttachedInstanceID),
				SurfaceID:  strings.TrimSpace(surface.SurfaceSessionID),
				Reason:     reason,
			})
		}
	}
	return reasons
}

func codexUpgradeSurfaceBusyReason(surface *state.SurfaceConsoleRecord, initiatorSurfaceID string) string {
	if surface == nil {
		return ""
	}
	if strings.TrimSpace(surface.SurfaceSessionID) == strings.TrimSpace(initiatorSurfaceID) {
		return ""
	}
	switch {
	case surface.Abandoning:
		return "surface_abandoning"
	case surface.PendingHeadless != nil:
		return "pending_headless"
	case surface.ActiveQueueItemID != "":
		return "active_queue_item"
	case len(surface.QueuedQueueItemIDs) != 0:
		return "queued_inputs"
	case surface.ActiveRequestCapture != nil:
		return "request_capture"
	case len(surface.PendingRequests) != 0:
		return "pending_requests"
	default:
		return ""
	}
}

func formatCodexUpgradeBusyReasons(reasons []codexUpgradeBusyReason) string {
	if len(reasons) == 0 {
		return ""
	}
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		label := strings.TrimSpace(reason.Reason)
		if instanceID := strings.TrimSpace(reason.InstanceID); instanceID != "" {
			label = instanceID + ":" + label
		}
		if surfaceID := strings.TrimSpace(reason.SurfaceID); surfaceID != "" {
			label = label + "@" + surfaceID
		}
		parts = append(parts, label)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
