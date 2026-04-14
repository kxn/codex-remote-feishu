package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
		if len(fields) != 2 {
			return parsedDebugCommand{}, fmt.Errorf("`/debug admin` 不接受额外参数。")
		}
		return parsedDebugCommand{Mode: debugCommandAdmin}, nil
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
		case "track":
			return parsedUpgradeCommand{Mode: upgradeCommandShowTrack}, nil
		case "latest":
			return parsedUpgradeCommand{Mode: upgradeCommandLatest}, nil
		case "local":
			return parsedUpgradeCommand{Mode: upgradeCommandLocal}, nil
		default:
			return parsedUpgradeCommand{}, fmt.Errorf("%s", upgradeSubcommandUsageSummary())
		}
	case 3:
		if fields[1] != "track" {
			return parsedUpgradeCommand{}, fmt.Errorf("%s", upgradeCommandUsageSyntax())
		}
		track := install.ParseReleaseTrack(fields[2])
		if track == "" {
			return parsedUpgradeCommand{}, fmt.Errorf("track 只支持 alpha、beta、production。")
		}
		return parsedUpgradeCommand{Mode: upgradeCommandSetTrack, Track: track}, nil
	default:
		return parsedUpgradeCommand{}, fmt.Errorf("%s", upgradeCommandUsageSyntax())
	}
}

func buildDebugStatusCatalog(stateValue install.InstallState, checkInFlight bool) *control.FeishuDirectCommandCatalog {
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
	summaryLines = append(summaryLines, upgradeCheckSummaryLine(checkInFlight))
	quickButtons := []control.CommandCatalogButton{
		runCommandButton("管理页外链", "/debug admin", "", false),
		runCommandButton("升级状态", "/upgrade", "", false),
		runCommandButton("检查/继续升级", "/upgrade latest", "primary", false),
	}
	if install.CurrentBuildAllowsLocalUpgrade() {
		quickButtons = append(quickButtons, runCommandButton("本地升级", "/upgrade local", "", false))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Debug",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "快捷操作",
				Entries: []control.CommandCatalogEntry{{
					Buttons: quickButtons,
				}},
			},
			{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"/debug"},
					Description: "输入 `/debug` 后面的参数，例如 `admin`。",
					Form:        control.FeishuCommandFormWithDefault(control.FeishuCommandDebug, ""),
				}},
			},
		},
	}
}

func buildUpgradeStatusCatalog(stateValue install.InstallState, checkInFlight bool) *control.FeishuDirectCommandCatalog {
	summaryLines := []string{
		fmt.Sprintf("当前来源：%s", displayInstallSource(stateValue.InstallSource)),
		fmt.Sprintf("当前 track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
		fmt.Sprintf("当前版本：%s", firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown")),
		fmt.Sprintf("最近检查：%s", formatOptionalTime(stateValue.LastCheckAt)),
	}
	if install.CurrentBuildAllowsLocalUpgrade() {
		summaryLines = append(summaryLines, fmt.Sprintf("本地升级产物：%s", install.LocalUpgradeArtifactPath(stateValue)))
	}
	if latest := strings.TrimSpace(stateValue.LastKnownLatestVersion); latest != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("最近看到的最新版本：%s", latest))
	}
	if pending := describePendingUpgrade(stateValue.PendingUpgrade); pending != "" {
		summaryLines = append(summaryLines, "待处理升级："+pending)
	} else {
		summaryLines = append(summaryLines, "待处理升级：无")
	}
	summaryLines = append(summaryLines, upgradeCheckSummaryLine(checkInFlight))
	currentTrack := strings.TrimSpace(string(stateValue.CurrentTrack))
	quickButtons := []control.CommandCatalogButton{
		runCommandButton("查看 track", "/upgrade track", "", false),
		runCommandButton("检查/继续升级", "/upgrade latest", "primary", false),
	}
	if install.CurrentBuildAllowsLocalUpgrade() {
		quickButtons = append(quickButtons, runCommandButton("本地升级", "/upgrade local", "", false))
	}
	return &control.FeishuDirectCommandCatalog{
		Title:        "Upgrade",
		Summary:      strings.Join(summaryLines, "\n"),
		Interactive:  true,
		DisplayStyle: control.CommandCatalogDisplayCompactButtons,
		Sections: []control.CommandCatalogSection{
			{
				Title: "快捷操作",
				Entries: []control.CommandCatalogEntry{{
					Buttons: quickButtons,
				}},
			},
			{
				Title: "切换 track",
				Entries: []control.CommandCatalogEntry{{
					Buttons: buildTrackCommandButtons(currentTrack),
				}},
			},
			{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"/upgrade"},
					Description: "输入 `/upgrade` 后面的参数，例如 `track beta`、`latest` 或 `local`。",
					Form:        control.FeishuCommandFormWithDefault(control.FeishuCommandUpgrade, ""),
				}},
			},
		},
	}
}

func buildUpgradePromptCatalog(stateValue install.InstallState) *control.FeishuDirectCommandCatalog {
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
	return &control.FeishuDirectCommandCatalog{
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
					{Label: "查看状态", CommandText: "/upgrade"},
				},
			}},
		}},
	}
}

