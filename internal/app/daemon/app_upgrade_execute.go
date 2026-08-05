package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type upgradeStartRequest struct {
	State            install.InstallState
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
	FlowID           string
	Context          context.Context
}

func (a *App) beginPendingUpgradeLocked(command control.DaemonCommand, stateValue install.InstallState) []eventcontract.Event {
	if stateValue.PendingUpgrade == nil || strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion) == "" {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_missing_candidate", "当前没有可继续的升级候选，请先发送 /upgrade latest 检查最新版本。")}
	}
	now := time.Now().UTC()
	stateValue.PendingUpgrade.GatewayID = xutil.FirstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	stateValue.PendingUpgrade.SurfaceSessionID = command.SurfaceSessionID
	stateValue.PendingUpgrade.ChatID = a.service.SurfaceChatID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.ActorUserID = a.service.SurfaceActorUserID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.SourceMessageID = command.SourceMessageID
	if stateValue.PendingUpgrade.RequestedAt == nil {
		stateValue.PendingUpgrade.RequestedAt = &now
	}
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("写入升级事务失败：%v", err))}
	}
	a.upgradeRuntime.StartInFlight = true
	go a.runPendingUpgradeStart(upgradeStartRequest{
		State:            stateValue,
		GatewayID:        stateValue.PendingUpgrade.GatewayID,
		SurfaceSessionID: stateValue.PendingUpgrade.SurfaceSessionID,
		SourceMessageID:  stateValue.PendingUpgrade.SourceMessageID,
	})
	return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_started", fmt.Sprintf("正在准备升级到 %s，服务会短暂重启。", xutil.FirstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))))}
}

func (a *App) runPendingUpgradeStart(request upgradeStartRequest) {
	baseCtx := request.Context
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(baseCtx, upgradePrepareTimeout)
	defer cancel()

	stateValue := request.State
	targetVersion := strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion)
	if request.FlowID != "" {
		a.enqueueDaemonAsyncResult(daemonAsyncResult{Apply: func(ctx context.Context, app *App) {
			app.handleUIEventsLocked(ctx, app.updateUpgradeOwnerFlowRunningLocked(request.SurfaceSessionID, request.FlowID, upgraderuntime.OwnerFlowStageRunning, "正在下载目标版本", "正在下载升级所需的目标版本。", true))
		}})
	}
	var targetBinary string
	var err error
	switch stateValue.PendingUpgrade.Source {
	case install.UpgradeSourceRelease:
		targetBinary, err = install.EnsureReleaseBinary(ctx, install.ReleaseBinaryOptions{
			Repository:   strings.TrimSpace(os.Getenv("CODEX_REMOTE_REPO")),
			BaseURL:      strings.TrimSpace(os.Getenv("CODEX_REMOTE_BASE_URL")),
			Version:      targetVersion,
			VersionsRoot: stateValue.VersionsRoot,
		})
	case install.UpgradeSourceDev:
		lookup := a.upgradeRuntime.DevManifest
		if lookup == nil {
			lookup = a.defaultDevManifestLookup
		}
		manifest, asset, lookupErr := lookup(ctx)
		if lookupErr != nil {
			a.finishUpgradeStartFailure(request, fmt.Errorf("读取 dev manifest 失败：%w", lookupErr))
			return
		}
		if latestVersion := strings.TrimSpace(manifest.Version); latestVersion != targetVersion {
			a.finishUpgradeStartFailure(request, fmt.Errorf("dev 构建已从 %s 更新到 %s，请重新发送 /upgrade dev 确认最新版本", targetVersion, latestVersion))
			return
		}
		targetBinary, err = install.EnsureDevBinary(ctx, install.DevBinaryOptions{
			Manifest:     manifest,
			Asset:        asset,
			VersionsRoot: stateValue.VersionsRoot,
		})
	default:
		err = fmt.Errorf("不支持的升级来源 %q", stateValue.PendingUpgrade.Source)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.finishUpgradeStartFailure(request, context.Canceled)
			return
		}
		a.finishUpgradeStartFailure(request, fmt.Errorf("下载目标版本失败：%w", err))
		return
	}

	if request.FlowID != "" {
		a.enqueueDaemonAsyncResult(daemonAsyncResult{Apply: func(ctx context.Context, app *App) {
			app.handleUIEventsLocked(ctx, app.updateUpgradeOwnerFlowRunningLocked(request.SurfaceSessionID, request.FlowID, upgraderuntime.OwnerFlowStageRunning, "正在准备回滚方案", "目标版本已下载，正在准备回滚信息和切换事务。", true))
		}})
	}
	rollbackCandidate, err := install.PrepareRollbackCandidate(stateValue, targetVersion)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.finishUpgradeStartFailure(request, context.Canceled)
			return
		}
		a.finishUpgradeStartFailure(request, fmt.Errorf("准备回滚候选失败：%w", err))
		return
	}
	identity, err := relayruntime.BinaryIdentityForPath(stateValue.CurrentBinaryPath, stateValue.CurrentVersion)
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("读取当前版本指纹失败：%w", err))
		return
	}
	rollbackCandidate.Fingerprint = identity.BuildFingerprint
	stateValue.RollbackCandidate = rollbackCandidate
	stateValue.PendingUpgrade.Phase = install.PendingUpgradePhasePrepared
	stateValue.PendingUpgrade.TargetSlot = xutil.FirstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), targetVersion)
	stateValue.PendingUpgrade.TargetBinaryPath = targetBinary

	a.mu.Lock()
	logPath := a.upgradeHelperLogPathLocked()
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		a.upgradeRuntime.StartInFlight = false
		a.upgradeRuntime.StartCancel = nil
		a.upgradeRuntime.StartFlowID = ""
		a.mu.Unlock()
		a.finishUpgradeStartFailure(request, fmt.Errorf("写入 prepared journal 失败：%w", err))
		return
	}
	helperPath, err := a.prepareUpgradeHelperShimLocked(stateValue)
	if err != nil {
		a.upgradeRuntime.StartInFlight = false
		a.upgradeRuntime.StartCancel = nil
		a.upgradeRuntime.StartFlowID = ""
		a.mu.Unlock()
		a.finishUpgradeStartFailure(request, fmt.Errorf("复制 upgrade helper 失败：%w", err))
		return
	}
	if request.FlowID != "" {
		events := a.sealUpgradeOwnerFlowRestartingLocked(request.SurfaceSessionID, request.FlowID)
		a.queueDaemonAsyncUIEventsLocked(events)
	}
	a.mu.Unlock()

	launchResult, err := install.StartUpgradeHelperProcess(context.Background(), install.UpgradeHelperLaunchOptions{
		State:        stateValue,
		HelperBinary: helperPath,
		StatePath:    stateValue.StatePath,
		LogPath:      logPath,
		Env:          append(append([]string{}, os.Environ()...), install.RuntimeEnvForState(stateValue)...),
		DirectExec:   true,
	})
	if err != nil {
		a.finishUpgradeStartFailure(request, fmt.Errorf("启动 upgrade helper 失败：%w", err))
		return
	}
	a.mu.Lock()
	stateValue.PendingUpgrade.HelperUnitName = strings.TrimSpace(launchResult.UnitName)
	_ = a.writeUpgradeStateLocked(stateValue)
	a.upgradeRuntime.StartCancel = nil
	a.upgradeRuntime.StartFlowID = ""
	a.mu.Unlock()

	_ = targetBinary
}

