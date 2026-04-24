package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const autoWhipPromptText = "你看还有没有别的任务需要完成，有就继续做，没有就说\"老板不要再打我了，真的没有事情干了\""
const autoWhipStopPhrase = "老板不要再打我了，真的没有事情干了"

func autoWhipNotice(surface *state.SurfaceConsoleRecord, code, title, text string) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}

func formatAutoRetryDelay(value time.Duration) string {
	if value <= 0 {
		return "0秒"
	}
	totalSeconds := int(value.Round(time.Second) / time.Second)
	if totalSeconds < 60 {
		return fmt.Sprintf("%d秒", totalSeconds)
	}
	minutes := totalSeconds / 60
	seconds := totalSeconds % 60
	if seconds == 0 {
		return fmt.Sprintf("%d分钟", minutes)
	}
	return fmt.Sprintf("%d分%d秒", minutes, seconds)
}

func autoWhipRetryScheduledNotice(surface *state.SurfaceConsoleRecord, count, max int, delay time.Duration) []eventcontract.Event {
	return autoWhipNotice(
		surface,
		"autowhip_retry_scheduled",
		"AutoWhip",
		fmt.Sprintf("上游不稳定，第 %d/%d 次，%s后重试", count, max, formatAutoRetryDelay(delay)),
	)
}

func autoWhipStartedNotice(surface *state.SurfaceConsoleRecord, count int) []eventcontract.Event {
	return autoWhipNotice(
		surface,
		"autowhip_started",
		"AutoWhip",
		fmt.Sprintf("Codex疑似偷懒,已抽打 %d次", count),
	)
}

func autoWhipCompletedNotice(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return autoWhipNotice(
		surface,
		"autowhip_completed",
		"AutoWhip",
		"Codex 已经把活干完了，老板放过他吧",
	)
}

func (s *Service) noteAutoWhipAction(surface *state.SurfaceConsoleRecord, action control.Action) {
	if surface == nil {
		return
	}
	switch action.Kind {
	case control.ActionTextMessage,
		control.ActionImageMessage,
		control.ActionFileMessage,
		control.ActionAttachInstance,
		control.ActionNewThread,
		control.ActionShowThreads,
		control.ActionShowAllThreads,
		control.ActionShowAllThreadWorkspaces,
		control.ActionShowRecentThreadWorkspaces,
		control.ActionShowScopedThreads,
		control.ActionUseThread,
		control.ActionFollowLocal,
		control.ActionDetach:
		s.resetAutoWhipProgress(surface)
	case control.ActionStop:
		s.resetAutoWhipProgress(surface)
		inst := s.root.Instances[surface.AttachedInstanceID]
		if (inst != nil && inst.ActiveTurnID != "") || surface.ActiveQueueItemID != "" {
			surface.AutoWhip.SuppressOnce = true
		}
	case control.ActionControlRequest:
		s.resetAutoWhipProgress(surface)
		if action.RequestControl != nil && normalizedRequestControl(action.RequestControl.Control) == normalizedRequestControl(frontstagecontract.RequestControlCancelTurn) {
			inst := s.root.Instances[surface.AttachedInstanceID]
			if (inst != nil && inst.ActiveTurnID != "") || surface.ActiveQueueItemID != "" {
				surface.AutoWhip.SuppressOnce = true
			}
		}
	}
}

func (s *Service) resetAutoWhipProgress(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	enabled := surface.AutoWhip.Enabled
	surface.AutoWhip = state.AutoWhipRuntimeRecord{
		Enabled: enabled,
	}
}

func (s *Service) clearAutoWhipPending(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.AutoWhip.PendingReason = ""
	surface.AutoWhip.PendingDueAt = time.Time{}
	surface.AutoWhip.PendingReplyToMessageID = ""
	surface.AutoWhip.PendingReplyToMessagePreview = ""
}

func pendingTurnTextValue(pending map[string]*completedTextItem, instanceID, threadID, turnID string) string {
	item := pending[turnRenderKey(instanceID, threadID, turnID)]
	if item == nil {
		return ""
	}
	return strings.TrimSpace(item.Text)
}

func normalizeAutoWhipText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), "")
}

func autoWhipShouldTrigger(text string) bool {
	return !strings.Contains(normalizeAutoWhipText(text), autoWhipStopPhrase)
}

func autoWhipBackoff(reason state.AutoWhipReason, count int) (time.Duration, int, bool) {
	var delays []time.Duration
	switch reason {
	case state.AutoWhipReasonIncompleteStop:
		delays = []time.Duration{3 * time.Second, 10 * time.Second, 30 * time.Second}
	default:
		return 0, 0, false
	}
	if count <= 0 || count > len(delays) {
		return 0, len(delays), false
	}
	return delays[count-1], len(delays), true
}

