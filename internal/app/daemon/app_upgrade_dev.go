package daemon

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

type devManifestLookupFunc func(context.Context) (install.DevManifest, install.DevManifestAsset, error)

type devUpgradeCheckRequest struct {
	Manual           bool
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
}

func (a *App) defaultDevManifestLookup(ctx context.Context) (install.DevManifest, install.DevManifestAsset, error) {
	return install.ResolveDevManifest(ctx, install.DevManifestLookupOptions{
		Repository:  strings.TrimSpace(os.Getenv("CODEX_REMOTE_REPO")),
		ManifestURL: strings.TrimSpace(os.Getenv("CODEX_REMOTE_DEV_MANIFEST_URL")),
	})
}

func (a *App) handleUpgradeDevCommand(command control.DaemonCommand, stateValue install.InstallState) []control.UIEvent {
	if a.upgradeCheckInFlight {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_busy", "当前已经有一个升级检查在进行中，请稍后再试。")}
	}
	if a.upgradeStartInFlight {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", "当前升级准备已经开始，服务会短暂重启，请稍后查看结果。")}
	}
	if clearStalePendingCandidateOnLiveVersion(&stateValue, a.currentBinaryVersion()) {
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("清理陈旧升级候选失败：%v", err))}
		}
	}
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceDev) {
		return a.beginPendingUpgradeLocked(command, stateValue)
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", fmt.Sprintf("当前升级事务处于 %s，暂时不能发起新检查。", stateValue.PendingUpgrade.Phase))}
	}
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceRelease) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_pending_other_source", "当前已有 release 升级候选，请先完成 `/upgrade latest`，或重新检查当前来源。")}
	}

	a.upgradeCheckInFlight = true
	go a.runDevUpgradeCheck(devUpgradeCheckRequest{
		Manual:           true,
		GatewayID:        command.GatewayID,
		SurfaceSessionID: command.SurfaceSessionID,
		SourceMessageID:  command.SourceMessageID,
	})
	return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_dev_check_started", "正在检查最新 dev 构建。")}
}

func (a *App) runDevUpgradeCheck(request devUpgradeCheckRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	lookup := a.devManifestLookup
	if lookup == nil {
		lookup = a.defaultDevManifestLookup
	}
	manifest, asset, err := lookup(ctx)
	completedAt := time.Now().UTC()

	a.mu.Lock()
	defer a.mu.Unlock()

	a.upgradeCheckInFlight = false
	a.upgradeNextCheckAt = completedAt.Add(a.upgradeCheckInterval)

	events := a.applyDevUpgradeCheckResultLocked(request, manifest, asset, err, completedAt)
	if len(events) > 0 {
		a.handleUIEvents(context.Background(), events)
	}
}

func (a *App) applyDevUpgradeCheckResultLocked(request devUpgradeCheckRequest, manifest install.DevManifest, _ install.DevManifestAsset, lookupErr error, completedAt time.Time) []control.UIEvent {
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		if request.Manual {
			return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
		}
		return nil
	}

	stateValue.LastCheckAt = &completedAt
	if lookupErr != nil {
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return nil
		}
		if request.Manual {
			return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_dev_check_failed", fmt.Sprintf("检查 dev 构建失败：%v", lookupErr))}
		}
		return nil
	}

	latestVersion := strings.TrimSpace(manifest.Version)
	stateValue.LastKnownLatestVersion = latestVersion
	currentVersion := strings.TrimSpace(stateValue.CurrentVersion)
	if currentVersion != "" && latestVersion == currentVersion {
		if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceDev) && stateValue.PendingUpgrade.TargetVersion == latestVersion {
			stateValue.PendingUpgrade = nil
		}
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return nil
		}
		if request.Manual {
			return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_dev_latest", fmt.Sprintf("当前已经是最新 dev 构建 %s。", latestVersion))}
		}
		return nil
	}

	if stateValue.PendingUpgrade == nil || !pendingUpgradeBusy(stateValue.PendingUpgrade) {
		requestedAt := completedAt
		stateValue.PendingUpgrade = &install.PendingUpgrade{
			Phase:         install.PendingUpgradePhaseAvailable,
			Source:        install.UpgradeSourceDev,
			TargetTrack:   stateValue.CurrentTrack,
			TargetVersion: latestVersion,
			TargetSlot:    latestVersion,
			RequestedAt:   &requestedAt,
		}
	}

	pending := stateValue.PendingUpgrade
	pending.GatewayID = firstNonEmpty(strings.TrimSpace(request.GatewayID), a.service.SurfaceGatewayID(request.SurfaceSessionID))
	pending.SurfaceSessionID = request.SurfaceSessionID
	pending.ChatID = a.service.SurfaceChatID(request.SurfaceSessionID)
	pending.ActorUserID = a.service.SurfaceActorUserID(request.SurfaceSessionID)
	pending.SourceMessageID = request.SourceMessageID
	if a.surfaceIsIdleForUpgradeLocked(request.SurfaceSessionID) {
		events := a.promptPendingUpgradeOnSurfaceLocked(request.SurfaceSessionID, stateValue, completedAt)
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("写入升级状态失败：%v", err))}
		}
		return events
	}
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("写入升级状态失败：%v", err))}
	}
	return []control.UIEvent{upgradeNoticeEvent(request.SurfaceSessionID, "upgrade_dev_candidate_pending", fmt.Sprintf("发现新的 dev 构建 %s，但当前窗口不空闲，已记录候选升级。等当前窗口空闲后，再次发送 /upgrade dev 继续升级。", latestVersion))}
}
