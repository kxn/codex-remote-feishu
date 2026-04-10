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
	debugCommandUpgrade    debugCommandMode = "upgrade"
)

type parsedDebugCommand struct {
	Mode  debugCommandMode
	Track install.ReleaseTrack
}

type upgradeCommandMode string

const (
	upgradeCommandShowStatus upgradeCommandMode = "status"
	upgradeCommandLatest     upgradeCommandMode = "latest"
	upgradeCommandLocal      upgradeCommandMode = "local"
)

type parsedUpgradeCommand struct {
	Mode upgradeCommandMode
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

	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_state_load_failed", fmt.Sprintf("读取升级状态失败：%v", err))}
	}

	switch parsed.Mode {
	case debugCommandShowStatus:
		return []control.UIEvent{{
			Kind:             control.UIEventCommandCatalog,
			SurfaceSessionID: command.SurfaceSessionID,
			CommandCatalog:   buildDebugStatusCatalog(stateValue, a.upgradeCheckInFlight),
		}}
	case debugCommandShowTrack:
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_status", buildTrackSummary(stateValue))}
	case debugCommandSetTrack:
		if a.upgradeCheckInFlight {
			return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_busy", "当前正在检查升级，暂时不能切换 track。")}
		}
		if pendingUpgradeBusy(stateValue.PendingUpgrade) {
			return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_busy", "当前已有升级事务进行中，暂时不能切换 track。")}
		}
		if stateValue.CurrentTrack == parsed.Track {
			return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_unchanged", fmt.Sprintf("当前 track 已经是 %s。", parsed.Track))}
		}
		stateValue.CurrentTrack = parsed.Track
		stateValue.LastKnownLatestVersion = ""
		if clearPendingCandidateOnTrackSwitch(stateValue.PendingUpgrade, parsed.Track) {
			stateValue.PendingUpgrade = nil
		}
		now := time.Now().UTC()
		a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
		if err := a.writeUpgradeStateLocked(stateValue); err != nil {
			return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_write_failed", fmt.Sprintf("切换 track 失败：%v", err))}
		}
		return []control.UIEvent{debugNoticeEvent(command.SurfaceSessionID, "debug_track_updated", fmt.Sprintf("当前 track 已切到 %s。需要立即检查时，请发送 /upgrade latest。", parsed.Track))}
	case debugCommandUpgrade:
		command.Text = "/upgrade latest"
		return a.handleUpgradeLatestCommand(command, stateValue)
	default:
		return debugUsageEvents(command.SurfaceSessionID, "不支持的 /debug 子命令。")
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
			Kind:             control.UIEventCommandCatalog,
			SurfaceSessionID: command.SurfaceSessionID,
			CommandCatalog:   buildUpgradeStatusCatalog(stateValue, a.upgradeCheckInFlight),
		}}
	case upgradeCommandLatest:
		return a.handleUpgradeLatestCommand(command, stateValue)
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
	if stateValue.PendingUpgrade != nil && pendingUpgradeCandidate(stateValue.PendingUpgrade) {
		return a.beginPendingUpgradeLocked(command, stateValue)
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []control.UIEvent{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_busy", fmt.Sprintf("当前升级事务处于 %s，暂时不能发起新检查。", stateValue.PendingUpgrade.Phase))}
	}
	track := stateValue.CurrentTrack
	if track == "" {
		track = install.ReleaseTrackAlpha
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
		HelperBinary: stateValue.CurrentBinaryPath,
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
		a.handleUIEvents(context.Background(), events)
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
		return []control.UIEvent{debugNoticeEvent(request.SurfaceSessionID, "debug_upgrade_candidate_pending", fmt.Sprintf("发现新版本 %s，但当前窗口不空闲，已记录候选升级，待空闲 surface 再提示。", latestVersion))}
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
		Kind:             control.UIEventCommandCatalog,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		CommandCatalog:   buildUpgradePromptCatalog(stateValue),
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

func parseDebugCommandText(text string) (parsedDebugCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return parsedDebugCommand{}, fmt.Errorf("缺少 /debug 子命令。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/debug" {
		return parsedDebugCommand{}, fmt.Errorf("不支持的 /debug 子命令。")
	}
	if len(fields) == 1 {
		return parsedDebugCommand{Mode: debugCommandShowStatus}, nil
	}
	switch fields[1] {
	case "upgrade":
		if len(fields) != 2 {
			return parsedDebugCommand{}, fmt.Errorf("`/debug upgrade` 不接受额外参数。")
		}
		return parsedDebugCommand{Mode: debugCommandUpgrade}, nil
	case "track":
		switch len(fields) {
		case 2:
			return parsedDebugCommand{Mode: debugCommandShowTrack}, nil
		case 3:
			track := install.ParseReleaseTrack(fields[2])
			if track == "" {
				return parsedDebugCommand{}, fmt.Errorf("track 只支持 alpha、beta、production。")
			}
			return parsedDebugCommand{Mode: debugCommandSetTrack, Track: track}, nil
		default:
			return parsedDebugCommand{}, fmt.Errorf("`/debug track` 只支持查看或设置 `alpha|beta|production`。")
		}
	default:
		return parsedDebugCommand{}, fmt.Errorf("不支持的 /debug 子命令。")
	}
}

func parseUpgradeCommandText(text string) (parsedUpgradeCommand, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return parsedUpgradeCommand{}, fmt.Errorf("缺少 /upgrade 子命令。")
	}
	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || fields[0] != "/upgrade" {
		return parsedUpgradeCommand{}, fmt.Errorf("不支持的 /upgrade 子命令。")
	}
	switch len(fields) {
	case 1:
		return parsedUpgradeCommand{Mode: upgradeCommandShowStatus}, nil
	case 2:
		switch fields[1] {
		case "latest":
			return parsedUpgradeCommand{Mode: upgradeCommandLatest}, nil
		case "local":
			return parsedUpgradeCommand{Mode: upgradeCommandLocal}, nil
		default:
			return parsedUpgradeCommand{}, fmt.Errorf("`/upgrade` 只支持 `latest` 或 `local`。")
		}
	default:
		return parsedUpgradeCommand{}, fmt.Errorf("`/upgrade` 只支持 `/upgrade`、`/upgrade latest`、`/upgrade local`。")
	}
}

func buildDebugStatusCatalog(stateValue install.InstallState, checkInFlight bool) *control.CommandCatalog {
	summaryLines := []string{
		fmt.Sprintf("当前来源：%s", displayInstallSource(stateValue.InstallSource)),
		fmt.Sprintf("当前 track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
		fmt.Sprintf("当前版本：%s", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown")),
		fmt.Sprintf("最近检查：%s", formatOptionalTime(stateValue.LastCheckAt)),
	}
	if latest := strings.TrimSpace(stateValue.LastKnownLatestVersion); latest != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("最近看到的最新版本：%s", latest))
	}
	if pending := describePendingUpgrade(stateValue.PendingUpgrade); pending != "" {
		summaryLines = append(summaryLines, "待处理升级："+pending)
	} else {
		summaryLines = append(summaryLines, "待处理升级：无")
	}
	if checkInFlight {
		summaryLines = append(summaryLines, "后台检查：进行中")
	} else {
		summaryLines = append(summaryLines, "后台检查：空闲")
	}
	return &control.CommandCatalog{
		Title:       "Debug / Upgrade",
		Summary:     strings.Join(summaryLines, "\n"),
		Interactive: false,
		Sections: []control.CommandCatalogSection{{
			Title: "命令",
			Entries: []control.CommandCatalogEntry{
				{Commands: []string{"/debug"}, Description: "查看当前升级状态和可用子命令。"},
				{Commands: []string{"/upgrade"}, Description: "查看正式升级入口和当前状态。"},
				{Commands: []string{"/upgrade latest"}, Description: "立即按当前 track 检查或继续升级到最新 release。"},
				{Commands: []string{"/upgrade local"}, Description: "使用固定本地 artifact 发起升级。"},
				{Commands: []string{"/debug track"}, Description: "查看当前 track。"},
				{Commands: []string{"/debug track alpha|beta|production"}, Description: "切换后续检查目标，不自动升级。"},
			},
		}},
	}
}

func buildUpgradeStatusCatalog(stateValue install.InstallState, checkInFlight bool) *control.CommandCatalog {
	summaryLines := []string{
		fmt.Sprintf("当前来源：%s", displayInstallSource(stateValue.InstallSource)),
		fmt.Sprintf("当前 track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
		fmt.Sprintf("当前版本：%s", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown")),
		fmt.Sprintf("最近检查：%s", formatOptionalTime(stateValue.LastCheckAt)),
		fmt.Sprintf("本地升级产物：%s", install.LocalUpgradeArtifactPath(stateValue)),
	}
	if latest := strings.TrimSpace(stateValue.LastKnownLatestVersion); latest != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("最近看到的最新版本：%s", latest))
	}
	if pending := describePendingUpgrade(stateValue.PendingUpgrade); pending != "" {
		summaryLines = append(summaryLines, "待处理升级："+pending)
	} else {
		summaryLines = append(summaryLines, "待处理升级：无")
	}
	if checkInFlight {
		summaryLines = append(summaryLines, "后台检查：进行中")
	} else {
		summaryLines = append(summaryLines, "后台检查：空闲")
	}
	return &control.CommandCatalog{
		Title:       "Upgrade",
		Summary:     strings.Join(summaryLines, "\n"),
		Interactive: false,
		Sections: []control.CommandCatalogSection{{
			Title: "命令",
			Entries: []control.CommandCatalogEntry{
				{Commands: []string{"/upgrade"}, Description: "查看当前升级状态和固定本地 artifact 路径。"},
				{Commands: []string{"/upgrade latest"}, Description: "检查或继续升级到当前 track 的最新 release。"},
				{Commands: []string{"/upgrade local"}, Description: "使用固定本地 artifact 发起升级。"},
				{Commands: []string{"/debug track"}, Description: "查看当前 track。"},
				{Commands: []string{"/debug track alpha|beta|production"}, Description: "切换 release track，不自动升级。"},
			},
		}},
	}
}

func buildUpgradePromptCatalog(stateValue install.InstallState) *control.CommandCatalog {
	targetVersion := ""
	if stateValue.PendingUpgrade != nil {
		targetVersion = firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))
	}
	summary := fmt.Sprintf(
		"检测到 %s track 有新版本可用。\n当前版本：%s\n目标版本：%s\n\n再次发送 /upgrade latest 继续升级流程。",
		firstNonEmpty(string(stateValue.CurrentTrack), "unknown"),
		firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown"),
		firstNonEmpty(targetVersion, "unknown"),
	)
	return &control.CommandCatalog{
		Title:       "发现可升级版本",
		Summary:     summary,
		Interactive: true,
		Sections: []control.CommandCatalogSection{{
			Title: "操作",
			Entries: []control.CommandCatalogEntry{{
				Commands:    []string{"/upgrade latest"},
				Description: "继续升级到当前 track 的最新版本。",
				Buttons: []control.CommandCatalogButton{
					{Label: "确认升级", CommandText: "/upgrade latest"},
					{Label: "查看状态", CommandText: "/debug"},
				},
			}},
		}},
	}
}

