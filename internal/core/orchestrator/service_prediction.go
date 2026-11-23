package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// One ephemeral record per surface also remembers consumed/expired source turns.
type predictionRecord struct {
	ID, SurfaceID, InstanceID, ThreadID, SourceTurnID, ActorUserID string
	Suggestion                                                     string
	Pending, Draft, Canceled                                       bool
	EditedText, MessageID, SendQueueItemID, SendCommandID          string
	Submitting                                                     bool
}

func (s *Service) predictionTargetCurrent(p *predictionRecord) bool {
	if p == nil || p.Canceled {
		return false
	}
	surface := s.root.Surfaces[p.SurfaceID]
	ownSubmission := predictionOwnsSubmissionQueue(p, surface)
	if surface == nil || surface.AttachedInstanceID != p.InstanceID || surface.SelectedThreadID != p.ThreadID ||
		!s.surfaceIsHeadless(surface) || s.surfaceBackend(surface) != agentproto.BackendCodex || surface.Abandoning ||
		(surface.RouteMode != state.RouteModePinned && surface.RouteMode != state.RouteModeFollowLocal) ||
		surface.PendingHeadless != nil || (!ownSubmission && (surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 0)) || activePendingRequest(surface) != nil {
		return false
	}
	inst := s.root.Instances[p.InstanceID]
	if inst == nil || !inst.Online || inst.ActiveTurnID != "" || (!ownSubmission && s.surfaceHasLiveRemoteWork(surface)) {
		return false
	}
	thread := inst.Threads[p.ThreadID]
	if !ordinaryThreadVisible(thread) || thread.ForkedFromID != "" || threadRuntimeActive(thread) {
		return false
	}
	if source := thread.Source; source != nil {
		if source.ParentThreadID != "" {
			return false
		}
		switch source.Kind {
		case agentproto.ThreadSourceKindCompact, agentproto.ThreadSourceKindThreadSpawn,
			agentproto.ThreadSourceKindMemoryConsolidation, agentproto.ThreadSourceKindSubAgentOther:
			return false
		}
	}
	if goal := thread.ThreadGoal; goal != nil && !goal.Cleared && goal.Status == "active" {
		return false
	}
	if ui := s.surfaceUIRuntimeState(surface); ui != nil {
		for _, question := range ui.AsyncUserInputs {
			if question != nil && question.ThreadID == p.ThreadID && !question.Submitted {
				return false
			}
		}
	}
	return true
}

func (s *Service) maybeRequestPrediction(outcome *remoteTurnOutcome) []eventcontract.Event {
	if outcome == nil || outcome.Surface == nil || outcome.Item == nil || outcome.Item.SourceKind != state.QueueItemSourceUser || outcome.Status != "completed" || outcome.ErrorMessage != "" || outcome.Problem != nil {
		return nil
	}
	surface := outcome.Surface
	inst := s.root.Instances[outcome.InstanceID]
	if inst == nil || !inst.CapabilitiesDeclared || !inst.Capabilities.ThreadPrediction || s.predictionUnsupported[outcome.InstanceID] {
		return nil
	}
	if old := s.predictions[surface.SurfaceSessionID]; old != nil && old.InstanceID == outcome.InstanceID && old.SourceTurnID == outcome.TurnID {
		return nil
	}
	p := &predictionRecord{ID: "prediction:" + s.nextAgentCommandID(), SurfaceID: surface.SurfaceSessionID,
		InstanceID: outcome.InstanceID, ThreadID: outcome.ThreadID, SourceTurnID: outcome.TurnID,
		ActorUserID: outcome.Item.ActorUserID, Pending: true}
	if p.ActorUserID == "" || p.SourceTurnID == "" || !s.predictionTargetCurrent(p) {
		return nil
	}
	if s.predictions == nil {
		s.predictions = map[string]*predictionRecord{}
	}
	s.predictions[p.SurfaceID] = p
	return []eventcontract.Event{predictionCommandEvent(p, agentproto.CommandThreadPredictionRequest, p.ID)}
}

