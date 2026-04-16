package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type releaseLookupFunc func(context.Context, install.ReleaseTrack) (install.ReleaseInfo, error)

type debugCommandMode string

const (
	debugCommandShowStatus debugCommandMode = "status"
	debugCommandShowTrack  debugCommandMode = "track_show"
	debugCommandSetTrack   debugCommandMode = "track_set"
	debugCommandAdmin      debugCommandMode = "admin"
	debugCommandUpgrade    debugCommandMode = "upgrade"
)

type parsedDebugCommand struct {
	Mode  debugCommandMode
	Track install.ReleaseTrack
}

type upgradeCommandMode string

const (
	upgradeCommandShowStatus upgradeCommandMode = "status"
	upgradeCommandShowTrack  upgradeCommandMode = "track_show"
	upgradeCommandSetTrack   upgradeCommandMode = "track_set"
	upgradeCommandLatest     upgradeCommandMode = "latest"
	upgradeCommandDev        upgradeCommandMode = "dev"
	upgradeCommandLocal      upgradeCommandMode = "local"
)

type parsedUpgradeCommand struct {
	Mode  upgradeCommandMode
	Track install.ReleaseTrack
}

type upgradeCheckRequest struct {
	Track            install.ReleaseTrack
	Manual           bool
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
}

func (a *App) defaultReleaseLookup(ctx context.Context, track install.ReleaseTrack) (install.ReleaseInfo, error) {
	return install.ResolveLatestRelease(ctx, install.ReleaseLookupOptions{
		Repository:     strings.TrimSpace(os.Getenv("CODEX_REMOTE_REPO")),
		ReleasesAPIURL: strings.TrimSpace(os.Getenv("CODEX_REMOTE_RELEASES_API_URL")),
		Track:          track,
	})
}

func (a *App) handleDebugDaemonCommand(command control.DaemonCommand) []control.UIEvent {
	parsed, err := parseDebugCommandText(command.Text)
	if err != nil {
		return debugUsageEvents(command.SurfaceSessionID, err.Error())
	}
	if parsed.Mode == debugCommandAdmin {
		return a.handleDebugAdminCommand(command)
	}

	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
	}

	switch parsed.Mode {
	case debugCommandShowStatus:
		return []control.UIEvent{{
			Kind:                       control.UIEventFeishuDirectCommandCatalog,
			SurfaceSessionID:           command.SurfaceSessionID,
			FeishuDirectCommandCatalog: buildDebugStatusCatalog(stateValue, a.upgradeCheckInFlight),
		}}
	case debugCommandShowTrack:
		return trackSummaryEvents(command.SurfaceSessionID, stateValue, true)
	case debugCommandSetTrack:
		return a.setTrackEvents(command.SurfaceSessionID, stateValue, parsed.Track, true)
	case debugCommandUpgrade:
		command.Text = "/upgrade latest"
		return a.handleUpgradeLatestCommand(command, stateValue)
	default:
		return debugUsageEvents(command.SurfaceSessionID, "不支持的 /debug 子命令。")
	}
}

func (a *App) handleDebugAdminCommand(command control.DaemonCommand) []control.UIEvent {
	service := a.externalAccess
	if service == nil {
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_admin_issue_failed", "生成管理页外链失败：external access 当前未启用。")}
	}
	adminURL := a.admin.adminURL
	localURL, err := a.ensureExternalAccessListenerLocked()
	if err != nil {
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_admin_issue_failed", fmt.Sprintf("生成管理页外链失败：%v", err))}
	}
	req := debugAdminIssueRequest(adminURL)
	surfaceID := command.SurfaceSessionID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		issued, err := service.IssueURL(ctx, req, localURL)

		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return
		}
		if err != nil {
			a.handleUIEventsLocked(context.Background(), []control.UIEvent{
				debugNoticeEvent(surfaceID, "debug_admin_issue_failed", fmt.Sprintf("生成管理页外链失败：%v", err)),
			})
			return
		}
		text := fmt.Sprintf(
			"临时管理页外链已生成：\n[打开管理页](%s)\n\n链接：`%s`\n有效期到：`%s`",
			issued.ExternalURL,
			issued.ExternalURL,
			issued.ExpiresAt.UTC().Format(time.RFC3339),
		)
		a.handleUIEventsLocked(context.Background(), []control.UIEvent{
			debugNoticeEvent(surfaceID, "debug_admin_link_ready", text),
		})
	}()
	return []control.UIEvent{
		debugNoticeEvent(command.SurfaceSessionID, "debug_admin_prepare_started", "正在准备临时管理页外链。首次启动 tunnel 或重新拉起 external access 时，可能需要几十秒，请稍候。"),
	}
}

