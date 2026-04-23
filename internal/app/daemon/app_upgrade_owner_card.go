package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const (
	upgradeOwnerFlowTTL          = 15 * time.Minute
	upgradeOwnerActionCheck      = "check"
	upgradeOwnerActionConfirm    = "confirm"
	upgradeOwnerActionCancel     = "cancel"
	upgradeOwnerPayloadKind      = "upgrade_owner_flow"
	upgradeOwnerPayloadFlowKey   = "picker_id"
	upgradeOwnerPayloadOptionKey = "option_id"
)

func (a *App) nextUpgradeOwnerFlowIDLocked() string {
	a.upgradeRuntime.NextFlowSeq++
	return fmt.Sprintf("upgrade-owner-%d", a.upgradeRuntime.NextFlowSeq)
}

func (a *App) activeUpgradeOwnerFlowLocked() *upgradeOwnerCardFlowRecord {
	flow := a.upgradeRuntime.ActiveFlow
	if flow == nil {
		return nil
	}
	if !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(time.Now().UTC()) {
		a.clearUpgradeOwnerFlowLocked()
		return nil
	}
	return flow
}

func (a *App) newUpgradeOwnerFlowLocked(surfaceID, ownerUserID, messageID string, stage upgradeOwnerCardFlowStage) *upgradeOwnerCardFlowRecord {
	now := time.Now().UTC()
	flow := &upgradeOwnerCardFlowRecord{
		FlowID:           a.nextUpgradeOwnerFlowIDLocked(),
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		OwnerUserID:      strings.TrimSpace(ownerUserID),
		MessageID:        strings.TrimSpace(messageID),
		Stage:            stage,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(upgradeOwnerFlowTTL),
	}
	a.upgradeRuntime.ActiveFlow = flow
	a.upgradeRuntime.StartCancel = nil
	a.upgradeRuntime.StartFlowID = ""
	return flow
}

func (a *App) refreshUpgradeOwnerFlowLocked(flow *upgradeOwnerCardFlowRecord, stage upgradeOwnerCardFlowStage) {
	if flow == nil {
		return
	}
	now := time.Now().UTC()
	flow.Stage = stage
	flow.UpdatedAt = now
	flow.ExpiresAt = now.Add(upgradeOwnerFlowTTL)
}

func (a *App) clearUpgradeOwnerFlowLocked() {
	a.upgradeRuntime.ActiveFlow = nil
	a.upgradeRuntime.StartCancel = nil
	a.upgradeRuntime.StartFlowID = ""
}

func (a *App) recordUpgradeOwnerCardMessageLocked(trackingKey, messageID string) {
	flow := a.activeUpgradeOwnerFlowLocked()
	if flow == nil {
		return
	}
	if strings.TrimSpace(flow.FlowID) != strings.TrimSpace(trackingKey) {
		return
	}
	flow.MessageID = strings.TrimSpace(messageID)
}

func (a *App) requireUpgradeOwnerFlowLocked(surfaceID, flowID, actorUserID string) (*upgradeOwnerCardFlowRecord, []eventcontract.Event) {
	flow := a.activeUpgradeOwnerFlowLocked()
	if flow == nil || strings.TrimSpace(flow.FlowID) != strings.TrimSpace(flowID) {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "upgrade_owner_expired", "这张升级卡片已失效，请重新发送 `/upgrade latest`。")}
	}
	if strings.TrimSpace(flow.SurfaceSessionID) != "" && strings.TrimSpace(flow.SurfaceSessionID) != strings.TrimSpace(surfaceID) {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "upgrade_owner_expired", "这张升级卡片已失效，请重新发送 `/upgrade latest`。")}
	}
	actorUserID = strings.TrimSpace(firstNonEmpty(actorUserID, a.service.SurfaceActorUserID(surfaceID)))
	if owner := strings.TrimSpace(flow.OwnerUserID); owner != "" && actorUserID != "" && owner != actorUserID {
		return nil, []eventcontract.Event{upgradeNoticeEvent(strings.TrimSpace(surfaceID), "upgrade_owner_unauthorized", "这张升级卡片只允许发起者本人操作。")}
	}
	return flow, nil
}

