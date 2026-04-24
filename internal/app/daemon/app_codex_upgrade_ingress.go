package daemon

import (
	"context"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (a *App) maybeHandleStandaloneCodexUpgradeActionLocked(ctx context.Context, action control.Action) bool {
	tx := a.codexUpgradeRuntime.Active
	if tx == nil || codexUpgradeAllowsAction(action) {
		return false
	}
	a.ensureSurfaceRouteForNotice(action)

	if strings.TrimSpace(action.SurfaceSessionID) == strings.TrimSpace(tx.InitiatorSurface) {
		a.handleUIEventsLocked(ctx, codexUpgradeBlockedEvents(action.SurfaceSessionID, false))
		a.syncSurfaceResumeStateLocked(nil)
		return true
	}
	if codexUpgradeQueueableInput(action) && strings.TrimSpace(a.service.AttachedInstanceID(action.SurfaceSessionID)) != "" {
		a.service.PauseSurfaceDispatch(action.SurfaceSessionID)
		events := a.applyIngressActionLocked(action)
		events = append(events, codexUpgradeBlockedEvents(action.SurfaceSessionID, true)...)
		a.handleUIEventsLocked(ctx, events)
		a.syncSurfaceResumeStateLocked(nil)
		return true
	}

	a.handleUIEventsLocked(ctx, codexUpgradeBlockedEvents(action.SurfaceSessionID, false))
	a.syncSurfaceResumeStateLocked(nil)
	return true
}

func codexUpgradeAllowsAction(action control.Action) bool {
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

func codexUpgradeQueueableInput(action control.Action) bool {
	switch action.Kind {
	case control.ActionTextMessage,
		control.ActionImageMessage,
		control.ActionFileMessage:
		return true
	default:
		return false
	}
}

func codexUpgradeBlockedEvents(surfaceID string, queued bool) []eventcontract.Event {
	text := "当前正在升级 Codex。请等待完成后再继续操作。"
	if queued {
		text = "当前正在升级 Codex，这条输入会在升级完成后执行。"
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code: "codex_upgrade_running",
			Text: text,
		},
	}}
}
