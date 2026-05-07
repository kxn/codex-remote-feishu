package daemon

import (
	"context"
	"fmt"
	"strings"
	"time"

	codexupgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/codexupgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

const (
	codexUpgradeOwnerFlowTTL    = 15 * time.Minute
	codexUpgradeOwnerFlowPrefix = "codex-upgrade-owner-"
)

func isCodexUpgradeOwnerFlowID(flowID string) bool {
	return strings.HasPrefix(strings.TrimSpace(flowID), codexUpgradeOwnerFlowPrefix)
}

func (a *App) nextCodexUpgradeOwnerFlowIDLocked() string {
	a.codexUpgradeRuntime.NextFlowSeq++
	return fmt.Sprintf("%s%d", codexUpgradeOwnerFlowPrefix, a.codexUpgradeRuntime.NextFlowSeq)
}

func (a *App) activeCodexUpgradeOwnerFlowLocked() *codexupgraderuntime.OwnerCardFlowRecord {
	flow := a.codexUpgradeRuntime.ActiveFlow
	if flow == nil {
		return nil
	}
	if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(time.Now().UTC()) {
		a.clearCodexUpgradeOwnerFlowLocked()
		return nil
	}
	return flow
}

func (a *App) newCodexUpgradeOwnerFlowLocked(surfaceID, ownerUserID, messageID string) *codexupgraderuntime.OwnerCardFlowRecord {
	now := time.Now().UTC()
	flow := &codexupgraderuntime.OwnerCardFlowRecord{
		FlowID:           a.nextCodexUpgradeOwnerFlowIDLocked(),
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		OwnerUserID:      strings.TrimSpace(ownerUserID),
		MessageID:        strings.TrimSpace(messageID),
		Stage:            codexupgraderuntime.OwnerFlowStageOpen,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(codexUpgradeOwnerFlowTTL),
	}
	a.codexUpgradeRuntime.ActiveFlow = flow
	return flow
}

func (a *App) refreshCodexUpgradeOwnerFlowLocked(flow *codexupgraderuntime.OwnerCardFlowRecord, stage codexupgraderuntime.OwnerCardFlowStage) {
	if flow == nil {
		return
	}
	now := time.Now().UTC()
	flow.Stage = stage
	flow.UpdatedAt = now
	flow.ExpiresAt = now.Add(codexUpgradeOwnerFlowTTL)
}

func (a *App) clearCodexUpgradeOwnerFlowLocked() {
	a.codexUpgradeRuntime.ActiveFlow = nil
}

func (a *App) recordCodexUpgradeOwnerCardMessageLocked(trackingKey, messageID string) {
	flow := a.activeCodexUpgradeOwnerFlowLocked()
	if flow == nil {
		return
	}
	if strings.TrimSpace(flow.FlowID) != strings.TrimSpace(trackingKey) {
		return
	}
	flow.MessageID = strings.TrimSpace(messageID)
}

func (a *App) requireCodexUpgradeOwnerFlowLocked(surfaceID, flowID, actorUserID string) (*codexupgraderuntime.OwnerCardFlowRecord, []eventcontract.Event) {
	flow := a.activeCodexUpgradeOwnerFlowLocked()
	if flow == nil || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(flowID) {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "codex_upgrade_owner_expired", "这张 Codex 升级卡片已失效，请重新发送 `/upgrade codex`。")}
	}
	if strings.TrimSpace(flow.SurfaceSessionID) != "" && strings.TrimSpace(flow.SurfaceSessionID) != strings.TrimSpace(surfaceID) {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "codex_upgrade_owner_expired", "这张 Codex 升级卡片已失效，请重新发送 `/upgrade codex`。")}
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, a.service.SurfaceActorUserID(surfaceID)))
	if owner := strings.TrimSpace(flow.OwnerUserID); owner != "" && actorUserID != "" && owner != actorUserID {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "codex_upgrade_owner_unauthorized", "这张 Codex 升级卡片只允许发起者本人操作。")}
	}
	return flow, nil
}