func predictionCommandEvent(p *predictionRecord, kind agentproto.CommandKind, id string) eventcontract.Event {
	return eventcontract.Event{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: p.SurfaceID,
		Command: &agentproto.Command{CommandID: id, Kind: kind,
			Origin:     agentproto.Origin{Surface: p.SurfaceID, UserID: p.ActorUserID},
			Target:     agentproto.Target{ThreadID: p.ThreadID},
			Prediction: agentproto.PredictionCommand{InstanceID: p.InstanceID, SourceTurnID: p.SourceTurnID, RequestCommandID: p.ID}}}
}

func (s *Service) invalidatePrediction(p *predictionRecord) []eventcontract.Event {
	if p == nil || p.Canceled {
		return nil
	}
	p.Canceled = true
	p.Suggestion = ""
	if !p.Submitting {
		p.EditedText = ""
	}
	if !p.Pending {
		return nil
	}
	p.Pending = false
	return []eventcontract.Event{predictionCommandEvent(p, agentproto.CommandThreadPredictionCancel, "prediction-cancel:"+p.ID)}
}

func (s *Service) predictionActionPrelude(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	switch action.Kind {
	case control.ActionPredictionAccept, control.ActionPredictionSend, control.ActionStatus,
		control.ActionReactionCreated, control.ActionMessageRecalled:
		return nil
	default:
		return s.invalidatePrediction(s.predictions[surface.SurfaceSessionID])
	}
}

func (s *Service) invalidateInstancePredictions(instanceID string) []eventcontract.Event {
	var events []eventcontract.Event
	for _, p := range s.predictions {
		if p.InstanceID == instanceID {
			events = append(events, s.invalidatePrediction(p)...)
		}
	}
	return events
}

func (s *Service) forgetInstancePredictions(instanceID string) {
	s.invalidateInstancePredictions(instanceID)
	delete(s.predictionUnsupported, instanceID)
}

func (s *Service) predictionEventPrelude(instanceID string, event agentproto.Event) []eventcontract.Event {
	events := s.predictionSubmissionStarted(instanceID, event)
	if event.Kind == agentproto.EventTurnStarted || event.Kind == agentproto.EventThreadFocused {
		return append(events, s.invalidateInstancePredictions(instanceID)...)
	}
	nativeActive := event.Kind == agentproto.EventThreadRuntimeStatusUpdated && event.RuntimeStatus != nil && event.RuntimeStatus.Type == agentproto.ThreadRuntimeStatusTypeActive
	turnCompleted := event.Kind == agentproto.EventTurnCompleted && remoteTurnStartAccepted(remoteTurnCompletionOrigin(event))
	if turnCompleted || nativeActive {
		for _, p := range s.predictions {
			if p.InstanceID == instanceID && p.ThreadID == event.ThreadID && (nativeActive || p.SourceTurnID != event.TurnID) {
				events = append(events, s.invalidatePrediction(p)...)
			}
		}
	}
	return events
}

func (s *Service) expirePredictions() []eventcontract.Event {
	var events []eventcontract.Event
	for _, p := range s.predictions {
		if p.Canceled {
			continue
		}
		if !s.predictionTargetCurrent(p) {
			events = append(events, s.invalidatePrediction(p)...)
		}
	}
	return events
}

func (s *Service) applyPredictionResult(instanceID string, event agentproto.Event) []eventcontract.Event {
	for _, p := range s.predictions {
		if p.ID != event.CommandID || p.InstanceID != instanceID || p.ThreadID != event.ThreadID || !p.Pending || p.Canceled {
			continue
		}
		p.Pending = false
		if !s.predictionTargetCurrent(p) {
			p.Canceled = true
			return nil
		}
		if result := event.Prediction; result != nil && result.Unsupported {
			p.Canceled = true
			if s.predictionUnsupported == nil {
				s.predictionUnsupported = map[string]bool{}
			}
			if s.predictionUnsupported[instanceID] {
				return nil
			}
			s.predictionUnsupported[instanceID] = true
			return notice(s.root.Surfaces[p.SurfaceID], "predictions_unsupported", "当前 Codex 不支持下一句建议；本次连接已停止请求建议。普通对话可以继续。")
		}
		if event.ErrorMessage != "" || event.Prediction == nil || event.Prediction.SourceTurnID != p.SourceTurnID {
			p.Canceled = true
			return nil
		}
		p.Suggestion = event.Prediction.Suggestion
		if p.Suggestion == "" {
			p.Canceled = true
			return nil
		}
		return []eventcontract.Event{s.pageEvent(s.root.Surfaces[p.SurfaceID], predictionSuggestionPage(p))}
	}
	return nil
}