func (a *App) finishUpgradeStartFailure(request upgradeStartRequest, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.upgradeRuntime.StartInFlight = false
	a.upgradeRuntime.StartCancel = nil
	a.upgradeRuntime.StartFlowID = ""

	stateValue, ok, loadErr := a.loadUpgradeStateLocked(true)
	if loadErr == nil && ok && stateValue.PendingUpgrade != nil {
		stateValue.PendingUpgrade = nil
		_ = a.writeUpgradeStateLocked(stateValue)
	}
	if request.FlowID != "" {
		events := a.finishUpgradeOwnerStartErrorLocked(request, err)
		a.queueDaemonAsyncUIEventsLocked(events)
		return
	}
	if request.SurfaceSessionID != "" {
		a.queueDaemonAsyncUIEventsLocked([]eventcontract.Event{
			upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_prepare_failed", err.Error()),
		})
	}
}

func (a *App) prepareUpgradeHelperShimLocked(stateValue install.InstallState) (string, error) {
	return install.PrepareUpgradeHelperShim(a.installStatePath(), stateValue.InstanceID)
}

func (a *App) upgradeHelperLogPathLocked() string {
	if strings.TrimSpace(a.headlessRuntime.Paths.DaemonLogFile) != "" {
		return a.headlessRuntime.Paths.DaemonLogFile
	}
	paths, err := relayruntime.DefaultPaths()
	if err != nil {
		return filepath.Join(filepath.Dir(a.installStatePath()), "upgrade-helper.log")
	}
	return paths.DaemonLogFile
}