func (a *App) handleUpgradeDaemonCommand(command control.DaemonCommand) []control.UIEvent {
	parsed, err := parseUpgradeCommandText(command.Text)
	if err != nil {
		return upgradeUsageEvents(command.SurfaceSessionID, err.Error())
	}

	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
	}

	switch parsed.Mode {
	case upgradeCommandShowStatus:
		return []control.UIEvent{{
			Kind:                       control.UIEventFeishuDirectCommandCatalog,
			SurfaceSessionID:           command.SurfaceSessionID,
			FeishuDirectCommandCatalog: buildUpgradeStatusCatalog(stateValue, a.upgradeCheckInFlight),
		}}
	case upgradeCommandShowTrack:
		return trackSummaryEvents(command.SurfaceSessionID, stateValue, false)
	case upgradeCommandSetTrack:
		return a.setTrackEvents(command.SurfaceSessionID, stateValue, parsed.Track, false)
	case upgradeCommandLatest:
		return a.handleUpgradeLatestCommand(command, stateValue)
	case upgradeCommandDev:
		return a.handleUpgradeDevCommand(command, stateValue)
	case upgradeCommandLocal:
		return a.handleUpgradeLocalCommand(command, stateValue)
	default:
		return upgradeUsageEvents(command.SurfaceSessionID, "不支持的 /upgrade 子命令。")
	}
}

func (a *App) handleUpgradeLatestCommand(command control.DaemonCommand, stateValue install.InstallState) []control.UIEvent {
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
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceRelease) {
		return a.beginPendingUpgradeLocked(command, stateValue)
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", fmt.Sprintf("当前升级事务处于 %s，暂时不能发起新检查。", stateValue.PendingUpgrade.Phase))}
	}
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceDev) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_pending_other_source", "当前已有 dev 构建升级候选，请改用 `/upgrade dev` 继续，或重新检查当前来源。")}
	}
	track := stateValue.CurrentTrack
	if track == "" {
		track = defaultUpgradeTrackForState(stateValue)
	}
	a.upgradeCheckInFlight = true
	go a.runUpgradeCheck(upgradeCheckRequest{
		Track:            track,
		Manual:           true,
		GatewayID:        command.GatewayID,
		SurfaceSessionID: command.SurfaceSessionID,
		SourceMessageID:  command.SourceMessageID,
	})
	return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_started", fmt.Sprintf("正在按 %s track 检查最新版本。", track))}
}

func (a *App) handleUpgradeLocalCommand(command control.DaemonCommand, stateValue install.InstallState) []control.UIEvent {
	if !install.CurrentBuildAllowsLocalUpgrade() {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_unsupported", "当前构建不支持 `/upgrade local`。如需本地升级，请使用 dev flavor 的源码构建。")}
	}
	if a.upgradeCheckInFlight {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_busy", "当前已有 release 检查在进行中，请稍后再试本地升级。")}
	}
	if a.upgradeStartInFlight || pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", "当前已有升级事务在进行中，请稍后再试。")}
	}
	artifactPath := install.LocalUpgradeArtifactPath(stateValue)
	if _, err := os.Stat(artifactPath); err != nil {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_artifact_missing", fmt.Sprintf("本地升级产物不存在：%s\n请先把新编译的 binary 放到这个固定路径，再发送 /upgrade local。", artifactPath))}
	}
	slot, err := install.RunLocalBinaryUpgradeWithStatePath(install.LocalBinaryUpgradeOptions{
		StatePath:    stateValue.StatePath,
		SourceBinary: artifactPath,
	})
	if err != nil {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_prepare_failed", fmt.Sprintf("准备本地升级失败：%v", err))}
	}
	return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_prepare_started", fmt.Sprintf("正在准备本地升级，目标 slot：%s。服务会短暂重启。", slot))}
}