func (a *App) upgradeOwnerFlowBlocksInputLocked() bool {
	flow := a.activeUpgradeOwnerFlowLocked()
	if flow == nil {
		return false
	}
	switch flow.Stage {
	case upgradeOwnerFlowStageRunning, upgradeOwnerFlowStageCancelling, upgradeOwnerFlowStageRestarting:
		return true
	default:
		return false
	}
}

func upgradeOwnerFlowAllowsAction(action control.Action) bool {
	switch action.Kind {
	case control.ActionStatus,
		control.ActionUpgradeCommand,
		control.ActionUpgradeOwnerFlow,
		control.ActionDebugCommand,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return true
	default:
		return false
	}
}

func upgradeOwnerFlowBlockedEvents(surfaceID string) []eventcontract.Event {
	return []eventcontract.Event{upgradeNoticeEvent(surfaceID, "upgrade_running_blocked", "当前正在准备升级。普通输入和其他操作已暂停，请等待完成，或在升级卡片里取消。")}
}

func upgradeOwnerButton(label, flowID, optionID, style string, disabled bool) control.CommandCatalogButton {
	return control.CommandCatalogButton{
		Label: label,
		Kind:  control.CommandCatalogButtonCallbackAction,
		CallbackValue: map[string]any{
			"kind":                       upgradeOwnerPayloadKind,
			upgradeOwnerPayloadFlowKey:   strings.TrimSpace(flowID),
			upgradeOwnerPayloadOptionKey: strings.TrimSpace(optionID),
		},
		Style:    style,
		Disabled: disabled,
	}
}

func upgradeOwnerCardEvent(surfaceID string, flow *upgradeOwnerCardFlowRecord, title, theme string, bodySections, noticeSections []control.FeishuCardTextSection, buttons []control.CommandCatalogButton, sealed bool) eventcontract.Event {
	interactive := len(buttons) > 0 && !sealed
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:          strings.TrimSpace(title),
		MessageID:      strings.TrimSpace(flowMessageID(flow)),
		TrackingKey:    strings.TrimSpace(flowTrackingKey(flow)),
		ThemeKey:       strings.TrimSpace(theme),
		Patchable:      true,
		BodySections:   append([]control.FeishuCardTextSection(nil), bodySections...),
		NoticeSections: append([]control.FeishuCardTextSection(nil), noticeSections...),
		Interactive:    interactive,
		Sealed:         sealed,
		RelatedButtons: append([]control.CommandCatalogButton(nil), buttons...),
	})
	return surfacePagePayloadEvent(surfaceID, eventcontract.PagePayload{View: view}, false)
}

