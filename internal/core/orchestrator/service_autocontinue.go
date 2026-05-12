package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const autoContinuePromptText = "上一次响应因上游推理中断，请从中断处继续完成当前任务；如果其实已经完成，请直接说明结果。"

func activeAutoContinueEpisode(surface *state.SurfaceConsoleRecord) *state.PendingAutoContinueEpisodeRecord {
	if surface == nil {
		return nil
	}
	return surface.AutoContinue.Episode
}

func (s *Service) nextAutoContinueEpisodeToken() string {
	s.nextAutoContinueEpisodeID++
	return fmt.Sprintf("autocontinue-%d", s.nextAutoContinueEpisodeID)
}

func autoContinueBackoff(consecutiveDryFailures int) (time.Duration, int, bool) {
	delays := []time.Duration{0, 0, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	if consecutiveDryFailures <= 0 || consecutiveDryFailures > len(delays) {
		return 0, len(delays), false
	}
	return delays[consecutiveDryFailures-1], len(delays), true
}

func (s *Service) autoContinueDispatchReady(surface *state.SurfaceConsoleRecord) bool {
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
	if s.progress.surfaceHasPendingCompact(surface) || s.surfaceHasPendingSteer(surface) {
		return false
	}
	return true
}

func autoContinueDelayText(delay time.Duration) string {
	return formatAutoRetryDelay(delay)
}

func autoContinueEpisodeSelectedThreadID(episode *state.PendingAutoContinueEpisodeRecord) string {
	if episode == nil {
		return ""
	}
	dispatchPlan := autoContinueEpisodePromptDispatchPlan(episode)
	if dispatchPlan.SurfaceBindingPolicy == agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		return strings.TrimSpace(dispatchPlan.EffectiveSourceThreadID())
	}
	return strings.TrimSpace(dispatchPlan.EffectiveExecutionThreadID())
}

func (s *Service) autoContinueStatusCardEvent(surface *state.SurfaceConsoleRecord, episode *state.PendingAutoContinueEpisodeRecord) eventcontract.Event {
	if surface == nil || episode == nil {
		return eventcontract.Event{}
	}
	title := "自动继续"
	theme := "progress"
	lines := []string{}
	sealed := false
	switch episode.State {
	case state.AutoContinueEpisodeScheduled:
		title = "等待自动继续"
		if episode.PendingDueAt.IsZero() || !episode.PendingDueAt.After(time.Time{}) {
			lines = append(lines, fmt.Sprintf("上游推理中断，准备开始第 %d 次自动继续。", episode.AttemptCount))
		} else {
			delay := episode.PendingDueAt.Sub(s.now())
			if delay < 0 {
				delay = 0
			}
			lines = append(lines, fmt.Sprintf("上游推理中断，计划在 %s 后开始第 %d 次自动继续。", autoContinueDelayText(delay), episode.AttemptCount))
		}
	case state.AutoContinueEpisodeRunning:
		title = "正在自动继续"
		lines = append(lines, fmt.Sprintf("上游推理中断，已开始第 %d 次自动继续。", episode.AttemptCount))
	case state.AutoContinueEpisodeCompleted:
		title = "自动继续完成"
		theme = "success"
		sealed = true
		lines = append(lines, "当前自动继续已完成。")
	case state.AutoContinueEpisodeCancelled:
		title = "已停止自动继续"
		theme = "info"
		sealed = true
		lines = append(lines, "当前自动继续已停止。")
	case state.AutoContinueEpisodeFailed:
		title = "自动继续失败"
		theme = "error"
		sealed = true
		lines = append(lines, fmt.Sprintf("自动继续已连续失败 %d 次，已停止继续重试。", episode.AttemptCount))
	default:
		lines = append(lines, "自动继续状态已更新。")
	}
	if episode.LastProblem != nil && strings.TrimSpace(episode.LastProblem.Message) != "" {
		lines = append(lines, episode.LastProblem.Message)
	}
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:       title,
		MessageID:   autoContinueStatusMessageID(surface, episode),
		TrackingKey: strings.TrimSpace(episode.EpisodeID),
		ThemeKey:    theme,
		Patchable:   true,
		BodySections: []control.FeishuCardTextSection{{
			Lines: lines,
		}},
		Interactive: false,
		Sealed:      sealed,
	})
	return eventcontract.NewEventFromPayload(
		eventcontract.PagePayload{View: view},
		eventcontract.EventMeta{
			Target: eventcontract.TargetRef{
				GatewayID:        strings.TrimSpace(surface.GatewayID),
				SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
			},
			SourceMessageID:      strings.TrimSpace(episode.RootReplyToMessageID),
			SourceMessagePreview: strings.TrimSpace(episode.RootReplyToMessagePreview),
			MessageDelivery:      eventcontract.ReplyThreadPatchTailDelivery(),
		},
	)
}