func buildTrackSummary(stateValue install.InstallState) string {
	lines := []string{
		fmt.Sprintf("当前 track：%s", firstNonEmpty(string(stateValue.CurrentTrack), "unknown")),
		fmt.Sprintf("安装来源：%s", displayInstallSource(stateValue.InstallSource)),
		fmt.Sprintf("当前构建允许的 track：%s", strings.Join(currentBuildTrackNames(), "、")),
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

func upgradeCheckSummaryLine(checkInFlight bool) string {
	if checkInFlight {
		return "升级检查：进行中"
	}
	return "升级检查：仅手动触发"
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

func clearStalePendingCandidateOnLiveVersion(stateValue *install.InstallState, liveVersion string) bool {
	if stateValue == nil || stateValue.PendingUpgrade == nil || !pendingUpgradeCandidate(stateValue.PendingUpgrade) {
		return false
	}
	pending := stateValue.PendingUpgrade
	if pending.Source != install.UpgradeSourceRelease {
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
	if stateValue.CurrentTrack == "" {
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

func trackSummaryEvents(surfaceID string, stateValue install.InstallState, legacyAlias bool) []control.UIEvent {
	text := buildTrackSummary(stateValue)
	if legacyAlias {
		text = legacyTrackAliasMessage("/upgrade track", text)
	}
	return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_status", text)}
}

func (a *App) setTrackEvents(surfaceID string, stateValue install.InstallState, nextTrack install.ReleaseTrack, legacyAlias bool) []control.UIEvent {
	if !install.CurrentBuildAllowsReleaseTrack(nextTrack) {
		return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_unsupported", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), unsupportedTrackMessage(nextTrack), legacyAlias))}
	}
	if a.upgradeCheckInFlight {
		return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_busy", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), "当前正在检查升级，暂时不能切换 track。", legacyAlias))}
	}
	if pendingUpgradeBusy(stateValue.PendingUpgrade) {
		return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_busy", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), "当前已有升级事务进行中，暂时不能切换 track。", legacyAlias))}
	}
	if stateValue.CurrentTrack == nextTrack {
		return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_unchanged", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), fmt.Sprintf("当前 track 已经是 %s。", nextTrack), legacyAlias))}
	}
	stateValue.CurrentTrack = nextTrack
	stateValue.LastKnownLatestVersion = ""
	if clearPendingCandidateOnTrackSwitch(stateValue.PendingUpgrade, nextTrack) {
		stateValue.PendingUpgrade = nil
	}
	now := time.Now().UTC()
	a.upgradeNextCheckAt = now.Add(a.upgradeCheckInterval)
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_write_failed", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), fmt.Sprintf("切换 track 失败：%v", err), legacyAlias))}
	}
	return []control.UIEvent{upgradeNoticeEvent(surfaceID, "upgrade_track_updated", legacyTrackAliasMessage("/upgrade track "+string(nextTrack), fmt.Sprintf("当前 track 已切到 %s。需要立即检查时，请发送 /upgrade latest。", nextTrack), legacyAlias))}
}

func legacyTrackAliasMessage(replacement, body string, legacyAlias ...bool) string {
	if len(legacyAlias) == 0 || !legacyAlias[0] {
		return body
	}
	return fmt.Sprintf("`/debug track` 仍可兼容，后续请改用 `%s`。\n\n%s", replacement, body)
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

func buildTrackCommandButtons(currentTrack string) []control.CommandCatalogButton {
	allowed := install.CurrentBuildAllowedReleaseTracks()
	buttons := make([]control.CommandCatalogButton, 0, len(allowed))
	for i, track := range allowed {
		style := ""
		if i == 0 {
			style = "primary"
		}
		buttons = append(buttons, runCommandButton(string(track), "/upgrade track "+string(track), style, currentTrack == string(track)))
	}
	return buttons
}

func unsupportedTrackMessage(track install.ReleaseTrack) string {
	return fmt.Sprintf("当前构建不支持 %s track。可用 track：%s。", track, strings.Join(currentBuildTrackNames(), "、"))
}

func upgradeSubcommandUsageSummary() string {
	parts := []string{"`track`", "`latest`"}
	if install.CurrentBuildAllowsLocalUpgrade() {
		parts = append(parts, "`local`")
	}
	return fmt.Sprintf("`/upgrade` 只支持 %s。", strings.Join(parts, "、"))
}

func upgradeCommandUsageSyntax() string {
	segments := []string{"/upgrade"}
	allowed := currentBuildTrackNames()
	if len(allowed) > 0 {
		segments = append(segments, fmt.Sprintf("`/upgrade track [%s]`", strings.Join(allowed, "|")))
	}
	segments = append(segments, "`/upgrade latest`")
	if install.CurrentBuildAllowsLocalUpgrade() {
		segments = append(segments, "`/upgrade local`")
	}
	return fmt.Sprintf("`/upgrade` 只支持 %s。", strings.Join(segments, "、"))
}
