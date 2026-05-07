package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	upgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/upgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type debugCommandMode string

const (
	debugCommandShowStatus debugCommandMode = "status"
)

type parsedDebugCommand struct {
	Mode debugCommandMode
}

type upgradeCommandMode = control.UpgradeCommandMode
type parsedUpgradeCommand = control.ParsedUpgradeCommand

const (
	upgradeCommandShowStatus = control.UpgradeCommandShowStatus
	upgradeCommandShowTrack  = control.UpgradeCommandShowTrack
	upgradeCommandSetTrack   = control.UpgradeCommandSetTrack
	upgradeCommandLatest     = control.UpgradeCommandLatest
	upgradeCommandCodex      = control.UpgradeCommandCodex
	upgradeCommandDev        = control.UpgradeCommandDev
	upgradeCommandLocal      = control.UpgradeCommandLocal
)

type upgradeCheckRequest struct {
	Track            install.ReleaseTrack
	Manual           bool
	GatewayID        string
	SurfaceSessionID string
	SourceMessageID  string
	FlowID           string
}

func (a *App) defaultReleaseLookup(ctx context.Context, track install.ReleaseTrack) (install.ReleaseInfo, error) {
	return install.ResolveLatestRelease(ctx, install.ReleaseLookupOptions{
		Repository:     strings.TrimSpace(os.Getenv("CODEX_REMOTE_REPO")),
		ReleasesAPIURL: strings.TrimSpace(os.Getenv("CODEX_REMOTE_RELEASES_API_URL")),
		Track:          track,
	})
}

func (a *App) handleDebugDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	parsed, err := parseDebugCommandText(command.Text)
	if err != nil {
		return debugUsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), err.Error())
	}
	switch parsed.Mode {
	case debugCommandShowStatus:
		return commandPageEvents(command.SurfaceSessionID, buildDebugRootPageView(install.InstallState{}, false, "", "", ""))
	default:
		return debugUsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), "不支持的 /debug 子命令。")
	}
}

func (a *App) handleUpgradeDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	parsed, err := parseUpgradeCommandText(command.Text)
	if err != nil {
		return upgradeUsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), err.Error())
	}

	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
	}

	switch parsed.Mode {
	case upgradeCommandShowStatus:
		return commandPageEvents(command.SurfaceSessionID, buildUpgradeRootPageView(stateValue, a.standaloneCodexUpgradeVisibleLocked(), "", "", ""))
	case upgradeCommandShowTrack:
		return trackSummaryEvents(command.SurfaceSessionID, stateValue)
	case upgradeCommandSetTrack:
		return a.setTrackEvents(command.SurfaceSessionID, stateValue, parsed.Track)
	case upgradeCommandLatest:
		return a.handleUpgradeLatestCommand(command, stateValue)
	case upgradeCommandCodex:
		return a.openCodexUpgradeOwnerFlowLocked(command)
	case upgradeCommandDev:
		if !install.CurrentBuildAllowsDevUpgrade() {
			return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_dev_unsupported", "当前构建不支持 `/upgrade dev`。如需 dev 构建升级，请使用 dev flavor 的源码构建。")}
		}
		return a.handleUpgradeDevCommand(command, stateValue)
	case upgradeCommandLocal:
		return a.handleUpgradeLocalCommand(command, stateValue)
	default:
		return upgradeUsageEvents(command.SurfaceSessionID, commandArgumentText(command.Text), "不支持的 /upgrade 子命令。")
	}
}