func (a *App) runUpgradeCheck(request upgradeCheckRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	lookup := a.upgradeLookup
	if lookup == nil {
		lookup = a.defaultReleaseLookup
	}
	release, err := lookup(ctx, request.Track)
	completedAt := time.Now().UTC()

	a.mu.Lock()
	defer a.mu.Unlock()

	a.upgradeCheckInFlight = false
	a.upgradeNextCheckAt = completedAt.Add(a.upgradeCheckInterval)

	events := a.applyUpgradeCheckResultLocked(request, release, err, completedAt)
	if len(events) > 0 {
		a.handleUIEventsLocked(context.Background(), events)
	}
}

func (a *App) applyUpgradeCheckResultLocked(request upgradeCheckRequest, release install.ReleaseInfo, lookupErr error, completedAt time.Time) []control.UIEvent {
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		if request.Manual {
			return []control.UIEvent{debugNoticeEvent(request.SurfaceSessionID, "debug_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
		}
		log.Printf("upgrade check load state failed: %v", err)
		return nil
	}

	stateValue.LastCheckAt = &completedAt
	if lookupErr != nil {
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			log.Printf("upgrade check write state failed: %v", err)
		}
		if request.Manual {
			return []control.UIEvent{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_check_failed", fmt.Sprintf("检查更新失败：%v", lookupErr))}
		}
		log.Printf("upgrade check failed: track=%s err=%v", request.Track, lookupErr)
		return nil
	}

	latestVersion := strings.TrimSpace(release.TagName)
	stateValue.LastKnownLatestVersion = latestVersion
	currentVersion := strings.TrimSpace(stateValue.CurrentVersion)
	if currentVersion != "" && latestVersion == currentVersion {
		if pending := stateValue.PendingUpgrade; pending != nil &&
			pending.Source == install.UpgradeSourceRelease &&
			pending.TargetTrack == stateValue.CurrentTrack &&
			pending.TargetVersion == latestVersion &&
			pendingUpgradeCandidate(pending) {
			stateValue.PendingUpgrade = nil
		}
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			log.Printf("upgrade check write latest state failed: %v", err)
		}
		if request.Manual {
			return []control.UIEvent{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_latest", fmt.Sprintf("当前已经是 %s track 的最新版本 %s。", stateValue.CurrentTrack, latestVersion))}
		}
		return nil
	}

	if stateValue.PendingUpgrade == nil || !pendingUpgradeBusy(stateValue.PendingUpgrade) {
		requestedAt := completedAt
		stateValue.PendingUpgrade = &install.PendingUpgrade{
			Phase:         install.PendingUpgradePhaseAvailable,
			Source:        install.UpgradeSourceRelease,
			TargetTrack:   stateValue.CurrentTrack,
			TargetVersion: latestVersion,
			TargetSlot:    latestVersion,
			RequestedAt:   &requestedAt,
		}
	}

	if request.Manual {
		pending := stateValue.PendingUpgrade
		if pending != nil {
			pending.GatewayID = firstNonEmpty(strings.TrimSpace(request.GatewayID), a.service.SurfaceGatewayID(request.SurfaceSessionID))
			pending.SurfaceSessionID = request.SurfaceSessionID
			pending.ChatID = a.service.SurfaceChatID(request.SurfaceSessionID)
			pending.ActorUserID = a.service.SurfaceActorUserID(request.SurfaceSessionID)
			pending.SourceMessageID = request.SourceMessageID
		}
		if a.surfaceIsIdleForUpgradeLocked(request.SurfaceSessionID) {
			events := a.promptPendingUpgradeOnSurfaceLocked(request.SurfaceSessionID, stateValue, completedAt)
			if err := a.writeUpgradeStateLocked(stateValue); err != nil {
				log.Printf("upgrade check write state before prompt failed: %v", err)
			}
			return events
		}
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			log.Printf("upgrade check write state failed: %v", err)
		}
		return []control.UIEvent{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_candidate_pending", fmt.Sprintf("发现新版本 %s，但当前窗口不空闲，已记录候选升级。等当前窗口空闲后，再次发送 /upgrade latest 继续升级。", latestVersion))}
	}

	events := a.promptPendingUpgradeOnBestSurfaceLocked(stateValue, completedAt)
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		log.Printf("upgrade check write state failed: %v", err)
	}
	return events
}

