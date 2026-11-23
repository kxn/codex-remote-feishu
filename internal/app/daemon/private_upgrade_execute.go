package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

var privateEnsureReleaseBinary = install.EnsureReleaseBinary
var privateStartUpgradeHelper = install.StartUpgradeHelperProcess

type privateUpgradeExecutionReceipt struct {
	Version              int
	EffectID             string
	Before, After        *orchestrator.PrivateUpgradeFlow
	Event                json.RawMessage
	CancellationObserved bool
}
type privateUpgradeExecutionReducer struct{ host *App }

func (r privateUpgradeExecutionReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var result privateUpgradeExecutionReceipt
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	cmd := original.DaemonCommand
	if result.Version != 1 || result.EffectID != effect.ID || cmd == nil || cmd.Kind != control.DaemonCommandUpgradeOwnerFlow || (cmd.OptionID != privateUpgradeExecute && cmd.OptionID != privateUpgradeResult) || result.Before == nil {
		return nil, privatechat.ErrJournalConflict
	}
	current := app.Service().PrivateUpgradeFlow(original.SurfaceSessionID)
	if result.CancellationObserved && current != nil && current.Phase == "running" {
		return nil, privatechat.ErrDeferred
	}
	if !reflect.DeepEqual(current, result.Before) {
		if current == nil || current.Phase != "cancelling" {
			return nil, privatechat.ErrJournalConflict
		}
		copy := *current
		copy.Phase = result.Before.Phase
		if !reflect.DeepEqual(&copy, result.Before) {
			return nil, privatechat.ErrJournalConflict
		}
	}
	if err = app.Service().SetPrivateUpgradeFlow(original.SurfaceSessionID, result.After); err != nil {
		return nil, err
	}
	event, err := privatechat.DecodeEffectEvent(result.Event)
	if err != nil {
		return nil, err
	}
	if event.SurfaceSessionID != original.SurfaceSessionID || event.Command != nil || event.DaemonCommand != nil {
		return nil, privatechat.ErrJournalConflict
	}
	out := []eventcontract.Event{event}
	if result.After != nil && result.After.Phase == "restarting" {
		next := control.DaemonCommand{Kind: control.DaemonCommandUpgradeOwnerFlow, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, SourceMessageID: cmd.SourceMessageID, PickerID: cmd.PickerID, OptionID: privateUpgradeResult}
		out = append(out, eventcontract.Event{Kind: eventcontract.KindDaemonCommand, GatewayID: cmd.GatewayID, SurfaceSessionID: cmd.SurfaceSessionID, DaemonCommand: &next})
	}
	return out, nil
}