func codexUpgradeOwnerButton(label, flowID, optionID, style string) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label:         label,
		Kind:          control.CommandCatalogButtonCallbackAction,
		CallbackValue: frontstagecontract.ActionPayloadUpgradeOwnerFlow(flowID, optionID),
		Style:         style,
	}
}

func codexUpgradeOwnerFlowTrackingKey(flow *codexupgraderuntime.OwnerCardFlowRecord) string {
	if flow == nil || strings.TrimSpace(flow.MessageID) != "" {
		return ""
	}
	return strings.TrimSpace(flow.FlowID)
}

func codexUpgradeOwnerFlowMessageID(flow *codexupgraderuntime.OwnerCardFlowRecord) string {
	if flow == nil {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}

func codexUpgradeOwnerCardEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord, title, theme string, bodySections, noticeSections []control.FeishuCardTextSection, buttons []control.CommandCatalogButton, sealed bool) eventcontract.Event {
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:          strings.TrimSpace(title),
		MessageID:      codexUpgradeOwnerFlowMessageID(flow),
		TrackingKey:    codexUpgradeOwnerFlowTrackingKey(flow),
		ThemeKey:       strings.TrimSpace(theme),
		Patchable:      true,
		BodySections:   append([]control.FeishuCardTextSection(nil), bodySections...),
		NoticeSections: append([]control.FeishuCardTextSection(nil), noticeSections...),
		Interactive:    len(buttons) > 0 && !sealed,
		Sealed:         sealed,
		RelatedButtons: append([]control.CommandCatalogButton(nil), buttons...),
	})
	return surfacePagePayloadEvent(surfaceID, eventcontract.PagePayload{View: view}, false)
}

func codexUpgradeOwnerContextSections(flow *codexupgraderuntime.OwnerCardFlowRecord) []control.FeishuCardTextSection {
	if flow == nil {
		return nil
	}
	sections := make([]control.FeishuCardTextSection, 0, 2)
	if current := strings.TrimSpace(flow.CurrentVersion); current != "" {
		sections = append(sections, commandCatalogTextSection("当前版本", current))
	}
	switch {
	case flow.HasUpdate && strings.TrimSpace(flow.TargetVersion) != "":
		sections = append(sections, commandCatalogTextSection("目标版本", strings.TrimSpace(flow.TargetVersion)))
	case flow.Checked && strings.TrimSpace(flow.LatestVersion) != "":
		sections = append(sections, commandCatalogTextSection("最新版本", strings.TrimSpace(flow.LatestVersion)))
	}
	return sections
}

func codexUpgradeOwnerNoticeSections(lines ...string) []control.FeishuCardTextSection {
	return commandCatalogSummarySections(lines...)
}

func codexUpgradeBusyNoticeLines(check codexUpgradeCheckResult) []string {
	if len(check.BusyReasons) == 0 {
		return nil
	}
	return []string{
		"当前还有其他窗口在运行或待处理，暂时不能开始升级。",
		"等其他窗口空闲后，再点一次“再次检查”。",
	}
}

func codexUpgradeOwnerReadyButtons(flow *codexupgraderuntime.OwnerCardFlowRecord) []control.CommandCatalogButton {
	if flow == nil {
		return nil
	}
	buttons := []control.CommandCatalogButton{}
	if flow.HasUpdate && flow.CanUpgrade {
		buttons = append(buttons, codexUpgradeOwnerButton("确认升级", flow.FlowID, upgradeOwnerActionConfirm, "primary"))
	}
	label := "检查更新"
	if flow.Checked {
		label = "再次检查"
	}
	buttons = append(buttons, codexUpgradeOwnerButton(label, flow.FlowID, upgradeOwnerActionCheck, "default"))
	return buttons
}

func codexUpgradeOwnerOpenEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord) eventcontract.Event {
	return codexUpgradeOwnerCardEvent(
		surfaceID,
		flow,
		"Codex 升级",
		"",
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections(
			"这里不会自动检查更新。",
			"点击“检查更新”时，才会去确认 npm 上的最新版本。",
		),
		codexUpgradeOwnerReadyButtons(flow),
		false,
	)
}

func codexUpgradeOwnerCheckingEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord) eventcontract.Event {
	return codexUpgradeOwnerCardEvent(
		surfaceID,
		flow,
		"正在检查 Codex 更新",
		"progress",
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections("正在确认当前版本、最新版本和是否可以开始升级。"),
		nil,
		false,
	)
}

func codexUpgradeOwnerStableEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord, extraLines ...string) eventcontract.Event {
	title := "Codex 升级"
	theme := ""
	lines := make([]string, 0, len(extraLines)+3)
	for _, line := range extraLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	switch {
	case !flow.Checked:
		lines = append(lines, "这里不会自动检查更新。需要时可以继续点“检查更新”。")
	case !flow.HasUpdate:
		title = "Codex 已是最新版本"
		theme = "success"
		lines = append(lines, fmt.Sprintf("当前已经是最新版本 %s。", firstNonEmpty(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(flow.LatestVersion), "unknown")))
		lines = append(lines, "如果还想再确认一次，可以继续点“再次检查”。")
	case flow.CanUpgrade:
		title = "发现可升级版本"
		theme = "approval"
		lines = append(lines, "确认升级前会再次校验是否仍然可以开始。")
	case flow.HasUpdate:
		title = "发现新版本，但暂时不能升级"
		if len(lines) == 0 {
			lines = append(lines,
				"当前还有其他窗口在运行或待处理，暂时不能开始升级。",
				"等其他窗口空闲后，再点一次“再次检查”。",
			)
		}
	default:
		lines = append(lines, "当前还不能开始升级。")
	}
	return codexUpgradeOwnerCardEvent(
		surfaceID,
		flow,
		title,
		theme,
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections(lines...),
		codexUpgradeOwnerReadyButtons(flow),
		false,
	)
}

func codexUpgradeOwnerRunningEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord) eventcontract.Event {
	return codexUpgradeOwnerCardEvent(
		surfaceID,
		flow,
		"正在升级 Codex",
		"progress",
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections(
			"正在安装目标版本，并准备重启正在运行的 child Codex。",
			"普通输入已暂停，完成后会自动恢复。",
		),
		nil,
		false,
	)
}

func codexUpgradeOwnerTerminalEvent(surfaceID string, flow *codexupgraderuntime.OwnerCardFlowRecord, title, theme string, extraLines ...string) eventcontract.Event {
	lines := make([]string, 0, len(extraLines))
	for _, line := range extraLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return codexUpgradeOwnerCardEvent(
		surfaceID,
		flow,
		title,
		theme,
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections(lines...),
		nil,
		true,
	)
}

func (a *App) standaloneCodexUpgradeVisibleLocked() bool {
	installation, err := a.inspectStandaloneCodexInstallation(context.Background())
	return err == nil && installation.Upgradeable()
}

func (a *App) openCodexUpgradeOwnerFlowLocked(command control.DaemonCommand) []eventcontract.Event {
	if tx := a.codexUpgradeRuntime.Active; tx != nil {
		if strings.TrimSpace(tx.InitiatorSurface) == strings.TrimSpace(command.SurfaceSessionID) {
			return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "codex_upgrade_running", "当前 Codex 升级已经在进行中，请查看现有升级卡片。")}
		}
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "codex_upgrade_running", "当前正在升级 Codex。只有发起升级的窗口会继续显示进度。")}
	}
	installation, err := a.inspectStandaloneCodexInstallation(context.Background())
	if err != nil {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "codex_upgrade_detect_failed", fmt.Sprintf("读取 Codex 安装状态失败：%v", err))}
	}
	if !installation.Upgradeable() {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "codex_upgrade_unavailable", "当前 Codex 不是可在这里升级的独立安装，因此不会显示这个升级入口。")}
	}
	messageID := ""
	if command.FromCardAction {
		messageID = command.SourceMessageID
	}
	flow := a.newCodexUpgradeOwnerFlowLocked(command.SurfaceSessionID, a.service.SurfaceActorUserID(command.SurfaceSessionID), messageID)
	flow.CurrentVersion = installation.CurrentVersion()
	return []eventcontract.Event{codexUpgradeOwnerOpenEvent(command.SurfaceSessionID, flow)}
}

