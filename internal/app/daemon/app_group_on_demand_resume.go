package daemon

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (a *App) maybeStartFeishuGroupOnDemandResumeLocked(action control.Action) ([]eventcontract.Event, bool) {
	surfaceID := strings.TrimSpace(action.SurfaceSessionID)
	if surfaceID == "" || !surfaceIsFeishuGroup(surfaceID) {
		return nil, false
	}
	if action.Kind != control.ActionTextMessage {
		return nil, false
	}
	snapshot := a.service.SurfaceSnapshot(surfaceID)
	if snapshot == nil {
		return nil, false
	}
	if strings.TrimSpace(snapshot.Attachment.InstanceID) != "" || strings.TrimSpace(snapshot.PendingHeadless.InstanceID) != "" {
		return nil, false
	}
	if a.surfaceResumeRuntime.store == nil {
		return nil, false
	}
	entry, ok := a.surfaceResumeRuntime.store.Get(surfaceID)
	if !ok || !surfaceResumeEntryAllowsOnDemandRecovery(entry, action) || !surfaceResumeEntryNeedsRecovery(entry) {
		return nil, false
	}
	if state.IsVSCodeProductMode(state.ProductMode(entry.ProductMode)) {
		return groupOnDemandResumeNotice(action, "group_on_demand_vscode_unsupported", "当前群暂不能自动恢复 VS Code", "群聊按需恢复当前只支持 headless 模式。请在私聊处理 VS Code 连接，或在群里切换到 headless 模式后再试。"), true
	}
	if !state.IsHeadlessProductMode(state.ProductMode(entry.ProductMode)) {
		return nil, false
	}

	events, result := a.service.TryAutoResumeHeadlessSurface(surfaceID, surfaceResumeAttemptFromEntry(entry), true)
	now := time.Now().UTC()
	switch result.Status {
	case orchestrator.SurfaceResumeStatusStarting:
		a.saveGroupOnDemandResumeContinuationLocked(surfaceID, action, now)
		return events, true
	case orchestrator.SurfaceResumeStatusThreadAttached, orchestrator.SurfaceResumeStatusWorkspaceAttached:
		a.clearGroupOnDemandTerminalFailureLocked(surfaceID)
		a.clearSurfaceResumeBackoffLocked(surfaceID)
		return events, false
	case orchestrator.SurfaceResumeStatusFailed:
		displayCode, emit := a.recordGroupOnDemandTerminalFailureLocked(surfaceID, result.FailureCode)
		events = rewriteHeadlessRestoreFailureEvents(events, displayCode, emit)
		if !emit {
			return nil, true
		}
		if len(events) == 0 {
			if notice := orchestrator.NoticeForSurfaceResumeFailure(displayCode); notice != nil {
				events = append(events, eventcontract.Event{
					Kind:             eventcontract.KindNotice,
					GatewayID:        action.GatewayID,
					SurfaceSessionID: surfaceID,
					Notice:           notice,
				})
			}
		}
		for i := range events {
			if strings.TrimSpace(events[i].GatewayID) == "" {
				events[i].GatewayID = action.GatewayID
			}
		}
		return events, true
	case orchestrator.SurfaceResumeStatusWaiting:
		return groupOnDemandResumeNotice(action, "group_on_demand_resume_waiting", "正在准备恢复", "当前群上下文还在准备恢复，请稍后再试。"), true
	default:
		return nil, false
	}
}

func surfaceResumeAttemptFromEntry(entry surfaceresume.Entry) orchestrator.SurfaceResumeAttempt {
	return orchestrator.SurfaceResumeAttempt{
		InstanceID:       entry.ResumeInstanceID,
		ThreadID:         entry.ResumeThreadID,
		ThreadTitle:      entry.ResumeThreadTitle,
		ThreadCWD:        entry.ResumeThreadCWD,
		WorkspaceKey:     entry.ResumeWorkspaceKey,
		Backend:          agentproto.Backend(entry.Backend),
		PrepareNewThread: strings.TrimSpace(entry.ResumeRouteMode) == string(state.RouteModeNewThreadReady),
		ResumeHeadless:   entry.ResumeHeadless,
		ReserveRoomSlot:  true,
	}
}