func (s *Service) handlePredictionAction(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	p := s.predictions[surface.SurfaceSessionID]
	if p == nil || p.ID != action.PredictionID || p.ActorUserID != action.ActorUserID || p.Pending || p.Suggestion == "" ||
		!s.predictionTargetCurrent(p) {
		return notice(surface, "prediction_expired", "这条建议已失效或不属于当前用户，请继续直接输入消息。")
	}
	if p.Submitting {
		return notice(surface, "prediction_submitting", "这条消息已在排队或发送中，请等待结果。")
	}
	if action.Kind == control.ActionPredictionAccept {
		if !p.Draft {
			p.EditedText = p.Suggestion
		}
		p.Draft = true
		p.MessageID = action.MessageID
		return []eventcontract.Event{s.pageEvent(surface, predictionDraftCallbackPage(p))}
	}
	if !p.Draft {
		return notice(surface, "prediction_not_adopted", "请先采用建议并编辑，再点击发送。")
	}
	text := strings.TrimSpace(action.Text)
	if text == "" {
		p.EditedText, p.MessageID = "", action.MessageID
		view := predictionDraftCallbackPage(p)
		view.StatusText = "请输入要发送的内容。"
		return []eventcontract.Event{s.pageEvent(surface, view)}
	}
	p.EditedText, p.MessageID, p.Submitting = text, action.MessageID, true
	events := s.enqueuePredictionText(surface, action, p)
	if p.SendQueueItemID == "" {
		p.Submitting = false
		if s.predictionTargetCurrent(p) {
			view := predictionDraftCallbackPage(p)
			if len(events) == 1 && events[0].Notice != nil {
				view.StatusText = events[0].Notice.Text
				events = nil
			}
			events = append([]eventcontract.Event{s.pageEvent(surface, view)}, events...)
		}
	} else {
		// Patch before dispatch; an unchanged callback cannot overwrite failure recovery.
		events = append([]eventcontract.Event{s.pageEvent(surface, predictionSubmittedPage(p, false))}, events...)
	}
	return events
}

func (s *Service) enqueuePredictionText(surface *state.SurfaceConsoleRecord, action control.Action, p *predictionRecord) []eventcontract.Event {
	action.Kind = control.ActionTextMessage
	if blocked := s.blockFeishuRoomNoWorkspaceDataPlane(surface, action); blocked != nil {
		return blocked
	}
	if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
		return blocked
	}
	if surface.ActiveRequestCapture != nil || surface.ReviewSession != nil {
		return notice(surface, "prediction_input_blocked", "请先结束当前的反馈或审阅，再发送这条消息。")
	}
	inst := s.root.Instances[p.InstanceID]
	if surface.ContractRefreshPending && !s.surfaceInstanceCompatibleForAttach(surface, inst) {
		return notice(surface, "contract_refresh_pending", "机器人配置已变化。请复制正文，重新选择会话后发送。")
	}
	threadID, cwd, routeMode, _ := freezeRoute(inst, surface)
	if threadID != p.ThreadID || strings.TrimSpace(cwd) == "" {
		return notice(surface, "thread_not_ready", "原会话暂时无法接收消息，请确认会话和工作目录后重试。")
	}
	threadPolicy := s.freezeCodexThreadPolicyForPrompt(surface)
	if s.codexCrossModelGroupTargetSource(inst, surface, threadID, cwd, surface.PromptOverride, threadPolicy) != "" {
		return notice(surface, "prediction_model_group_changed", "当前模型需要开启新会话。请复制正文，再选择模型或另行发送。")
	}
	s.noteAutoWhipAction(surface, action)
	s.recordInboundSurfaceMessage(surface, action.MessageID, state.SurfaceMessageKindText)
	inputs, stagedMessageIDs := s.stagedPromptInputs(surface, action.ActorUserID)
	inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: p.EditedText})
	s.clearPendingTextInput(surface)
	events := s.maybeSealPlanProposalForInput(surface)
	return append(events, s.enqueueQueueItem(surface, action.MessageID, p.EditedText, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)...)
}