func (s *Service) nextAutoWhipAttempt(surface *state.SurfaceConsoleRecord, reason state.AutoWhipReason) (int, time.Duration, int, bool) {
	if surface == nil {
		return 0, 0, 0, false
	}
	count := 1
	switch reason {
	case state.AutoWhipReasonIncompleteStop:
		count = surface.AutoWhip.IncompleteStopCount + 1
	default:
		return 0, 0, 0, false
	}
	delay, max, ok := autoWhipBackoff(reason, count)
	return count, delay, max, ok
}

func (s *Service) scheduleAutoWhip(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, turnID string, reason state.AutoWhipReason) []eventcontract.Event {
	if surface == nil || item == nil {
		return nil
	}
	count, delay, max, ok := s.nextAutoWhipAttempt(surface, reason)
	if !ok {
		s.resetAutoWhipProgress(surface)
		return notice(surface, "autowhip_backoff_exhausted", "autowhip 已达到连续触发上限，已停止本轮自动补打。你可以手动继续，或在新的上下文里再次触发。")
	}

	switch reason {
	case state.AutoWhipReasonIncompleteStop:
		surface.AutoWhip.IncompleteStopCount = count
	}
	surface.AutoWhip.PendingReason = reason
	surface.AutoWhip.PendingDueAt = s.now().Add(delay)
	surface.AutoWhip.ConsecutiveCount = count
	surface.AutoWhip.LastTriggeredTurnID = turnID
	surface.AutoWhip.PendingReplyToMessageID = firstNonEmpty(item.ReplyToMessageID, item.SourceMessageID)
	surface.AutoWhip.PendingReplyToMessagePreview = firstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview)
	if count == max {
		// The final scheduled retry is still allowed; the next attempt will emit the stop notice.
	}
	return nil
}

func (s *Service) autoWhipSurfaceReady(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || !surface.AutoWhip.Enabled {
		return false
	}
	if surface.AttachedInstanceID == "" || surface.Abandoning || surface.PendingHeadless != nil {
		return false
	}
	if surface.DispatchMode != state.DispatchModeNormal {
		return false
	}
	if surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
		return false
	}
	if s.surfaceHasLiveRemoteWork(surface) {
		return false
	}
	return true
}

func (s *Service) maybeScheduleAutoWhipAfterRemoteTurn(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, turnID string, cause terminalCause, finalText string, summary *control.FileChangeSummary) []eventcontract.Event {
	if surface == nil || item == nil || !surface.AutoWhip.Enabled {
		return nil
	}
	if surface.AutoWhip.SuppressOnce {
		surface.AutoWhip.SuppressOnce = false
		s.resetAutoWhipProgress(surface)
		return nil
	}
	if !s.autoWhipSurfaceReady(surface) {
		s.resetAutoWhipProgress(surface)
		return nil
	}
	if cause != terminalCauseCompleted {
		s.resetAutoWhipProgress(surface)
		return nil
	}
	if autoWhipShouldTrigger(finalText) {
		return s.scheduleAutoWhip(surface, item, turnID, state.AutoWhipReasonIncompleteStop)
	}
	s.resetAutoWhipProgress(surface)
	return autoWhipCompletedNotice(surface)
}

func (s *Service) maybeDispatchPendingAutoWhip(surface *state.SurfaceConsoleRecord, now time.Time) []eventcontract.Event {
	if surface == nil || !surface.AutoWhip.Enabled || surface.AutoWhip.PendingReason == "" || surface.AutoWhip.PendingDueAt.IsZero() {
		return nil
	}
	if now.Before(surface.AutoWhip.PendingDueAt) {
		return nil
	}
	if !s.autoWhipSurfaceReady(surface) {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.turns.pendingRemote[inst.InstanceID] != nil {
		return nil
	}
	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	if !createThread && threadID == "" {
		return nil
	}
	if createThread && strings.TrimSpace(cwd) == "" {
		return nil
	}

	replyToMessageID := surface.AutoWhip.PendingReplyToMessageID
	replyToMessagePreview := surface.AutoWhip.PendingReplyToMessagePreview
	reason := surface.AutoWhip.PendingReason
	count := surface.AutoWhip.ConsecutiveCount
	s.clearAutoWhipPending(surface)
	events := make([]eventcontract.Event, 0, 2)
	if reason == state.AutoWhipReasonIncompleteStop {
		events = append(events, autoWhipStartedNotice(surface, count)...)
	}
	events = append(events, s.enqueueAutoWhipQueueItem(
		surface,
		replyToMessageID,
		replyToMessagePreview,
		[]agentproto.Input{{Type: agentproto.InputText, Text: autoWhipPromptText}},
		threadID,
		cwd,
		routeMode,
		surface.PromptOverride,
		false,
	)...)
	return events
}