func (a *App) saveGroupOnDemandResumeContinuationLocked(surfaceID string, action control.Action, now time.Time) {
	if a.surfaceResumeRuntime.groupOnDemandContinuations == nil {
		a.surfaceResumeRuntime.groupOnDemandContinuations = map[string]*groupOnDemandResumeContinuation{}
	}
	expiresAt := now.Add(30 * time.Second)
	if snapshot := a.service.SurfaceSnapshot(surfaceID); snapshot != nil && !snapshot.PendingHeadless.ExpiresAt.IsZero() {
		expiresAt = snapshot.PendingHeadless.ExpiresAt
	}
	a.surfaceResumeRuntime.groupOnDemandContinuations[surfaceID] = &groupOnDemandResumeContinuation{
		Action:    cloneControlAction(action),
		CreatedAt: now,
		ExpireAt:  expiresAt,
	}
}

func (a *App) replayGroupOnDemandResumeContinuationsLocked(ctx context.Context, instanceID string, now time.Time) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" || len(a.surfaceResumeRuntime.groupOnDemandContinuations) == 0 {
		return
	}
	replays := make([]control.Action, 0)
	for surfaceID, continuation := range a.surfaceResumeRuntime.groupOnDemandContinuations {
		if continuation == nil {
			delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
			continue
		}
		if !continuation.ExpireAt.IsZero() && now.After(continuation.ExpireAt) {
			delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
			continue
		}
		snapshot := a.service.SurfaceSnapshot(surfaceID)
		if snapshot == nil || strings.TrimSpace(snapshot.Attachment.InstanceID) != instanceID {
			continue
		}
		delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
		a.service.ReleaseFeishuRoomGroupOnDemandReservation(surfaceID)
		replays = append(replays, cloneControlAction(continuation.Action))
	}
	for _, action := range replays {
		a.handleActionLocked(ctx, action, ingressEpisodeOptions{allowGroupOnDemandResume: false})
	}
}

func (a *App) consumeGroupOnDemandResumeContinuationLocked(surfaceID string) (*groupOnDemandResumeContinuation, bool) {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" || len(a.surfaceResumeRuntime.groupOnDemandContinuations) == 0 {
		return nil, false
	}
	continuation := a.surfaceResumeRuntime.groupOnDemandContinuations[surfaceID]
	if continuation == nil {
		delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
		return nil, false
	}
	delete(a.surfaceResumeRuntime.groupOnDemandContinuations, surfaceID)
	return continuation, true
}

func cloneControlAction(action control.Action) control.Action {
	clone := action
	clone.Inputs = append([]agentproto.Input(nil), action.Inputs...)
	clone.SteerInputs = append([]agentproto.Input(nil), action.SteerInputs...)
	clone.Files = append([]control.ActionFileAttachment(nil), action.Files...)
	if action.RequestAnswers != nil {
		clone.RequestAnswers = make(map[string][]string, len(action.RequestAnswers))
		for key, values := range action.RequestAnswers {
			clone.RequestAnswers[key] = append([]string(nil), values...)
		}
	}
	if action.Inbound != nil {
		inbound := *action.Inbound
		clone.Inbound = &inbound
	}
	return clone
}

func groupOnDemandResumeNotice(action control.Action, code, title, text string) []eventcontract.Event {
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		GatewayID:        action.GatewayID,
		SurfaceSessionID: action.SurfaceSessionID,
		Notice: &control.Notice{
			Code:     code,
			Title:    title,
			Text:     text,
			ThemeKey: "warning",
		},
	}}
}

func (a *App) recordGroupOnDemandTerminalFailureLocked(surfaceID, code string) (string, bool) {
	surfaceID = strings.TrimSpace(surfaceID)
	code = strings.TrimSpace(code)
	if !isTerminalSurfaceResumeFailure(code) {
		return code, true
	}
	if a.surfaceResumeRuntime.groupTerminalFailureNotices == nil {
		a.surfaceResumeRuntime.groupTerminalFailureNotices = map[string]string{}
	}
	if last, ok := a.surfaceResumeRuntime.groupTerminalFailureNotices[surfaceID]; ok && last == code {
		return code, false
	}
	a.surfaceResumeRuntime.groupTerminalFailureNotices[surfaceID] = code
	return code, true
}

func (a *App) clearGroupOnDemandTerminalFailureLocked(surfaceID string) {
	if a.surfaceResumeRuntime.groupTerminalFailureNotices != nil {
		delete(a.surfaceResumeRuntime.groupTerminalFailureNotices, strings.TrimSpace(surfaceID))
	}
}