func buildTrackSummary(stateValue install.InstallState) string {
	lines := []string{
		fmt.Sprintf("当前 track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
		fmt.Sprintf("安装来源：%s", displayInstallSource(stateValue.InstallSource)),
	}
	if latest := strings.TrimSpace(stateValue.LastKnownLatestVersion); latest != "" {
		lines = append(lines, fmt.Sprintf("最近看到的最新版本：%s", latest))
	}
	lines = append(lines, "切换 track 不会自动触发升级；需要立即检查时请发送 /upgrade latest。")
	return strings.Join(lines, "\n")
}

func describePendingUpgrade(pending *install.PendingUpgrade) string {
	if pending == nil {
		return ""
	}
	version := strings.TrimSpace(pending.TargetVersion)
	if version == "" {
		version = "unknown"
	}
	phase := strings.TrimSpace(pending.Phase)
	if phase == "" {
		phase = "unknown"
	}
	source := strings.TrimSpace(string(pending.Source))
	target := firstNonEmpty(strings.TrimSpace(pending.TargetSlot), version)
	if source == "" {
		return fmt.Sprintf("%s (%s)", target, phase)
	}
	return fmt.Sprintf("%s/%s (%s)", source, target, phase)
}

func displayInstallSource(source install.InstallSource) string {
	switch source {
	case install.InstallSourceRelease:
		return "release"
	case install.InstallSourceRepo:
		return "repo"
	default:
		return "unknown"
	}
}

func formatOptionalTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return "从未检查"
	}
	return value.UTC().Format(time.RFC3339)
}

