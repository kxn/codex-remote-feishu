package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const autoContinuePromptText = "你看还有没有别的任务需要完成，有就继续做，没有就说\"老板不要再打我了，真的没有事情干了\""
const autoContinueStopPhrase = "老板不要再打我了，真的没有事情干了"

func autoContinueNotice(surface *state.SurfaceConsoleRecord, code, title, text string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	return []control.UIEvent{{
		Kind:             control.UIEventNotice,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  code,
			Title: title,
			Text:  text,
		},
	}}
}

func formatAutoContinueDelay(value time.Duration) string {
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

func autoContinueRetryScheduledNotice(surface *state.SurfaceConsoleRecord, count, max int, delay time.Duration) []control.UIEvent {
	return autoContinueNotice(
		surface,
		"auto_continue_retry_scheduled",
		"AutoWhip",
		fmt.Sprintf("上游不稳定，第 %d/%d 次，%s后重试", count, max, formatAutoContinueDelay(delay)),
	)
}

func autoContinueWhipStartedNotice(surface *state.SurfaceConsoleRecord, count int) []control.UIEvent {
	return autoContinueNotice(
		surface,
		"auto_continue_whip_started",
		"AutoWhip",
		fmt.Sprintf("Codex疑似偷懒,已抽打 %d次", count),
	)
}

func autoContinueCompletedNotice(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	return autoContinueNotice(
		surface,
		"auto_continue_completed",
		"AutoWhip",
		"Codex 已经把活干完了，老板放过他吧",
	)
}

func (s *Service) noteAutoContinueAction(surface *state.SurfaceConsoleRecord, action control.Action) {
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
		s.resetAutoContinueProgress(surface)
	case control.ActionStop:
		s.resetAutoContinueProgress(surface)
		inst := s.root.Instances[surface.AttachedInstanceID]
		if (inst != nil && inst.ActiveTurnID != "") || surface.ActiveQueueItemID != "" {
			surface.AutoContinue.SuppressOnce = true
		}
	}
}

func (s *Service) resetAutoContinueProgress(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	enabled := surface.AutoContinue.Enabled
	surface.AutoContinue = state.AutoContinueRuntimeRecord{
		Enabled: enabled,
	}
}

func (s *Service) clearAutoContinuePending(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.AutoContinue.PendingReason = ""
	surface.AutoContinue.PendingDueAt = time.Time{}
	surface.AutoContinue.PendingReplyToMessageID = ""
	surface.AutoContinue.PendingReplyToMessagePreview = ""
}

func pendingTurnTextValue(pending map[string]*completedTextItem, instanceID, threadID, turnID string) string {
	item := pending[turnRenderKey(instanceID, threadID, turnID)]
	if item == nil {
		return ""
	}
	return strings.TrimSpace(item.Text)
}

func normalizeAutoContinueText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), "")
}

func autoContinueShouldWhip(text string) bool {
	return !strings.Contains(normalizeAutoContinueText(text), autoContinueStopPhrase)
}

func autoContinueBackoff(reason state.AutoContinueReason, count int) (time.Duration, int, bool) {
	var delays []time.Duration
	switch reason {
	case state.AutoContinueReasonIncompleteStop:
		delays = []time.Duration{3 * time.Second, 10 * time.Second, 30 * time.Second}
	case state.AutoContinueReasonRetryableFailure:
		delays = []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second, 300 * time.Second}
	default:
		return 0, 0, false
	}
	if count <= 0 || count > len(delays) {
		return 0, len(delays), false
	}
	return delays[count-1], len(delays), true
}

func (s *Service) nextAutoContinueAttempt(surface *state.SurfaceConsoleRecord, reason state.AutoContinueReason) (int, time.Duration, int, bool) {
	if surface == nil {
		return 0, 0, 0, false
	}
	count := 1
	switch reason {
	case state.AutoContinueReasonIncompleteStop:
		count = surface.AutoContinue.IncompleteStopCount + 1
	case state.AutoContinueReasonRetryableFailure:
		count = surface.AutoContinue.RetryableFailureCount + 1
	default:
		return 0, 0, 0, false
	}
	delay, max, ok := autoContinueBackoff(reason, count)
	return count, delay, max, ok
}