func upgradeOwnerContextSections(currentVersion, targetVersion, track string) []control.FeishuCardTextSection {
	sections := make([]control.FeishuCardTextSection, 0, 3)
	if currentVersion = strings.TrimSpace(currentVersion); currentVersion != "" {
		sections = append(sections, commandCatalogTextSection("当前版本", currentVersion))
	}
	if targetVersion = strings.TrimSpace(targetVersion); targetVersion != "" {
		sections = append(sections, commandCatalogTextSection("目标版本", targetVersion))
	}
	if track = strings.TrimSpace(track); track != "" {
		sections = append(sections, commandCatalogTextSection("当前 track", track))
	}
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func upgradeOwnerNoticeSections(lines ...string) []control.FeishuCardTextSection {
	return commandCatalogSummarySections(lines...)
}

func flowMessageID(flow *upgradeOwnerCardFlowRecord) string {
	if flow == nil {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}

func flowTrackingKey(flow *upgradeOwnerCardFlowRecord) string {
	if flow == nil || strings.TrimSpace(flow.MessageID) != "" {
		return ""
	}
	return strings.TrimSpace(flow.FlowID)
}

func upgradeOwnerCheckingEvent(surfaceID string, flow *upgradeOwnerCardFlowRecord, stateValue install.InstallState) eventcontract.Event {
	track := firstNonEmpty(strings.TrimSpace(string(stateValue.CurrentTrack)), "unknown")
	bodySections := upgradeOwnerContextSections(
		firstNonEmpty(strings.TrimSpace(stateValue.CurrentVersion), "unknown"),
		"",
		track,
	)
	noticeSections := upgradeOwnerNoticeSections(fmt.Sprintf("正在按 %s track 检查最新版本。", track))
	return upgradeOwnerCardEvent(surfaceID, flow, "正在检查升级", "progress", bodySections, noticeSections, nil, false)
}

func upgradeOwnerConfirmEvent(surfaceID string, flow *upgradeOwnerCardFlowRecord, stateValue install.InstallState) eventcontract.Event {
	targetVersion := firstNonEmpty(strings.TrimSpace(flow.TargetVersion), pendingTargetVersion(stateValue.PendingUpgrade), strings.TrimSpace(stateValue.LastKnownLatestVersion), "unknown")
	currentVersion := firstNonEmpty(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(stateValue.CurrentVersion), "unknown")
	track := firstNonEmpty(strings.TrimSpace(string(flow.Track)), strings.TrimSpace(string(stateValue.CurrentTrack)), "unknown")
	bodySections := upgradeOwnerContextSections(currentVersion, targetVersion, track)
	noticeSections := upgradeOwnerNoticeSections("确认后会开始下载并准备升级。执行期间普通输入会暂停，直到完成或取消。")
	return upgradeOwnerCardEvent(surfaceID, flow, "发现可升级版本", "approval", bodySections, noticeSections, []control.CommandCatalogButton{
		upgradeOwnerButton("确认升级", flow.FlowID, upgradeOwnerActionConfirm, "primary", false),
		upgradeOwnerButton("取消", flow.FlowID, upgradeOwnerActionCancel, "default", false),
	}, false)
}

func upgradeOwnerRunningEvent(surfaceID string, flow *upgradeOwnerCardFlowRecord, title, detail string, canCancel bool) eventcontract.Event {
	targetVersion := firstNonEmpty(strings.TrimSpace(flow.TargetVersion), "unknown")
	bodySections := upgradeOwnerContextSections(strings.TrimSpace(flow.CurrentVersion), targetVersion, strings.TrimSpace(string(flow.Track)))
	noticeSections := upgradeOwnerNoticeSections(strings.TrimSpace(detail), "普通输入已暂停，请等待完成。")
	var buttons []control.CommandCatalogButton
	if canCancel {
		buttons = []control.CommandCatalogButton{
			upgradeOwnerButton("取消升级", flow.FlowID, upgradeOwnerActionCancel, "default", false),
		}
	}
	return upgradeOwnerCardEvent(surfaceID, flow, title, "progress", bodySections, noticeSections, buttons, false)
}

func upgradeOwnerTerminalEvent(surfaceID string, flow *upgradeOwnerCardFlowRecord, title, theme string, bodySections, noticeSections []control.FeishuCardTextSection) eventcontract.Event {
	return upgradeOwnerCardEvent(surfaceID, flow, title, theme, bodySections, noticeSections, nil, true)
}

func pendingTargetVersion(pending *install.PendingUpgrade) string {
	if pending == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(pending.TargetSlot), strings.TrimSpace(pending.TargetVersion))
}

func (a *App) activeUpgradeOwnerFlowMatchesPendingLocked(pending *install.PendingUpgrade) bool {
	flow := a.activeUpgradeOwnerFlowLocked()
	if flow == nil || pending == nil {
		return false
	}
	if flow.Source != pending.Source {
		return false
	}
	return firstNonEmpty(strings.TrimSpace(flow.TargetVersion), "") == pendingTargetVersion(pending)
}

func (a *App) startUpgradeLatestOwnerCheckLocked(command control.DaemonCommand, stateValue install.InstallState) []eventcontract.Event {
	track := stateValue.CurrentTrack
	if track == "" {
		track = defaultUpgradeTrackForState(stateValue)
	}
	messageID := ""
	if command.FromCardAction {
		messageID = command.SourceMessageID
	}
	flow := a.newUpgradeOwnerFlowLocked(command.SurfaceSessionID, a.service.SurfaceActorUserID(command.SurfaceSessionID), messageID, upgradeOwnerFlowStageChecking)
	flow.Source = install.UpgradeSourceRelease
	flow.Track = track
	flow.CurrentVersion = strings.TrimSpace(stateValue.CurrentVersion)
	events := []eventcontract.Event{
		upgradeOwnerCheckingEvent(command.SurfaceSessionID, flow, stateValue),
		{
			Kind:             eventcontract.KindDaemonCommand,
			GatewayID:        command.GatewayID,
			SurfaceSessionID: command.SurfaceSessionID,
			SourceMessageID:  command.SourceMessageID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandUpgradeOwnerFlow,
				GatewayID:        command.GatewayID,
				SurfaceSessionID: command.SurfaceSessionID,
				SourceMessageID:  command.SourceMessageID,
				PickerID:         flow.FlowID,
				OptionID:         upgradeOwnerActionCheck,
			},
		},
	}
	return events
}