func (s *Service) predictionSubmissionStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	if event.Kind != agentproto.EventTurnStarted || event.TurnID == "" || event.Initiator.Kind == agentproto.InitiatorLocalUI {
		return nil
	}
	binding := s.lookupRemoteTurnForEvent(instanceID, event)
	if binding == nil {
		return nil
	}
	p := s.predictions[binding.SurfaceSessionID]
	if p == nil || !p.Submitting || p.InstanceID != instanceID || p.ThreadID != event.ThreadID ||
		p.SendCommandID == "" || p.SendCommandID != binding.CommandID || p.SendQueueItemID != binding.QueueItemID {
		return nil
	}
	if event.TurnID == p.SourceTurnID || (event.CommandID != "" && event.CommandID != p.SendCommandID) ||
		(event.Initiator.SurfaceSessionID != "" && event.Initiator.SurfaceSessionID != p.SurfaceID) {
		return nil
	}
	view := predictionSubmittedPage(p, true)
	p.SendCommandID, p.EditedText = "", ""
	return []eventcontract.Event{s.pageEvent(s.root.Surfaces[p.SurfaceID], view)}
}

func predictionOwnsSubmissionQueue(p *predictionRecord, surface *state.SurfaceConsoleRecord) bool {
	if p == nil || surface == nil || !p.Submitting || p.SendQueueItemID == "" {
		return false
	}
	item := surface.QueueItems[p.SendQueueItemID]
	if item == nil || (item.Status != state.QueueItemQueued && item.Status != state.QueueItemDispatching) {
		return false
	}
	if surface.ActiveQueueItemID != "" && surface.ActiveQueueItemID != item.ID {
		return false
	}
	for _, id := range surface.QueuedQueueItemIDs {
		if id != item.ID {
			return false
		}
	}
	return true
}

func (s *Service) bindPredictionQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord) {
	p := s.predictions[surface.SurfaceSessionID]
	if p != nil && !p.Canceled && p.Submitting && p.SendQueueItemID == "" &&
		item.SourceKind == state.QueueItemSourceUser && item.ActorUserID == p.ActorUserID && item.SourceMessageID == p.MessageID {
		p.SendQueueItemID = item.ID
	}
}

func (s *Service) bindPredictionSendCommand(surface *state.SurfaceConsoleRecord, commandID string) {
	p := s.predictions[surface.SurfaceSessionID]
	if p != nil && p.Submitting && p.SendQueueItemID != "" && surface.ActiveQueueItemID == p.SendQueueItemID {
		p.SendCommandID = commandID
	}
}

func (s *Service) restorePredictionDraftAfterDispatchFailure(surfaceID, commandID string) []eventcontract.Event {
	p := s.predictions[surfaceID]
	surface := s.root.Surfaces[surfaceID]
	if p == nil || surface == nil || p.Canceled || !p.Submitting || p.SendCommandID == "" || p.SendCommandID != commandID {
		return nil
	}
	item := surface.QueueItems[p.SendQueueItemID]
	if item == nil || item.Status != state.QueueItemFailed {
		return nil
	}
	p.Submitting = false
	p.SendQueueItemID, p.SendCommandID = "", ""
	if !s.predictionTargetCurrent(p) {
		s.invalidatePrediction(p)
		return nil
	}
	return []eventcontract.Event{s.pageEvent(surface, predictionDraftPage(p))}
}

func (s *Service) failPredictionDispatch(commandID string) bool {
	if strings.HasPrefix(commandID, "prediction-cancel:") {
		return true
	}
	if !strings.HasPrefix(commandID, "prediction:") {
		return false
	}
	for _, p := range s.predictions {
		if p.ID == commandID {
			p.Canceled = true
			p.Pending = false
			p.Suggestion = ""
		}
	}
	return true
}