func autoContinueStatusMessageID(surface *state.SurfaceConsoleRecord, episode *state.PendingAutoContinueEpisodeRecord) string {
	if !autoContinueEpisodeCanPatchTail(surface, episode) {
		return ""
	}
	return strings.TrimSpace(episode.NoticeMessageID)
}

func (s *Service) maybeScheduleAutoContinueAfterOutcome(outcome *remoteTurnOutcome) []eventcontract.Event {
	if outcome == nil || outcome.Surface == nil || outcome.Item == nil || outcome.Cause != terminalCauseAutoContinueEligible {
		return nil
	}
	surface := outcome.Surface
	if !surface.AutoContinue.Enabled {
		return nil
	}
	episode := activeAutoContinueEpisode(surface)
	continuing := false
	if episode != nil && strings.TrimSpace(outcome.Binding.AutoContinueEpisodeID) != "" && strings.TrimSpace(episode.EpisodeID) == strings.TrimSpace(outcome.Binding.AutoContinueEpisodeID) {
		continuing = true
	}
	if !continuing || episode == nil {
		dispatchPlan := queuedItemPromptDispatchPlan(outcome.Item)
		if binding := outcome.Binding; binding != nil {
			dispatchPlan = remoteBindingPromptDispatchPlan(binding)
		}
		episode = &state.PendingAutoContinueEpisodeRecord{
			EpisodeID:                 s.nextAutoContinueEpisodeToken(),
			InstanceID:                outcome.InstanceID,
			FrozenDispatchPlan:        dispatchPlan,
			FrozenRouteMode:           outcome.Item.RouteModeAtEnqueue,
			FrozenOverride:            outcome.Item.FrozenOverride,
			FrozenPlanMode:            outcome.Item.FrozenPlanMode,
			RootReplyToMessageID:      strings.TrimSpace(firstNonEmpty(outcome.Binding.ReplyToMessageID, outcome.Item.ReplyToMessageID, outcome.Item.SourceMessageID)),
			RootReplyToMessagePreview: strings.TrimSpace(firstNonEmpty(outcome.Binding.ReplyToMessagePreview, outcome.Item.ReplyToMessagePreview, outcome.Item.SourceMessagePreview)),
			TriggerKind:               state.AutoContinueTriggerKindEligibleFailure,
		}
		surface.AutoContinue.Episode = episode
	}
	episode.LastTurnID = strings.TrimSpace(outcome.TurnID)
	episode.LastProblem = cloneProblem(outcome.Problem)
	if threadID := strings.TrimSpace(outcome.ThreadID); threadID != "" {
		setAutoContinueEpisodeExecutionThreadID(episode, threadID)
	}
	dispatchPlan := autoContinueEpisodePromptDispatchPlan(episode)
	if binding := outcome.Binding; binding != nil {
		bindingPlan := remoteBindingPromptDispatchPlan(binding)
		if strings.TrimSpace(bindingPlan.SourceThreadID) != "" {
			dispatchPlan.SourceThreadID = strings.TrimSpace(bindingPlan.SourceThreadID)
		}
		if strings.TrimSpace(bindingPlan.CWD) != "" {
			dispatchPlan.CWD = strings.TrimSpace(bindingPlan.CWD)
		}
		setAutoContinueEpisodePromptDispatchPlan(episode, dispatchPlan)
	}
	if strings.TrimSpace(outcome.Binding.ReplyToMessageID) != "" {
		episode.RootReplyToMessageID = strings.TrimSpace(outcome.Binding.ReplyToMessageID)
		episode.RootReplyToMessagePreview = strings.TrimSpace(outcome.Binding.ReplyToMessagePreview)
	}

	dryFailures := 1
	if continuing && !outcome.AnyOutputSeen {
		dryFailures = episode.ConsecutiveDryFailureCount + 1
	}
	episode.ConsecutiveDryFailureCount = dryFailures
	delay, _, ok := autoContinueBackoff(dryFailures)
	if !ok {
		if outcome.AnyOutputSeen {
			episode.NoticeMessageID = ""
			episode.NoticeAppendSeq = 0
		}
		episode.State = state.AutoContinueEpisodeFailed
		episode.PendingDueAt = time.Time{}
		return []eventcontract.Event{s.autoContinueFailureEvent(surface, episode)}
	}
	if outcome.AnyOutputSeen {
		episode.NoticeMessageID = ""
		episode.NoticeAppendSeq = 0
	}
	episode.AttemptCount++
	episode.CurrentAttemptOutputSeen = false
	episode.PendingDueAt = s.now().Add(delay)
	episode.State = state.AutoContinueEpisodeScheduled
	return []eventcontract.Event{s.autoContinueStatusCardEvent(surface, episode)}
}

func (s *Service) autoContinueFailureEvent(surface *state.SurfaceConsoleRecord, episode *state.PendingAutoContinueEpisodeRecord) eventcontract.Event {
	return s.autoContinueStatusCardEvent(surface, episode)
}