func (a *App) openUpgradeLatestOwnerConfirmLocked(command control.DaemonCommand, stateValue install.InstallState) []eventcontract.Event {
	messageID := ""
	if command.FromCardAction {
		messageID = command.SourceMessageID
	}
	flow := a.newUpgradeOwnerFlowLocked(command.SurfaceSessionID, a.service.SurfaceActorUserID(command.SurfaceSessionID), messageID, upgradeOwnerFlowStageConfirm)
	flow.Source = install.UpgradeSourceRelease
	flow.Track = stateValue.CurrentTrack
	flow.CurrentVersion = strings.TrimSpace(stateValue.CurrentVersion)
	flow.TargetVersion = pendingTargetVersion(stateValue.PendingUpgrade)
	return []eventcontract.Event{upgradeOwnerConfirmEvent(command.SurfaceSessionID, flow, stateValue)}
}

func (a *App) handleUpgradeOwnerFlowCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	switch strings.TrimSpace(command.OptionID) {
	case upgradeOwnerActionCheck:
		return a.startUpgradeOwnerCheckLocked(command)
	case upgradeOwnerActionConfirm:
		return a.confirmUpgradeOwnerFlowLocked(command)
	case upgradeOwnerActionCancel:
		return a.cancelUpgradeOwnerFlowLocked(command)
	default:
		return nil
	}
}

func (a *App) startUpgradeOwnerCheckLocked(command control.DaemonCommand) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(command.SurfaceSessionID, command.PickerID, a.service.SurfaceActorUserID(command.SurfaceSessionID))
	if blocked != nil {
		return blocked
	}
	if strings.TrimSpace(flow.MessageID) == "" {
		a.clearUpgradeOwnerFlowLocked()
		return nil
	}
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return a.finishUpgradeOwnerFlowFailureLocked(command.SurfaceSessionID, flow.FlowID, fmt.Sprintf("读取升级状态失败：%v", err))
	}
	if a.upgradeRuntime.CheckInFlight {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_check_busy", "当前已经有一个升级检查在进行中，请稍后再试。")}
	}
	track := stateValue.CurrentTrack
	if track == "" {
		track = defaultUpgradeTrackForState(stateValue)
	}
	flow.Track = track
	flow.CurrentVersion = strings.TrimSpace(stateValue.CurrentVersion)
	a.upgradeRuntime.CheckInFlight = true
	go a.runUpgradeCheck(upgradeCheckRequest{
		Track:            track,
		Manual:           true,
		GatewayID:        command.GatewayID,
		SurfaceSessionID: command.SurfaceSessionID,
		SourceMessageID:  command.SourceMessageID,
		FlowID:           flow.FlowID,
	})
	return nil
}