func (s *Service) scheduleAutoContinue(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, turnID string, reason state.AutoContinueReason) []control.UIEvent {
	if surface == nil || item == nil {
		return nil
	}
	count, delay, max, ok := s.nextAutoContinueAttempt(surface, reason)
	if !ok {
		s.resetAutoContinueProgress(surface)
		return notice(surface, "auto_continue_backoff_exhausted", "autowhip 已达到连续触发上限，已停止本轮自动补打。你可以手动继续，或在新的上下文里再次触发。")
	}

	switch reason {
	case state.AutoContinueReasonIncompleteStop:
		surface.AutoContinue.IncompleteStopCount = count
	case state.AutoContinueReasonRetryableFailure:
		surface.AutoContinue.RetryableFailureCount = count
	}
	surface.AutoContinue.PendingReason = reason
	surface.AutoContinue.PendingDueAt = s.now().Add(delay)
	surface.AutoContinue.ConsecutiveCount = count
	surface.AutoContinue.LastTriggeredTurnID = turnID
	surface.AutoContinue.PendingReplyToMessageID = firstNonEmpty(item.ReplyToMessageID, item.SourceMessageID)
	surface.AutoContinue.PendingReplyToMessagePreview = firstNonEmpty(item.ReplyToMessagePreview, item.SourceMessagePreview)
	if count == max {
		// The final scheduled retry is still allowed; the next attempt will emit the stop notice.
	}
	if reason == state.AutoContinueReasonRetryableFailure {
		return autoContinueRetryScheduledNotice(surface, count, max, delay)
	}
	return nil
}

func (s *Service) autoContinueSurfaceReady(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || !surface.AutoContinue.Enabled {
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

func (s *Service) maybeScheduleAutoContinueAfterRemoteTurn(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, turnID, status string, problem *agentproto.ErrorInfo, finalText string, summary *control.FileChangeSummary) []control.UIEvent {
	if surface == nil || item == nil || !surface.AutoContinue.Enabled {
		return nil
	}
	if surface.AutoContinue.SuppressOnce {
		surface.AutoContinue.SuppressOnce = false
		s.resetAutoContinueProgress(surface)
		return nil
	}
	if !s.autoContinueSurfaceReady(surface) {
		s.resetAutoContinueProgress(surface)
		return nil
	}
	if problem != nil && problem.Retryable {
		return s.scheduleAutoContinue(surface, item, turnID, state.AutoContinueReasonRetryableFailure)
	}
	if autoContinueShouldWhip(finalText) {
		return s.scheduleAutoContinue(surface, item, turnID, state.AutoContinueReasonIncompleteStop)
	}
	s.resetAutoContinueProgress(surface)
	return autoContinueCompletedNotice(surface)
}

func (s *Service) maybeDispatchPendingAutoContinue(surface *state.SurfaceConsoleRecord, now time.Time) []control.UIEvent {
	if surface == nil || !surface.AutoContinue.Enabled || surface.AutoContinue.PendingReason == "" || surface.AutoContinue.PendingDueAt.IsZero() {
		return nil
	}
	if now.Before(surface.AutoContinue.PendingDueAt) {
		return nil
	}
	if !s.autoContinueSurfaceReady(surface) {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.pendingRemote[inst.InstanceID] != nil {
		return nil
	}
	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	if !createThread && threadID == "" {
		return nil
	}
	if createThread && strings.TrimSpace(cwd) == "" {
		return nil
	}

	replyToMessageID := surface.AutoContinue.PendingReplyToMessageID
	replyToMessagePreview := surface.AutoContinue.PendingReplyToMessagePreview
	reason := surface.AutoContinue.PendingReason
	count := surface.AutoContinue.ConsecutiveCount
	s.clearAutoContinuePending(surface)
	events := make([]control.UIEvent, 0, 2)
	if reason == state.AutoContinueReasonIncompleteStop {
		events = append(events, autoContinueWhipStartedNotice(surface, count)...)
	}
	events = append(events, s.enqueueAutoContinueQueueItem(
		surface,
		replyToMessageID,
		replyToMessagePreview,
		[]agentproto.Input{{Type: agentproto.InputText, Text: autoContinuePromptText}},
		threadID,
		cwd,
		routeMode,
		surface.PromptOverride,
		false,
	)...)
	return events
}