func pendingUpgradeBusy(pending *install.PendingUpgrade) bool {
	if pending == nil {
		return false
	}
	switch strings.TrimSpace(pending.Phase) {
	case install.PendingUpgradePhasePrepared,
		install.PendingUpgradePhaseSwitching,
		install.PendingUpgradePhaseObserving:
		return true
	default:
		return false
	}
}

func pendingUpgradeCandidate(pending *install.PendingUpgrade) bool {
	if pending == nil {
		return false
	}
	switch strings.TrimSpace(pending.Phase) {
	case install.PendingUpgradePhaseAvailable, install.PendingUpgradePhasePrompted:
		return true
	default:
		return false
	}
}

func clearPendingCandidateOnTrackSwitch(pending *install.PendingUpgrade, nextTrack install.ReleaseTrack) bool {
	if pending == nil {
		return false
	}
	if pendingUpgradeBusy(pending) {
		return false
	}
	return pending.TargetTrack != nextTrack || pendingUpgradeCandidate(pending)
}

func debugUsageEvents(surfaceID, message string) []control.UIEvent {
	events := []control.UIEvent{}
	if strings.TrimSpace(message) != "" {
		events = append(events, debugNoticeEvent(surfaceID, "debug_usage_error", message))
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventCommandCatalog,
		SurfaceSessionID: surfaceID,
		CommandCatalog: &control.CommandCatalog{
			Title:       "Debug / Upgrade",
			Summary:     "支持的命令：/debug、/debug track、/debug track alpha|beta|production、/upgrade、/upgrade latest、/upgrade local",
			Interactive: false,
		},
	})
	return events
}

func upgradeUsageEvents(surfaceID, message string) []control.UIEvent {
	events := []control.UIEvent{}
	if strings.TrimSpace(message) != "" {
		events = append(events, upgradeNoticeEvent(surfaceID, "upgrade_usage_error", message))
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventCommandCatalog,
		SurfaceSessionID: surfaceID,
		CommandCatalog: &control.CommandCatalog{
			Title:       "Upgrade",
			Summary:     "支持的命令：/upgrade、/upgrade latest、/upgrade local；track 维护命令仍使用 /debug track。",
			Interactive: false,
		},
	})
	return events
}

func debugNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Debug",
			Text:  text,
		},
	}
}

func upgradeNoticeEvent(surfaceID, code, text string) control.UIEvent {
	return control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surfaceID,
		Notice: &control.Notice{
			Code:  code,
			Title: "Upgrade",
			Text:  text,
		},
	}
}