func (a *App) confirmUpgradeOwnerFlowLocked(command control.DaemonCommand) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(command.SurfaceSessionID, command.PickerID, a.service.SurfaceActorUserID(command.SurfaceSessionID))
	if blocked != nil {
		return blocked
	}
	if flow.Stage != upgradeOwnerFlowStageConfirm {
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_owner_invalid_stage", "当前升级卡片已经不在可确认状态，请重新发送 `/upgrade latest`。")}
	}
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return a.finishUpgradeOwnerFlowFailureLocked(command.SurfaceSessionID, flow.FlowID, fmt.Sprintf("读取升级状态失败：%v", err))
	}
	if !pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceRelease) {
		return a.finishUpgradeOwnerFlowFailureLocked(command.SurfaceSessionID, flow.FlowID, "当前没有可继续的 release 升级候选，请重新发送 `/upgrade latest`。")
	}
	now := time.Now().UTC()
	stateValue.PendingUpgrade.GatewayID = firstNonEmpty(strings.TrimSpace(command.GatewayID), a.service.SurfaceGatewayID(command.SurfaceSessionID))
	stateValue.PendingUpgrade.SurfaceSessionID = command.SurfaceSessionID
	stateValue.PendingUpgrade.ChatID = a.service.SurfaceChatID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.ActorUserID = a.service.SurfaceActorUserID(command.SurfaceSessionID)
	stateValue.PendingUpgrade.SourceMessageID = firstNonEmpty(strings.TrimSpace(flow.MessageID), strings.TrimSpace(command.SourceMessageID))
	if stateValue.PendingUpgrade.RequestedAt == nil {
		stateValue.PendingUpgrade.RequestedAt = &now
	}
	if err := a.writeUpgradeStateLocked(stateValue); err != nil {
		return a.finishUpgradeOwnerFlowFailureLocked(command.SurfaceSessionID, flow.FlowID, fmt.Sprintf("写入升级事务失败：%v", err))
	}
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageRunning)
	flow.TargetVersion = pendingTargetVersion(stateValue.PendingUpgrade)
	a.upgradeRuntime.StartInFlight = true
	startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	a.upgradeRuntime.StartCancel = cancel
	a.upgradeRuntime.StartFlowID = flow.FlowID
	go a.runPendingUpgradeStart(upgradeStartRequest{
		State:            stateValue,
		GatewayID:        stateValue.PendingUpgrade.GatewayID,
		SurfaceSessionID: stateValue.PendingUpgrade.SurfaceSessionID,
		SourceMessageID:  stateValue.PendingUpgrade.SourceMessageID,
		FlowID:           flow.FlowID,
		Context:          startCtx,
	})
	return []eventcontract.Event{
		upgradeOwnerRunningEvent(command.SurfaceSessionID, flow, "正在下载目标版本", "正在下载升级所需的目标版本并准备切换。", true),
	}
}

func (a *App) cancelUpgradeOwnerFlowLocked(command control.DaemonCommand) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(command.SurfaceSessionID, command.PickerID, a.service.SurfaceActorUserID(command.SurfaceSessionID))
	if blocked != nil {
		return blocked
	}
	switch flow.Stage {
	case upgradeOwnerFlowStageConfirm:
		stateValue, ok, err := a.loadUpgradeStateLocked(true)
		if err == nil && ok && pendingUpgradeCandidateFromSource(stateValue.PendingUpgrade, install.UpgradeSourceRelease) {
			stateValue.PendingUpgrade = nil
			_ = a.writeUpgradeStateLocked(stateValue)
		}
		event := upgradeOwnerTerminalEvent(
			command.SurfaceSessionID,
			flow,
			"升级已取消",
			"info",
			upgradeOwnerContextSections(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(string(flow.Track))),
			upgradeOwnerNoticeSections("已取消这次 release 升级。需要时可重新发送 `/upgrade latest`。"),
		)
		a.clearUpgradeOwnerFlowLocked()
		return []eventcontract.Event{event}
	case upgradeOwnerFlowStageRunning:
		flow.CancelRequested = true
		a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageCancelling)
		if a.upgradeRuntime.StartFlowID == flow.FlowID && a.upgradeRuntime.StartCancel != nil {
			cancel := a.upgradeRuntime.StartCancel
			a.upgradeRuntime.StartCancel = nil
			cancel()
		}
		return []eventcontract.Event{
			upgradeOwnerRunningEvent(command.SurfaceSessionID, flow, "正在取消升级", "正在停止当前升级准备流程，请稍候。", false),
		}
	case upgradeOwnerFlowStageCancelling:
		return nil
	default:
		return []eventcontract.Event{upgradeNoticeEvent(command.SurfaceSessionID, "upgrade_cancel_unavailable", "当前升级流程已经无法取消，请等待完成或重新发送 `/upgrade latest`。")}
	}
}