func (a *App) startCodexUpgradeOwnerCheckLocked(command control.DaemonCommand) []eventcontract.Event {
	flow, blocked := a.requireCodexUpgradeOwnerFlowLocked(command.SurfaceSessionID, command.PickerID, a.service.SurfaceActorUserID(command.SurfaceSessionID))
	if blocked != nil {
		return blocked
	}
	a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageChecking)
	go a.runCodexUpgradeOwnerCheck(flow.FlowID, command.SurfaceSessionID)
	return []eventcontract.Event{codexUpgradeOwnerCheckingEvent(command.SurfaceSessionID, flow)}
}

func (a *App) runCodexUpgradeOwnerCheck(flowID, surfaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), codexUpgradeCheckTimeout)
	defer cancel()

	check, err := a.checkStandaloneCodexUpgrade(ctx, surfaceID)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	var events []eventcontract.Event
	if err != nil {
		events = a.finishCodexUpgradeOwnerCheckLocked(surfaceID, flowID, codexUpgradeCheckResult{}, fmt.Sprintf("检查更新失败：%v", err))
	} else {
		events = a.finishCodexUpgradeOwnerCheckLocked(surfaceID, flowID, check, "")
	}
	if len(events) != 0 {
		a.handleUIEventsLocked(context.Background(), events)
	}
}

func (a *App) finishCodexUpgradeOwnerCheckLocked(surfaceID, flowID string, check codexUpgradeCheckResult, extraLine string) []eventcontract.Event {
	flow, blocked := a.requireCodexUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return blocked
	}
	a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageReady)
	if check.Installation.Upgradeable() {
		flow.CurrentVersion = firstNonEmpty(strings.TrimSpace(check.CurrentVersion), strings.TrimSpace(check.Installation.CurrentVersion()))
	}
	flow.LatestVersion = strings.TrimSpace(check.LatestVersion)
	flow.TargetVersion = strings.TrimSpace(check.LatestVersion)
	flow.Checked = check.Installation.Upgradeable()
	flow.HasUpdate = check.HasUpdate
	flow.CanUpgrade = check.CanUpgrade
	if !flow.Checked {
		flow.Stage = codexupgraderuntime.OwnerFlowStageOpen
	}
	lines := []string{}
	if strings.TrimSpace(extraLine) != "" {
		lines = append(lines, strings.TrimSpace(extraLine))
	}
	if check.Installation.BundleBacked() {
		lines = append(lines, "当前 Codex 由 VS Code bundle 提供，不走这里的独立升级流程。")
		flow.Checked = false
		flow.HasUpdate = false
		flow.CanUpgrade = false
		flow.LatestVersion = ""
		flow.TargetVersion = ""
	}
	if !check.Installation.Upgradeable() && !check.Installation.BundleBacked() && strings.TrimSpace(check.Installation.Problem) != "" {
		lines = append(lines, fmt.Sprintf("当前 Codex 不支持这里升级：%s", strings.TrimSpace(check.Installation.Problem)))
		flow.Checked = false
		flow.HasUpdate = false
		flow.CanUpgrade = false
		flow.LatestVersion = ""
		flow.TargetVersion = ""
	}
	if flow.HasUpdate && !flow.CanUpgrade {
		lines = append(lines, codexUpgradeBusyNoticeLines(check)...)
	}
	return []eventcontract.Event{codexUpgradeOwnerStableEvent(surfaceID, flow, lines...)}
}

func (a *App) confirmCodexUpgradeOwnerFlowLocked(command control.DaemonCommand) []eventcontract.Event {
	flow, blocked := a.requireCodexUpgradeOwnerFlowLocked(command.SurfaceSessionID, command.PickerID, a.service.SurfaceActorUserID(command.SurfaceSessionID))
	if blocked != nil {
		return blocked
	}
	if flow.Stage != codexupgraderuntime.OwnerFlowStageReady || !flow.Checked || !flow.HasUpdate || !flow.CanUpgrade {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "codex_upgrade_owner_invalid_stage", "当前这张 Codex 升级卡片还不在可确认状态，请先重新检查。")}
	}
	a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageChecking)
	go a.runCodexUpgradeOwnerConfirm(flow.FlowID, command.SurfaceSessionID)
	return []eventcontract.Event{codexUpgradeOwnerCardEvent(
		command.SurfaceSessionID,
		flow,
		"正在确认升级条件",
		"progress",
		codexUpgradeOwnerContextSections(flow),
		codexUpgradeOwnerNoticeSections("正在做最后一次校验，确认版本和窗口状态都还允许开始升级。"),
		nil,
		false,
	)}
}