func (a *App) handleUpgradeLatestCommand(command control.DaemonCommand, stateValue install.InstallState) []eventcontract.Event {
	if a.upgradeRuntime.CheckInFlight {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_busy", "当前已经有一个升级检查在进行中，请稍后再试。")}
	}
	if a.upgradeRuntime.StartInFlight {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", "当前升级准备已经开始，服务会短暂重启，请稍后查看结果。")}
	}
	if clearStalePendingCandidateOnLiveVersion(&stateValue, a.currentBinaryVersion()) {
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("清理陈旧升级候选失败：%v", err))}
		}
	}
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceRelease) {
		return a.openUpgradeLatestOwnerConfirmLocked(command, stateValue)
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", fmt.Sprintf("当前升级事务处于 %s，暂时不能发起新检查。", stateValue.PendingUpgrade.Phase))}
	}
	if pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceDev) {
		if !install.CurrentBuildAllowsDevUpgrade() {
			stateValue.PendingUpgrade = nil
			if err := a.writeUpgradeStateLocked(stateValue); err != nil {
				return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_prepare_failed", fmt.Sprintf("清理不受支持的 dev 升级候选失败：%v", err))}
			}
			return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_pending_dev_unsupported", "当前构建不支持 dev 构建升级，已忽略旧的 dev 升级候选。")}
		}
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_pending_other_source", "当前已有 dev 构建升级候选，请改用 `/upgrade dev` 继续，或重新检查当前来源。")}
	}
	return a.startUpgradeLatestOwnerCheckLocked(command, stateValue)
}

func (a *App) handleUpgradeLocalCommand(command control.DaemonCommand, stateValue install.InstallState) []eventcontract.Event {
	if !install.CurrentBuildAllowsLocalUpgrade() {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_unsupported", "当前构建不支持 `/upgrade local`。如需本地升级，请使用 dev flavor 的源码构建。")}
	}
	if a.upgradeRuntime.CheckInFlight {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_busy", "当前已有 release 检查在进行中，请稍后再试本地升级。")}
	}
	if a.upgradeRuntime.StartInFlight || pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", "当前已有升级事务在进行中，请稍后再试。")}
	}
	artifactPath := install.LocalUpgradeArtifactPath(stateValue)
	if _, err := os.Stat(artifactPath); err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_artifact_missing", fmt.Sprintf("本地升级产物不存在：%s\n请先把新编译的 binary 放到这个固定路径，再发送 /upgrade local。", artifactPath))}
	}
	slot, err := install.RunLocalBinaryUpgradeWithStatePath(install.LocalBinaryUpgradeOptions{
		StatePath:    stateValue.StatePath,
		SourceBinary: artifactPath,
	})
	if err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_prepare_failed", fmt.Sprintf("准备本地升级失败：%v", err))}
	}
	return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_local_prepare_started", fmt.Sprintf("正在准备本地升级，目标 slot：%s。服务会短暂重启。", slot))}
}

func (a *App) runUpgradeCheck(request upgradeCheckRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), upgradeMetadataTimeout)
	defer cancel()

	lookup := a.upgradeRuntime.Lookup
	if lookup == nil {
		lookup = a.defaultReleaseLookup
	}
	release, err := lookup(ctx, request.Track)
	completedAt := time.Now().UTC()

	a.mu.Lock()
	defer a.mu.Unlock()

	a.upgradeRuntime.CheckInFlight = false
	a.upgradeRuntime.NextCheckAt = completedAt.Add(a.upgradeRuntime.CheckInterval)

	events := a.applyUpgradeCheckResultLocked(request, release, err, completedAt)
	if len(events) > 0 {
		a.handleUIEventsLocked(context.Background(), events)
	}
}