func (a *App) maybeFlushUpgradeResultLocked(now time.Time) []eventcontract.Event {
	if a.upgradeRuntime.ResultScanEvery <= 0 {
		return nil
	}
	if !a.upgradeRuntime.NextResultScan.IsZero() && now.Before(a.upgradeRuntime.NextResultScan) {
		return nil
	}
	a.upgradeRuntime.NextResultScan = now.Add(a.upgradeRuntime.ResultScanEvery)

	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil || !ok || stateValue.PendingUpgrade == nil {
		return nil
	}
	switch stateValue.PendingUpgrade.Phase {
	case install.PendingUpgradePhaseCommitted, install.PendingUpgradePhaseRolledBack, install.PendingUpgradePhaseFailed:
	default:
		return nil
	}
	pending := stateValue.PendingUpgrade
	if strings.TrimSpace(pending.SurfaceSessionID) == "" || strings.TrimSpace(pending.ChatID) == "" {
		return nil
	}
	if pending.ResultDeliveryNextAttemptAt != nil && now.Before(*pending.ResultDeliveryNextAttemptAt) {
		return nil
	}
	pending.ResultDeliveryAttempts++
	pending.ResultDeliveryLastAttemptAt = &now
	pending.ResultDeliveryLastError = ""
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return nil
	}
	a.service.MaterializeSurface(pending.SurfaceSessionID, pending.GatewayID, pending.ChatID, pending.ActorUserID)
	return []eventcontract.Event{upgradeNoticeEvent(pending.SurfaceSessionID, "upgrade_result", buildUpgradeResultText(stateValue))}
}

func buildUpgradeResultText(stateValue install.InstallState) string {
	pending := stateValue.PendingUpgrade
	if pending == nil {
		return "升级结果已就绪。"
	}
	switch pending.Phase {
	case install.PendingUpgradePhaseCommitted:
		return fmt.Sprintf("已升级到 %s。", xutil.FirstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion)))
	case install.PendingUpgradePhaseRolledBack:
		return fmt.Sprintf("升级到 %s 失败，已自动回滚到 %s。", xutil.FirstNonEmpty(strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion), "unknown"), xutil.FirstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown"))
	default:
		return "升级失败。"
	}
}

func isUpgradeResultDeliveryEvent(event eventcontract.Event) bool {
	if event.Kind != eventcontract.KindNotice || event.Notice == nil {
		return false
	}
	return strings.TrimSpace(event.Notice.Code) == "upgrade_result"
}

func (a *App) ackUpgradeResultDeliveryLocked(event eventcontract.Event) {
	if !isUpgradeResultDeliveryEvent(event) {
		return
	}
	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil || !ok || stateValue.PendingUpgrade == nil {
		return
	}
	if !pendingUpgradeMatchesResultEvent(stateValue.PendingUpgrade, event) {
		return
	}
	stateValue.PendingUpgrade = nil
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		logUpgradeResultDeliveryStateError("ack", event, err)
	}
}

func (a *App) recordUpgradeResultDeliveryFailureLocked(event eventcontract.Event, deliveryErr error, now time.Time) {
	if !isUpgradeResultDeliveryEvent(event) {
		return
	}
	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil || !ok || stateValue.PendingUpgrade == nil {
		return
	}
	pending := stateValue.PendingUpgrade
	if !pendingUpgradeMatchesResultEvent(pending, event) {
		return
	}
	pending.ResultDeliveryLastError = strings.TrimSpace(deliveryErr.Error())
	if pending.ResultDeliveryLastError == "" {
		pending.ResultDeliveryLastError = "delivery failed"
	}
	next := now.Add(a.upgradeRuntime.ResultScanEvery)
	if a.upgradeRuntime.ResultScanEvery <= 0 {
		next = now.Add(time.Minute)
	}
	pending.ResultDeliveryNextAttemptAt = &next
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		logUpgradeResultDeliveryStateError("failure", event, err)
	}
}

func pendingUpgradeMatchesResultEvent(pending *install.PendingUpgrade, event eventcontract.Event) bool {
	if pending == nil {
		return false
	}
	if strings.TrimSpace(pending.SurfaceSessionID) != strings.TrimSpace(event.SurfaceSessionID) {
		return false
	}
	switch pending.Phase {
	case install.PendingUpgradePhaseCommitted, install.PendingUpgradePhaseRolledBack, install.PendingUpgradePhaseFailed:
		return true
	default:
		return false
	}
}

func logUpgradeResultDeliveryStateError(action string, event eventcontract.Event, err error) {
	log.Printf("upgrade result delivery %s state write failed: surface=%s err=%v", action, event.SurfaceSessionID, err)
}