func (a *App) finishUpgradeOwnerFlowConfirmLocked(surfaceID, flowID string, stateValue install.InstallState) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	flow.Source = install.UpgradeSourceRelease
	flow.Track = stateValue.CurrentTrack
	flow.CurrentVersion = strings.TrimSpace(stateValue.CurrentVersion)
	flow.TargetVersion = pendingTargetVersion(stateValue.PendingUpgrade)
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageConfirm)
	return []eventcontract.Event{upgradeOwnerConfirmEvent(surfaceID, flow, stateValue)}
}

func (a *App) finishUpgradeOwnerFlowLatestLocked(surfaceID, flowID string, stateValue install.InstallState, latestVersion string) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageCompleted)
	bodySections := upgradeOwnerContextSections(strings.TrimSpace(stateValue.CurrentVersion), "", firstNonEmpty(strings.TrimSpace(string(stateValue.CurrentTrack)), "unknown"))
	noticeSections := upgradeOwnerNoticeSections(fmt.Sprintf("当前已经是 %s track 的最新版本 %s。", firstNonEmpty(strings.TrimSpace(string(stateValue.CurrentTrack)), "unknown"), firstNonEmpty(strings.TrimSpace(latestVersion), "unknown")))
	event := upgradeOwnerTerminalEvent(surfaceID, flow, "已是最新版本", "success", bodySections, noticeSections)
	a.clearUpgradeOwnerFlowLocked()
	return []eventcontract.Event{event}
}

func (a *App) finishUpgradeOwnerFlowFailureLocked(surfaceID, flowID, text string) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageFailed)
	event := upgradeOwnerTerminalEvent(
		surfaceID,
		flow,
		"升级失败",
		"error",
		upgradeOwnerContextSections(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(string(flow.Track))),
		upgradeOwnerNoticeSections(strings.TrimSpace(text)),
	)
	a.clearUpgradeOwnerFlowLocked()
	return []eventcontract.Event{event}
}

func (a *App) finishUpgradeOwnerFlowCancelledLocked(surfaceID, flowID string) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageCancelled)
	event := upgradeOwnerTerminalEvent(
		surfaceID,
		flow,
		"升级已取消",
		"info",
		upgradeOwnerContextSections(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(string(flow.Track))),
		upgradeOwnerNoticeSections("已取消这次 release 升级。需要时可重新发送 `/upgrade latest`。"),
	)
	a.clearUpgradeOwnerFlowLocked()
	return []eventcontract.Event{event}
}

func (a *App) updateUpgradeOwnerFlowRunningLocked(surfaceID, flowID string, stage upgradeOwnerCardFlowStage, title, detail string, canCancel bool) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	a.refreshUpgradeOwnerFlowLocked(flow, stage)
	return []eventcontract.Event{upgradeOwnerRunningEvent(surfaceID, flow, title, detail, canCancel)}
}

func (a *App) sealUpgradeOwnerFlowRestartingLocked(surfaceID, flowID string) []eventcontract.Event {
	flow, blocked := a.requireUpgradeOwnerFlowLocked(surfaceID, flowID, a.service.SurfaceActorUserID(surfaceID))
	if blocked != nil {
		return nil
	}
	a.refreshUpgradeOwnerFlowLocked(flow, upgradeOwnerFlowStageRestarting)
	return []eventcontract.Event{
		upgradeOwnerTerminalEvent(
			surfaceID,
			flow,
			"正在重启",
			"progress",
			upgradeOwnerContextSections(strings.TrimSpace(flow.CurrentVersion), strings.TrimSpace(flow.TargetVersion), strings.TrimSpace(string(flow.Track))),
			upgradeOwnerNoticeSections("升级准备完成，服务正在重启。恢复后会回到默认交互状态。"),
		),
	}
}

func (a *App) finishUpgradeOwnerStartErrorLocked(request upgradeStartRequest, err error) []eventcontract.Event {
	if request.FlowID == "" {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return a.finishUpgradeOwnerFlowCancelledLocked(request.SurfaceSessionID, request.FlowID)
	}
	return a.finishUpgradeOwnerFlowFailureLocked(request.SurfaceSessionID, request.FlowID, err.Error())
}