func (a *App) applyUpgradeCheckResultLocked(request upgradeCheckRequest, release install.ReleaseInfo, lookupErr error, completedAt time.Time) []eventcontract.Event {
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		if request.Manual {
			return []eventcontract.Event{debugNoticeEvent(request.SurfaceSessionID, "debug_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
		}
		log.Printf("upgrade check load state failed: %v", err)
		return nil
	}

	stateValue.LastCheckAt = &completedAt
	if lookupErr != nil {
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			log.Printf("upgrade check write state failed: %v", err)
		}
		if strings.TrimSpace(request.FlowID) != "" {
			return a.finishUpgradeOwnerFlowFailureLocked(request.SurfaceSessionID, request.FlowID, fmt.Sprintf("检查更新失败：%v", lookupErr))
		}
		if request.Manual {
			return []eventcontract.Event{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_check_failed", fmt.Sprintf("检查更新失败：%v", lookupErr))}
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
		if strings.TrimSpace(request.FlowID) != "" {
			return a.finishUpgradeOwnerFlowLatestLocked(request.SurfaceSessionID, request.FlowID, stateValue, latestVersion)
		}
		if request.Manual {
			return []eventcontract.Event{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_latest", fmt.Sprintf("当前已经是 %s track 的最新版本 %s。", stateValue.CurrentTrack, latestVersion))}
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
		if strings.TrimSpace(request.FlowID) != "" {
			if stateValue.PendingUpgrade != nil {
				stateValue.PendingUpgrade.Phase = install.PendingUpgradePhasePrompted
			}
			if err := a.writeUpgradeStateLocked(stateValue); err != nil {
				log.Printf("upgrade check write state before owner confirm failed: %v", err)
				return a.finishUpgradeOwnerFlowFailureLocked(request.SurfaceSessionID, request.FlowID, fmt.Sprintf("写入升级状态失败：%v", err))
			}
			return a.finishUpgradeOwnerFlowConfirmLocked(request.SurfaceSessionID, request.FlowID, stateValue)
		}
		if a.surfaceAllowsManualUpgradePromptLocked(request.SurfaceSessionID) {
			events := a.promptPendingUpgradeOnSurfaceLocked(request.SurfaceSessionID, stateValue, completedAt)
			if err := a.writeUpgradeStateLocked(stateValue); err != nil {
				log.Printf("upgrade check write state before prompt failed: %v", err)
			}
			return events
		}
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			log.Printf("upgrade check write state failed: %v", err)
		}
		return []eventcontract.Event{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_candidate_pending", fmt.Sprintf("发现新版本 %s，但当前窗口不空闲，已记录候选升级。等当前窗口空闲后，再次发送 /upgrade latest 继续升级。", latestVersion))}
	}

	events := a.promptPendingUpgradeOnBestSurfaceLocked(stateValue, completedAt)
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		log.Printf("upgrade check write state failed: %v", err)
	}
	return events
}

func (a *App) maybeStartAutoUpgradeCheckLocked(now time.Time) {
	if a.upgradeRuntime.CheckInFlight || a.upgradeRuntime.StartInFlight || a.upgradeRuntime.CheckInterval <= 0 {
		return
	}
	if a.upgradeRuntime.NextCheckAt.IsZero() {
		nextAt := a.daemonStartedAt.Add(a.upgradeRuntime.StartupDelay)
		stateValue, ok, err := a.loadUpgradeStateLocked(true)
		if err != nil {
			log.Printf("upgrade schedule load state failed: %v", err)
		} else if ok && stateValue.LastCheckAt != nil {
			candidate := stateValue.LastCheckAt.Add(a.upgradeRuntime.CheckInterval)
			if candidate.After(nextAt) {
				nextAt = candidate
			}
		}
		a.upgradeRuntime.NextCheckAt = nextAt
	}
	if now.Before(a.upgradeRuntime.NextCheckAt) {
		return
	}

	stateValue, ok, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		log.Printf("upgrade auto-check load state failed: %v", err)
		a.upgradeRuntime.NextCheckAt = now.Add(a.upgradeRuntime.CheckInterval)
		return
	}
	if !ok || stateValue.CurrentTrack == "" || stateValue.CurrentVersion == "" {
		a.upgradeRuntime.NextCheckAt = now.Add(a.upgradeRuntime.CheckInterval)
		return
	}
	if stateValue.PendingUpgrade != nil {
		a.upgradeRuntime.NextCheckAt = now.Add(a.upgradeRuntime.CheckInterval)
		return
	}

	a.upgradeRuntime.CheckInFlight = true
	a.upgradeRuntime.NextCheckAt = now.Add(a.upgradeRuntime.CheckInterval)
	go a.runUpgradeCheck(upgradeCheckRequest{Track: stateValue.CurrentTrack})
}

func (a *App) maybePromptPendingUpgradeLocked(now time.Time) []eventcontract.Event {
	if a.upgradeRuntime.PromptScanEvery <= 0 {
		return nil
	}
	if !a.upgradeRuntime.NextPromptScan.IsZero() && now.Before(a.upgradeRuntime.NextPromptScan) {
		return nil
	}
	a.upgradeRuntime.NextPromptScan = now.Add(a.upgradeRuntime.PromptScanEvery)

	stateValue, ok, err := a.loadUpgradeStateLocked(false)
	if err != nil {
		log.Printf("upgrade prompt scan load state failed: %v", err)
		return nil
	}
	if !ok || stateValue.PendingUpgrade == nil || !pendingUpgradeCandidate(stateValue.PendingUpgrade) {
		return nil
	}
	if a.activeUpgradeOwnerFlowMatchesPendingLocked(stateValue.PendingUpgrade) {
		return nil
	}
	return a.promptPendingUpgradeOnBestSurfaceLocked(stateValue, now)
}

func (a *App) promptPendingUpgradeOnBestSurfaceLocked(stateValue install.InstallState, now time.Time) []eventcontract.Event {
	surface := a.selectIdleSurfaceLocked("")
	if surface == nil {
		return nil
	}
	return a.promptPendingUpgradeOnSurfaceLocked(surface.SurfaceSessionID, stateValue, now)
}

func (a *App) promptPendingUpgradeOnSurfaceLocked(surfaceID string, stateValue install.InstallState, now time.Time) []eventcontract.Event {
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
	if pending.Source == install.UpgradeSourceRelease {
		flow := a.newUpgradeOwnerFlowLocked(surface.SurfaceSessionID, surface.ActorUserID, "", upgraderuntime.OwnerFlowStageConfirm)
		flow.Source = pending.Source
		flow.Track = stateValue.CurrentTrack
		flow.CurrentVersion = strings.TrimSpace(stateValue.CurrentVersion)
		flow.TargetVersion = pendingTargetVersion(pending)
		return []eventcontract.Event{upgradeOwnerConfirmEvent(surface.SurfaceSessionID, flow, stateValue)}
	}
	page := buildUpgradePromptPageView(stateValue)
	pageView := control.NormalizeFeishuPageView(page)
	return []eventcontract.Event{
		eventcontract.NewEventFromPayload(
			eventcontract.PagePayload{View: pageView},
			eventcontract.EventMeta{
				Target: eventcontract.TargetRef{
					GatewayID:        strings.TrimSpace(surface.GatewayID),
					SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
				},
			},
		),
	}
}

func (a *App) loadUpgradeStateLocked(create bool) (install.InstallState, bool, error) {
	path := a.installStatePath()
	configPath := strings.TrimSpace(a.serverIdentity.ConfigPath)

	a.upgradeStateIOMu.Lock()
	a.mu.Unlock()
	statePtr, err := loadInstallStateIfPresent(path)
	currentBinary, binaryErr := a.currentBinaryPath()
	a.upgradeStateIOMu.Unlock()
	a.mu.Lock()
	if err != nil {
		return install.InstallState{}, false, err
	}
	if statePtr == nil && !create {
		return install.InstallState{}, false, nil
	}
	if binaryErr != nil {
		return install.InstallState{}, false, binaryErr
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
	stateValue.ConfigPath = firstNonEmpty(strings.TrimSpace(stateValue.ConfigPath), configPath)
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
	a.upgradeStateIOMu.Lock()
	a.mu.Unlock()
	err := install.WriteState(stateValue.StatePath, stateValue)
	a.upgradeStateIOMu.Unlock()
	a.mu.Lock()
	return err
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

func (a *App) surfaceAllowsManualUpgradePromptLocked(surfaceID string) bool {
	surface := a.surfaceByIDLocked(surfaceID)
	return a.surfaceAllowsManualUpgradePrompt(surface)
}

func (a *App) surfaceIsIdleForUpgrade(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || surface.Abandoning {
		return false
	}
	if pending := surface.PendingHeadless; pending != nil {
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

func (a *App) surfaceAllowsManualUpgradePrompt(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || surface.Abandoning {
		return false
	}
	if pending := surface.PendingHeadless; pending != nil && !pending.AutoRestore {
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