func (a *App) maybeStartAutoUpgradeCheckLocked(now time.Time) {
	if a.upgradeCheckInFlight || a.upgradeStartInFlight || a.upgradeCheckInterval <= 0 {
		return
	}
	if a.upgradeNextCheckAt.IsZero() {
		nextAt := a.daemonStartedAt.Add(a.upgradeStartupDelay)
		stateValue, ok, err := a.loadUpgradeStateLocked(true)
		if err != nil {
			log.Printf("upgrade schedule load state failed: %v", err)
		} else if ok && stateValue.LastCheckAt != nil {
			candidate := stateValue.LastCheckAt.Add(a.upgradeCheckInterval)
			if candidate.After(nextAt) {
				nextAt = candidate
			}
		}
		a.upgradeNextCheckAt = nextAt
	}
	if now.Before(a.upgradeNextCheckAt) {
		return
	}

	stateValue, ok, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		log.Printf("upgrade auto-check load state failed: %v", err)
		a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
		return
	}
	if !ok || stateValue.CurrentTrack == "" || stateValue.CurrentVersion == "" {
		a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
		return
	}
	if stateValue.PendingUpgrade != nil {
		a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
		return
	}

	a.upgradeCheckInFlight = true
	a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
	go a.runUpgradeCheck(upgradeCheckRequest{Track: stateValue.CurrentTrack})
}

func (a *App) maybePromptPendingUpgradeLocked(now time.Time) []control.UIEvent {
	if a.upgradePromptScanEvery <= 0 {
		return nil
	}
	if !a.upgradeNextPromptScan.IsZero() && now.Before(a.upgradeNextPromptScan) {
		return nil
	}
	a.upgradeNextPromptScan = now.Add(a.upgradePromptScanEvery)

	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil {
		log.Printf("upgrade prompt scan load state failed: %v", err)
		return nil
	}
	if !ok || stateValue.PendingUpgrade == nil || !pendingUpgradeCandidate(stateValue.PendingUpgrade) {
		return nil
	}
	return a.promptPendingUpgradeOnBestSurfaceLocked(stateValue, now)
}

func (a *App) promptPendingUpgradeOnBestSurfaceLocked(stateValue install.InstallState, now time.Time) []control.UIEvent {
	surface := a.selectIdleSurfaceLocked("")
	if surface == nil {
		return nil
	}
	return a.promptPendingUpgradeOnSurfaceLocked(surface.SurfaceSessionID, stateValue, now)
}

func (a *App) promptPendingUpgradeOnSurfaceLocked(surfaceID string, stateValue install.InstallState, now time.Time) []control.UIEvent {
	pending := stateValue.PendingUpgrade
	if pending == nil {
		return nil
	}
	surface := a.surfaceByIDLocked(surfaceID)
	if surface == nil {
		return nil
	}
	pending.Phase = install.PendingUpgradePhasePrompted
	pending.GatewayID = surface.GatewayID
	pending.SurfaceSessionID = surface.SurfaceSessionID
	pending.ChatID = surface.ChatID
	pending.ActorUserID = surface.ActorUserID
	if pending.RequestedAt == nil {
		promptedAt := now
		pending.RequestedAt = &promptedAt
	}
	return []control.UIEvent{{
		Kind:                       control.UIEventFeishuDirectCommandCatalog,
		GatewayID:                  surface.GatewayID,
		SurfaceSessionID:           surface.SurfaceSessionID,
		FeishuDirectCommandCatalog: buildUpgradePromptCatalog(stateValue),
	}}
}