func (a *App) runCodexUpgradeOwnerConfirm(flowID, surfaceID string) {
	a.mu.Lock()
	flow := a.activeCodexUpgradeOwnerFlowLocked()
	targetVersion := ""
	actorUserID := ""
	if flow != nil && strings.TrimSpace(flow.FlowID) == strings.TrimSpace(flowID) {
		targetVersion = flow.TargetVersion
		actorUserID = flow.OwnerUserID
	}
	a.mu.Unlock()

	err := a.startStandaloneCodexUpgrade(context.Background(), codexUpgradeStartRequest{
		SurfaceSessionID: surfaceID,
		ActorUserID:      actorUserID,
		TargetVersion:    targetVersion,
		OnComplete: func(err error) {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.shuttingDown {
				return
			}
			events := a.finishCodexUpgradeOwnerRunLocked(surfaceID, flowID, err)
			if len(events) != 0 {
				a.handleUIEventsLocked(context.Background(), events)
			}
		},
	})

	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), codexUpgradeCheckTimeout)
		check, checkErr := a.checkStandaloneCodexUpgrade(ctx, surfaceID)
		cancel()
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.shuttingDown {
			return
		}
		events := []eventcontract.Event{}
		if checkErr != nil {
			events = a.finishCodexUpgradeOwnerCheckLocked(surfaceID, flowID, codexUpgradeCheckResult{}, fmt.Sprintf("确认升级失败：%v", err))
		} else {
			events = a.finishCodexUpgradeOwnerCheckLocked(surfaceID, flowID, check, fmt.Sprintf("确认升级失败：%v", err))
		}
		if len(events) != 0 {
			a.handleUIEventsLocked(context.Background(), events)
		}
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return
	}
	flow = a.activeCodexUpgradeOwnerFlowLocked()
	if flow == nil || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(flowID) {
		return
	}
	a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageRunning)
	a.handleUIEventsLocked(context.Background(), []eventcontract.Event{codexUpgradeOwnerRunningEvent(surfaceID, flow)})
}

func (a *App) finishCodexUpgradeOwnerRunLocked(surfaceID, flowID string, runErr error) []eventcontract.Event {
	flow, blocked := a.requireCodexUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return blocked
	}
	if runErr != nil {
		a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageFailed)
		event := codexUpgradeOwnerTerminalEvent(
			surfaceID,
			flow,
			"Codex 升级失败",
			"error",
			runErr.Error(),
			"需要时可以重新发送 `/upgrade codex` 再检查一次。",
		)
		a.clearCodexUpgradeOwnerFlowLocked()
		return []eventcontract.Event{event}
	}
	flow.CurrentVersion = firstNonEmpty(strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(flow.LatestVersion), strings.TrimSpace(flow.CurrentVersion))
	a.refreshCodexUpgradeOwnerFlowLocked(flow, codexupgraderuntime.OwnerFlowStageSucceeded)
	event := codexUpgradeOwnerTerminalEvent(
		surfaceID,
		flow,
		"Codex 升级完成",
		"success",
		fmt.Sprintf("已升级到 %s。", firstNonEmpty(strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(flow.CurrentVersion), "unknown")),
	)
	a.clearCodexUpgradeOwnerFlowLocked()
	return []eventcontract.Event{event}
}

func (a *App) handleCodexUpgradeOwnerFlowCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	switch strings.TrimSpace(command.OptionID) {
	case upgradeOwnerActionCheck:
		return a.startCodexUpgradeOwnerCheckLocked(command)
	case upgradeOwnerActionConfirm:
		return a.confirmCodexUpgradeOwnerFlowLocked(command)
	default:
		return nil
	}
}
