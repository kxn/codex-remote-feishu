package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/upgradecontract"
)

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
	case "admin":
		return parsedDebugCommand{}, fmt.Errorf("`/debug admin` 已废弃，请改用 `/admin web`。")
	default:
		return parsedDebugCommand{}, fmt.Errorf("不支持的 /debug 子命令。")
	}
}

func parseUpgradeCommandText(text string) (parsedUpgradeCommand, error) {
	parsed, err := upgradecontract.ParseCommandText(text)
	if err == nil {
		return parsedUpgradeCommand{
			Mode:  parsed.Mode,
			Track: install.ParseReleaseTrack(string(parsed.Track)),
		}, nil
	}
	if upgradecontract.IsInvalidTrackError(err) {
		return parsedUpgradeCommand{}, err
	}
	if argument := strings.TrimSpace(control.FeishuActionArgumentText(text)); argument != "" {
		return parsedUpgradeCommand{}, fmt.Errorf("%s", upgradecontract.UsageSyntax(currentUpgradeCapabilityPolicy()))
	}
	return parsedUpgradeCommand{}, fmt.Errorf("%s", upgradecontract.UsageSummary(currentUpgradeCapabilityPolicy()))
}

func buildUpgradePromptPageView(stateValue install.InstallState) control.FeishuPageView {
	targetVersion := ""
	title := "发现可升级版本"
	confirmCommand := "/upgrade latest"
	description := "继续升级到当前 track 的最新版本。"
	summarySections := []control.FeishuCardTextSection{}
	if stateValue.PendingUpgrade != nil {
		targetVersion = firstNonEmpty(strings.TrimSpace(stateValue.PendingUpgrade.TargetSlot), strings.TrimSpace(stateValue.PendingUpgrade.TargetVersion))
		if stateValue.PendingUpgrade.Source == install.UpgradeSourceDev {
			title = "发现开发版更新"
			confirmCommand = "/upgrade dev"
			description = "继续升级到最新的 dev 构建。"
			summarySections = []control.FeishuCardTextSection{
				commandCatalogTextSection(
					"",
					"检测到新的 dev 构建可用。",
					fmt.Sprintf("当前版本：%s", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown")),
					fmt.Sprintf("目标版本：%s", firstNonEmpty(targetVersion, "unknown")),
				),
				commandCatalogTextSection("下一步", "再次发送 /upgrade dev 继续升级流程。"),
			}
		}
	}
	if len(summarySections) == 0 {
		summarySections = []control.FeishuCardTextSection{
			commandCatalogTextSection(
				"",
				fmt.Sprintf("检测到 %s track 有新版本可用。", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
				fmt.Sprintf("当前版本：%s", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown")),
				fmt.Sprintf("目标版本：%s", firstNonEmpty(targetVersion, "unknown")),
			),
			commandCatalogTextSection("下一步", "再次发送 /upgrade latest 继续升级流程。"),
		}
	}
	return control.NormalizeFeishuPageView(control.FeishuPageView{
		CommandID:       control.FeishuCommandUpgrade,
		Title:           title,
		SummarySections: summarySections,
		Interactive:     true,
		Sections: []control.CommandCatalogSection{{
			Title: "操作",
			Entries: []control.CommandCatalogEntry{{
				Commands:    []string{confirmCommand},
				Description: description,
				Buttons: []control.CommandCatalogButton{
					runCommandButton("确认升级", confirmCommand, "", false),
					runCommandButton("查看状态", "/upgrade", "", false),
				},
			}},
		}},
	})
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

func pendingUpgradeCandidateFromSource(pending *install.PendingUpgrade, source install.UpgradeSource) bool {
	if !pendingUpgradeCandidate(pending) {
		return false
	}
	return pending.Source == source
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

func clearStalePendingCandidateOnLiveVersion(stateValue *install.InstallState, liveVersion string) bool {
	if stateValue == nil || stateValue.PendingUpgrade == nil || !pendingUpgradeCandidate(stateValue.PendingUpgrade) {
		return false
	}
	pending := stateValue.PendingUpgrade
	if pending.Source != install.UpgradeSourceRelease && pending.Source != install.UpgradeSourceDev {
		return false
	}
	liveVersion = strings.TrimSpace(liveVersion)
	if liveVersion == "" {
		return false
	}
	targetVersion := firstNonEmpty(strings.TrimSpace(pending.TargetVersion), strings.TrimSpace(pending.TargetSlot))
	if targetVersion == "" || targetVersion != liveVersion {
		return false
	}
	stateValue.CurrentVersion = liveVersion
	stateValue.CurrentSlot = firstNonEmpty(strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion), liveVersion)
	if stateValue.CurrentTrack == "" && pending.Source == install.UpgradeSourceRelease {
		stateValue.CurrentTrack = firstNonEmptyTrack(pending.TargetTrack, install.ParseReleaseTrack(liveVersion))
	}
	stateValue.LastKnownLatestVersion = liveVersion
	stateValue.PendingUpgrade = nil
	return true
}

func firstNonEmptyTrack(values ...install.ReleaseTrack) install.ReleaseTrack {
	for _, value := range values {
		if strings.TrimSpace(string(value)) != "" {
			return value
		}
	}
	return ""
}

func trackSummaryEvents(surfaceID string, stateValue install.InstallState) []eventcontract.Event {
	return commandPageEvents(surfaceID, buildUpgradeTrackPageView(stateValue))
}

func (a *App) setTrackEvents(surfaceID string, stateValue install.InstallState, nextTrack install.ReleaseTrack) []eventcontract.Event {
	if !install.CurrentBuildAllowsReleaseTrack(nextTrack) {
		return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_unsupported", unsupportedTrackMessage(nextTrack))}
	}
	if a.upgradeRuntime.CheckInFlight {
		return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_busy", "当前正在检查升级，暂时不能切换 track。")}
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_busy", "当前已有升级事务进行中，暂时不能切换 track。")}
	}
	if stateValue.CurrentTrack == nextTrack {
		return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_unchanged", fmt.Sprintf("当前 track 已经是 %s。", nextTrack))}
	}
	stateValue.CurrentTrack = nextTrack
	stateValue.LastKnownLatestVersion = ""
	if clearPendingCandidateOnTrackSwitch(stateValue.PendingUpgrade, nextTrack) {
		stateValue.PendingUpgrade = nil
	}
	now := time.Now().UTC()
	a.upgradeRuntime.NextCheckAt = now.Add(a.upgradeRuntime.CheckInterval)
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_write_failed", fmt.Sprintf("切换 track 失败：%v", err))}
	}
	return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_track_updated", fmt.Sprintf("当前 track 已切到 %s。需要立即检查时，请发送 /upgrade latest。", nextTrack))}
}

func defaultUpgradeTrackForState(stateValue install.InstallState) install.ReleaseTrack {
	return install.DefaultTrackForInstallSource(stateValue.InstallSource)
}

func currentBuildTrackNames() []string {
	values := install.CurrentBuildAllowedReleaseTracks()
	names := make([]string, 0, len(values))
	for _, track := range values {
		names = append(names, string(track))
	}
	return names
}

func unsupportedTrackMessage(track install.ReleaseTrack) string {
	return fmt.Sprintf("当前构建不支持 %s track。可用 track：%s。", track, strings.Join(currentBuildTrackNames(), "、"))
}

func upgradeSubcommandUsageSummary() string {
	return upgradecontract.UsageSummary(currentUpgradeCapabilityPolicy())
}

func upgradeCommandUsageSyntax() string {
	return upgradecontract.UsageSyntax(currentUpgradeCapabilityPolicy())
}

func currentUpgradeCapabilityPolicy() upgradecontract.CapabilityPolicy {
	values := install.CurrentBuildAllowedReleaseTracks()
	tracks := make([]string, 0, len(values))
	for _, track := range values {
		tracks = append(tracks, string(track))
	}
	return upgradecontract.CapabilityPolicy{
		AllowedReleaseTracks: upgradecontract.NormalizeReleaseTracks(tracks),
		AllowDevUpgrade:      install.CurrentBuildAllowsDevUpgrade(),
		AllowLocalUpgrade:    install.CurrentBuildAllowsLocalUpgrade(),
	}
}