func (s *Service) maybeDispatchPendingAutoContinue(surface *state.SurfaceConsoleRecord, now time.Time) []eventcontract.Event {
	episode := activeAutoContinueEpisode(surface)
	if episode == nil || !surface.AutoContinue.Enabled || episode.State != state.AutoContinueEpisodeScheduled {
		return nil
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" || strings.TrimSpace(surface.AttachedInstanceID) != strings.TrimSpace(episode.InstanceID) {
		clearAutoContinueRuntime(surface)
		return nil
	}
	switch episode.FrozenRouteMode {
	case state.RouteModeNewThreadReady:
		if surface.RouteMode != state.RouteModeNewThreadReady || strings.TrimSpace(surface.PreparedThreadCWD) != strings.TrimSpace(autoContinueEpisodePromptDispatchPlan(episode).CWD) {
			clearAutoContinueRuntime(surface)
			return nil
		}
	default:
		if expectedThreadID := autoContinueEpisodeSelectedThreadID(episode); expectedThreadID != "" && strings.TrimSpace(surface.SelectedThreadID) != expectedThreadID {
			clearAutoContinueRuntime(surface)
			return nil
		}
	}
	if !episode.PendingDueAt.IsZero() && now.Before(episode.PendingDueAt) {
		return nil
	}
	if !s.autoContinueDispatchReady(surface) {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || s.turns.pendingRemote[inst.InstanceID] != nil || surface.ActiveQueueItemID != "" {
		return nil
	}
	return s.dispatchAutoContinueEpisode(surface, inst, episode)
}

func (s *Service) dispatchAutoContinueEpisode(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, episode *state.PendingAutoContinueEpisodeRecord) []eventcontract.Event {
	if surface == nil || inst == nil || episode == nil {
		return nil
	}
	if events, restarting := s.maybeRestartClaudeHeadlessForPrompt(surface, inst, episode.FrozenOverride, autoContinueEpisodePromptDispatchPlan(episode).CWD); restarting {
		return events
	}
	s.nextQueueItemID++
	itemID := fmt.Sprintf("queue-%d", s.nextQueueItemID)
	dispatchPlan := autoContinueEpisodePromptDispatchPlan(episode)
	item := &state.QueueItemRecord{
		ID:                    itemID,
		SurfaceSessionID:      surface.SurfaceSessionID,
		ActorUserID:           surface.ActorUserID,
		SourceKind:            state.QueueItemSourceAutoContinue,
		AutoContinueEpisodeID: episode.EpisodeID,
		ReplyToMessageID:      episode.RootReplyToMessageID,
		ReplyToMessagePreview: episode.RootReplyToMessagePreview,
		Inputs:                []agentproto.Input{{Type: agentproto.InputText, Text: autoContinuePromptText}},
		FrozenDispatchPlan:    dispatchPlan,
		FrozenOverride:        episode.FrozenOverride,
		FrozenPlanMode:        episode.FrozenPlanMode,
		RouteModeAtEnqueue:    episode.FrozenRouteMode,
		Status:                state.QueueItemDispatching,
	}
	surface.QueueItems[item.ID] = item
	binding := newRemoteTurnBindingForQueueItem(surface, inst, item)
	if binding != nil {
		binding.AutoContinueEpisodeID = episode.EpisodeID
		binding.AttemptTriggerKind = string(episode.TriggerKind)
	}
	s.activateSurfaceQueueItemDispatchWithBinding(surface, item, binding)
	episode.State = state.AutoContinueEpisodeRunning
	episode.PendingDueAt = time.Time{}
	episode.CurrentAttemptOutputSeen = false
	return []eventcontract.Event{
		s.autoContinueStatusCardEvent(surface, episode),
		{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command:          s.promptSendCommandFromQueueItem(surface, item, surface.ActorUserID, episode.RootReplyToMessageID),
		},
	}
}

func (s *Service) finishAutoContinueEpisode(outcome *remoteTurnOutcome) {
	if outcome == nil || outcome.Surface == nil {
		return
	}
	episode := activeAutoContinueEpisode(outcome.Surface)
	if episode == nil {
		return
	}
	if strings.TrimSpace(outcome.Binding.AutoContinueEpisodeID) == "" || strings.TrimSpace(episode.EpisodeID) != strings.TrimSpace(outcome.Binding.AutoContinueEpisodeID) {
		return
	}
	if outcome.Cause == terminalCauseCompleted {
		outcome.Surface.AutoContinue.Episode = nil
	}
}

func (s *Service) cancelAutoContinueEpisode(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	episode := activeAutoContinueEpisode(surface)
	if surface == nil || episode == nil {
		return nil
	}
	episode.State = state.AutoContinueEpisodeCancelled
	episode.PendingDueAt = time.Time{}
	episode.CurrentAttemptOutputSeen = false
	return []eventcontract.Event{s.autoContinueStatusCardEvent(surface, episode)}
}
