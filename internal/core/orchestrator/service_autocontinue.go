package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const autoContinuePromptText = "任务都完成了吗？如果没有就继续干，都完成了就可以停下来"

var autoContinueCompletionPhrases = []string{
	"已完成",
	"已经完成",
	"完成了",
	"done",
	"fixed",
	"resolved",
	"all set",
	"tests passed",
	"已提交",
	"已推送",
}

var autoContinueNeedUserPhrases = []string{
	"请确认",
	"等你确认",
	"需要授权",
	"需要你",
	"请提供",
	"请回复",
	"need approval",
	"need your input",
}

var autoContinueStrongIncompletePhrases = []string{
	"还需要",
	"尚未",
	"还没",
	"未完成",
	"没做完",
	"remaining",
	"still need",
	"todo",
}

var autoContinueNextStepPhrases = []string{
	"下一步",
	"接下来",
	"后续还要",
	"i will next",
	"next step",
}

var autoContinuePausePhrases = []string{
	"先做到这里",
	"暂时先",
	"后面继续",
	"稍后继续",
}

var autoContinueRemainingListPhrases = []string{
	"还需要:",
	"还需要：",
	"todo:",
	"remaining:",
}

func (s *Service) noteAutoContinueAction(surface *state.SurfaceConsoleRecord, action control.Action) {
	if surface == nil {
		return
	}
	switch action.Kind {
	case control.ActionTextMessage,
		control.ActionImageMessage,
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
	replacer := strings.NewReplacer(
		"`", " ",
		"*", " ",
		"_", " ",
		"#", " ",
		"\r", " ",
		"\n", " ",
		"\t", " ",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(strings.TrimSpace(text)))), " ")
}

func containsAnyPhrase(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if phrase != "" && strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func autoContinueIncompleteScore(text string, summary *control.FileChangeSummary) int {
	normalized := normalizeAutoContinueText(text)
	if normalized == "" {
		return 0
	}
	if containsAnyPhrase(normalized, autoContinueCompletionPhrases) || containsAnyPhrase(normalized, autoContinueNeedUserPhrases) {
		return -1
	}

	score := 0
	if containsAnyPhrase(normalized, autoContinueStrongIncompletePhrases) {
		score += 3
	}
	if containsAnyPhrase(normalized, autoContinueNextStepPhrases) {
		score += 2
	}
	if containsAnyPhrase(normalized, autoContinuePausePhrases) {
		score += 2
	}
	if containsAnyPhrase(normalized, autoContinueRemainingListPhrases) {
		score++
	}
	if summary != nil && (containsAnyPhrase(normalized, autoContinueStrongIncompletePhrases) || containsAnyPhrase(normalized, autoContinueNextStepPhrases) || containsAnyPhrase(normalized, autoContinuePausePhrases)) {
		score++
	}
	return score
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
		return notice(surface, "auto_continue_backoff_exhausted", "auto-continue 已达到连续重试上限，已停止本轮自动继续。你可以手动继续，或在新的上下文里再次触发。")
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
	score := autoContinueIncompleteScore(finalText, summary)
	if score >= 3 {
		return s.scheduleAutoContinue(surface, item, turnID, state.AutoContinueReasonIncompleteStop)
	}
	s.resetAutoContinueProgress(surface)
	return nil
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
	s.clearAutoContinuePending(surface)
	return s.enqueueAutoContinueQueueItem(
		surface,
		replyToMessageID,
		replyToMessagePreview,
		[]agentproto.Input{{Type: agentproto.InputText, Text: autoContinuePromptText}},
		threadID,
		cwd,
		routeMode,
		surface.PromptOverride,
		false,
	)
}