// A prepared execution is attempted once. An unknown attempt never launches a
// second helper; its install journal and external process must be reconciled.
func runPrivateUpgradeExecution(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, cmd control.DaemonCommand) ([]privatechat.EffectRecord, error) {
	reducer := privateUpgradeExecutionReducer{host}
	if record.State == "confirmed" {
		return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, reducer)
	}
	if record.State == "unknown" {
		return reconcilePrivateUpgradeResult(ctx, host, p, j, o, generation, record, cmd, true)
	}
	if record.State != "prepared" {
		return nil, privatechat.ErrDeferred
	}
	var err error
	var flow *orchestrator.PrivateUpgradeFlow
	if err = p.InspectEffect(ctx, o, generation, record.Effect.ID, func(app *privatechat.App, _ privatechat.EffectRecord) error {
		flow = app.Service().PrivateUpgradeFlow(o.SurfaceID)
		return nil
	}); err != nil {
		return nil, err
	}
	if flow == nil || flow.ID != cmd.PickerID || flow.OwnerUserID != o.UserID || (flow.Phase != "running" && flow.Phase != "cancelling") || (flow.Source != string(install.UpgradeSourceRelease) && flow.Source != string(install.UpgradeSourceDev) && flow.Source != string(install.UpgradeSourceLocal)) {
		return nil, privatechat.ErrJournalConflict
	}
	if err = j.MarkUpgradeExecutionSending(ctx, o, generation, record.Effect.ID, flow.ID); errors.Is(err, privatechat.ErrUpgradeCancelled) {
		if err = j.MarkEffectSending(ctx, o, generation, record.Effect.ID); err != nil {
			return nil, err
		}
		return finishPrivateUpgradeExecution(ctx, host, p, j, o, generation, record, flow, "cancelled", "升级已取消", "info", "已取消这次升级。", true)
	} else if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, upgradePrepareTimeout)
	defer cancel()
	claimed := false
	host.mu.Lock()
	state, _, err := host.loadUpgradeStateLocked(true)
	if err == nil && (host.upgradeRuntime.CheckInFlight || host.upgradeRuntime.StartInFlight || pendingUpgradeBusy(state.PendingUpgrade) || install.UpgradeStateFingerprint(state) != flow.StateFingerprint) {
		err = fmt.Errorf("升级配置或已有事务已变化，请重新检查")
	}
	if err == nil {
		host.upgradeRuntime.StartInFlight = true
		host.upgradeRuntime.StartCancel = cancel
		host.upgradeRuntime.StartFlowID = flow.ID
		if flow.Source != string(install.UpgradeSourceLocal) {
			now := time.Now().UTC()
			state.PendingUpgrade = &install.PendingUpgrade{Phase: install.PendingUpgradePhaseAvailable, Source: install.UpgradeSource(flow.Source), TargetTrack: install.ReleaseTrack(flow.Track), TargetVersion: flow.TargetVersion, TargetSlot: flow.TargetVersion, RequestedAt: &now, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, SourceMessageID: flow.MessageID, PrivateRequestID: flow.ID}
			err = host.writeUpgradeStateLocked(state)
			claimed = err == nil
		}
	}
	host.mu.Unlock()
	cancelled, cancelErr := j.UpgradeCancelled(ctx, o, generation, flow.ID)
	if cancelErr != nil {
		return nil, cancelErr
	}
	if cancelled {
		cancel()
		if err == nil {
			err = context.Canceled
		}
	}
	launchAttempted := false
	if err == nil {
		launchAttempted, err = prepareAndLaunchPrivateUpgrade(runCtx, host, &state, flow, o)
	}
	host.mu.Lock()
	if host.upgradeRuntime.StartFlowID == flow.ID {
		host.upgradeRuntime.StartInFlight = false
		host.upgradeRuntime.StartCancel = nil
		host.upgradeRuntime.StartFlowID = ""
	}
	if err != nil && claimed && state.PendingUpgrade != nil {
		current, ok, e := host.loadUpgradeStateLocked(false)
		if e == nil && ok && current.PendingUpgrade != nil && current.PendingUpgrade.SurfaceSessionID == o.SurfaceID && current.PendingUpgrade.TargetVersion == flow.TargetVersion && current.PendingUpgrade.Phase == install.PendingUpgradePhaseAvailable {
			current.PendingUpgrade = nil
			_ = host.writeUpgradeStateLocked(current)
		}
	}
	host.mu.Unlock()
	if launchAttempted && err != nil {
		// The helper may have started before a transport/process error or a
		// subsequent journal write failed. Never report failure or relaunch.
		raw, _ := json.Marshal(map[string]string{"error": err.Error()})
		_ = j.ResolveEffect(context.WithoutCancel(ctx), o, generation, record.Effect.ID, "unknown", raw, nil)
		return nil, privatechat.ErrDeferred
	}
	cancelled, cancelErr = j.UpgradeCancelled(context.WithoutCancel(ctx), o, generation, flow.ID)
	if cancelErr != nil {
		return nil, cancelErr
	}
	phase, title, theme, text := "restarting", "正在重启", "info", "升级事务已启动，服务将短暂重启。"
	if err != nil {
		phase, title, theme, text = "failed", "升级失败", "error", err.Error()
		if cancelled && errors.Is(err, context.Canceled) {
			phase, title, theme, text = "cancelled", "升级已取消", "info", "已取消这次升级。"
		}
	}
	return finishPrivateUpgradeExecution(ctx, host, p, j, o, generation, record, flow, phase, title, theme, text, cancelled)
}
func finishPrivateUpgradeExecution(ctx context.Context, host *App, p *privatechat.Processor, j *privatechat.Journal, o privatechat.Owner, generation int64, record privatechat.EffectRecord, flow *orchestrator.PrivateUpgradeFlow, phase, title, theme, text string, cancelled bool) ([]privatechat.EffectRecord, error) {
	updated := *flow
	updated.Phase = phase
	view := upgradePrivateOwnerView(o, &updated)
	event := upgradeOwnerTerminalEvent(o.SurfaceID, view, title, theme, upgradeOwnerContextSections(flow.CurrentVersion, flow.TargetVersion, flow.Track), upgradeOwnerNoticeSections(text))
	event.GatewayID = o.GatewayID
	event.SurfaceSessionID = o.SurfaceID
	event.SourceMessageID = flow.MessageID
	event.InlineReplaceCurrentCard = true
	eventRaw, e := privatechat.EncodeEffectEvent(event)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(privateUpgradeExecutionReceipt{Version: 1, EffectID: record.Effect.ID, Before: flow, After: &updated, Event: eventRaw, CancellationObserved: cancelled})
	if e != nil {
		return nil, e
	}
	commitCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if e = j.ConfirmLocalEffect(commitCtx, o, generation, record.Effect.ID, raw, privateLocalVerifier{o, generation, record.Effect, raw}); e != nil {
		return nil, e
	}
	return p.ApplyLocalHostReceipt(ctx, o, generation, record.Effect.ID, privateUpgradeExecutionReducer{host})
}
func prepareAndLaunchPrivateUpgrade(ctx context.Context, host *App, state *install.InstallState, flow *orchestrator.PrivateUpgradeFlow, o privatechat.Owner) (bool, error) {
	if flow.Source == string(install.UpgradeSourceLocal) {
		artifact, err := freezePrivateLocalArtifact(*state, flow)
		if err != nil {
			return false, err
		}
		defer os.Remove(artifact)
		_, err = privateRunLocalUpgrade(install.LocalBinaryUpgradeOptions{StatePath: state.StatePath, SourceBinary: artifact, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, SourceMessageID: flow.MessageID, PrivateRequestID: flow.ID, SourceSHA256: flow.SourceSHA256})
		return true, err
	}
	var binary string
	var err error
	switch install.UpgradeSource(flow.Source) {
	case install.UpgradeSourceRelease:
		binary, err = privateEnsureReleaseBinary(ctx, install.ReleaseBinaryOptions{Repository: strings.TrimSpace(os.Getenv(config.CodexRemoteRepoEnv)), BaseURL: strings.TrimSpace(os.Getenv(config.CodexRemoteBaseURLEnv)), Version: flow.TargetVersion, VersionsRoot: state.VersionsRoot})
	case install.UpgradeSourceDev:
		host.mu.Lock()
		lookup := host.upgradeRuntime.DevManifest
		if lookup == nil {
			lookup = host.defaultDevManifestLookup
		}
		host.mu.Unlock()
		var manifest install.DevManifest
		var asset install.DevManifestAsset
		manifest, asset, err = lookup(ctx)
		if err == nil && manifest.Version != flow.TargetVersion {
			err = fmt.Errorf("dev 构建已变化，请重新检查")
		}
		if err == nil {
			binary, err = install.EnsureDevBinary(ctx, install.DevBinaryOptions{Manifest: manifest, Asset: asset, VersionsRoot: state.VersionsRoot})
		}
	default:
		err = fmt.Errorf("unsupported upgrade source")
	}
	if err != nil {
		return false, fmt.Errorf("下载目标版本失败：%w", err)
	}
	rollback, err := install.PrepareRollbackCandidate(*state, flow.TargetVersion)
	if err != nil {
		return false, err
	}
	identity, err := relayruntime.BinaryIdentityForPath(state.CurrentBinaryPath, state.CurrentVersion)
	if err != nil {
		return false, err
	}
	rollback.Fingerprint = identity.BuildFingerprint
	state.RollbackCandidate = rollback
	state.PendingUpgrade.Phase = install.PendingUpgradePhasePrepared
	state.PendingUpgrade.TargetBinaryPath = binary
	host.mu.Lock()
	logPath := host.upgradeHelperLogPathLocked()
	err = host.writeUpgradeStateLocked(*state)
	var helper string
	if err == nil {
		helper, err = host.prepareUpgradeHelperShimLocked(*state)
	}
	host.mu.Unlock()
	if err != nil {
		return false, err
	}
	result, err := privateStartUpgradeHelper(context.Background(), install.UpgradeHelperLaunchOptions{State: *state, HelperBinary: helper, StatePath: state.StatePath, LogPath: logPath, Env: append(append([]string{}, os.Environ()...), install.RuntimeEnvForState(*state)...), DirectExec: true})
	if err != nil {
		return true, err
	}
	host.mu.Lock()
	current, exists, err := host.loadUpgradeStateLocked(false)
	if err == nil && exists && current.PendingUpgrade != nil && current.PendingUpgrade.TargetVersion == flow.TargetVersion && current.PendingUpgrade.Phase == install.PendingUpgradePhasePrepared {
		current.PendingUpgrade.HelperUnitName = strings.TrimSpace(result.UnitName)
		err = host.writeUpgradeStateLocked(current)
	}
	host.mu.Unlock()
	return true, err
}