func (a *App) loadUpgradeStateLocked(create bool) (install.InstallState, bool, error) {
	path := a.installStatePath()
	statePtr, err := loadInstallStateIfPresent(path)
	if err != nil {
		return install.InstallState{}, false, err
	}
	if statePtr == nil && !create {
		return install.InstallState{}, false, nil
	}

	currentBinary, err := a.currentBinaryPath()
	if err != nil {
		return install.InstallState{}, false, err
	}

	var stateValue install.InstallState
	if statePtr != nil {
		stateValue = *statePtr
	} else {
		stateValue = install.InstallState{StatePath: path}
	}
	install.ApplyStateMetadata(&stateValue, install.StateMetadataOptions{
		StatePath:       path,
		InstalledBinary: currentBinary,
		CurrentVersion:  a.currentBinaryVersion(),
	})
	stateValue.ConfigPath = firstNonEmpty(strings.TrimSpace(stateValue.ConfigPath), strings.TrimSpace(a.serverIdentity.ConfigPath))
	stateValue.StatePath = path
	stateValue.InstalledBinary = firstNonEmpty(strings.TrimSpace(stateValue.InstalledBinary), currentBinary)
	stateValue.InstalledWrapperBinary = firstNonEmpty(strings.TrimSpace(stateValue.InstalledWrapperBinary), currentBinary)
	stateValue.InstalledRelaydBinary = firstNonEmpty(strings.TrimSpace(stateValue.InstalledRelaydBinary), currentBinary)
	stateValue.CurrentBinaryPath = firstNonEmpty(strings.TrimSpace(stateValue.CurrentBinaryPath), currentBinary)
	return stateValue, true, nil
}

func (a *App) writeUpgradeStateLocked(stateValue install.InstallState) error {
	if strings.TrimSpace(stateValue.StatePath) == "" {
		stateValue.StatePath = a.installStatePath()
	}
	return install.WriteState(stateValue.StatePath, stateValue)
}

func (a *App) selectIdleSurfaceLocked(preferredSurfaceID string) *state.SurfaceConsoleRecord {
	surfaces := a.service.Surfaces()
	candidates := make([]*state.SurfaceConsoleRecord, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil || !strings.EqualFold(strings.TrimSpace(surface.Platform), "feishu") {
			continue
		}
		if !a.surfaceIsIdleForUpgrade(surface) {
			continue
		}
		candidates = append(candidates, surface)
	}
	if len(candidates) == 0 {
		return nil
	}
	if preferredSurfaceID != "" {
		for _, surface := range candidates {
			if surface.SurfaceSessionID == preferredSurfaceID {
				return surface
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		switch {
		case !left.LastInboundAt.Equal(right.LastInboundAt):
			return left.LastInboundAt.After(right.LastInboundAt)
		default:
			return left.SurfaceSessionID < right.SurfaceSessionID
		}
	})
	return candidates[0]
}

func (a *App) surfaceByIDLocked(surfaceID string) *state.SurfaceConsoleRecord {
	for _, surface := range a.service.Surfaces() {
		if surface != nil && surface.SurfaceSessionID == surfaceID {
			return surface
		}
	}
	return nil
}

func (a *App) surfaceIsIdleForUpgradeLocked(surfaceID string) bool {
	surface := a.surfaceByIDLocked(surfaceID)
	return a.surfaceIsIdleForUpgrade(surface)
}

func (a *App) surfaceIsIdleForUpgrade(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || surface.Abandoning || surface.PendingHeadless != nil {
		return false
	}
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) > 0 {
		return false
	}
	if surface.ActiveRequestCapture != nil || len(surface.PendingRequests) > 0 {
		return false
	}
	return true
}
