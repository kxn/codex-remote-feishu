package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleCompactCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
		return blocked
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" || !s.surfaceOwnsThread(surface, threadID) || !threadVisible(inst.Threads[threadID]) {
		return notice(surface, "compact_requires_thread", "当前还没有可整理的会话。请先 /use 选择一个会话。")
	}
	if binding := s.turns.compactTurns[inst.InstanceID]; binding != nil {
		if binding.SurfaceSessionID == surface.SurfaceSessionID || binding.ThreadID == threadID {
			return notice(surface, "compact_in_progress", "当前正在整理上下文，请稍候。")
		}
		return notice(surface, "compact_busy", "当前实例正在处理其他工作，暂时不能整理上下文。")
	}
	if binding := s.turns.pendingRemote[inst.InstanceID]; binding != nil {
		return notice(surface, "compact_busy", "当前实例正在处理其他工作，暂时不能整理上下文。")
	}
	if inst.ActiveTurnID != "" {
		return notice(surface, "compact_busy", "当前已有正在执行的任务，暂时不能整理上下文。请等待完成或先 /stop。")
	}
	if s.surfaceHasPendingSteer(surface) {
		return notice(surface, "compact_busy", "当前正在把排队输入并入本轮执行，暂时不能整理上下文。")
	}
	if surface.ActiveQueueItemID != "" {
		return notice(surface, "compact_busy", "当前请求正在派发或执行，暂时不能整理上下文。请等待完成或先 /stop。")
	}
	if len(surface.QueuedQueueItemIDs) != 0 {
		return notice(surface, "compact_busy", "当前还有排队消息，暂时不能整理上下文。请等待队列清空或先 /stop。")
	}
	s.clearThreadHistoryRuntime(surface)
	s.clearTargetPickerRuntime(surface)
	flow := newOwnerCardFlowRecord(
		ownerCardFlowKindCompact,
		s.pickers.nextCompactFlowToken(),
		firstNonEmpty(surface.ActorUserID),
		s.now(),
		defaultCompactOwnerTTL,
		ownerCardFlowPhaseLoading,
	)
	if commandCardOwnsInlineResult(action) {
		flow.MessageID = strings.TrimSpace(action.MessageID)
	}
	s.setActiveOwnerCardFlow(surface, flow)
	binding := &compactTurnBinding{
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
		FlowID:           flow.FlowID,
		ThreadID:         threadID,
		Status:           compactTurnStatusDispatching,
	}
	s.turns.compactTurns[inst.InstanceID] = binding
	events := s.emitCompactOwnerDispatching(surface, binding)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandThreadCompactStart,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: "",
			},
			Target: agentproto.Target{
				ThreadID: threadID,
			},
		},
	})
	return events
}

func (s *Service) promoteCompactTurn(instanceID string, event agentproto.Event) []eventcontract.Event {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	binding := s.turns.compactTurns[instanceID]
	if binding == nil || strings.TrimSpace(binding.TurnID) != "" {
		return nil
	}
	if event.Initiator.Kind != agentproto.InitiatorRemoteSurface || strings.TrimSpace(event.Initiator.SurfaceSessionID) == "" {
		return nil
	}
	if binding.SurfaceSessionID != event.Initiator.SurfaceSessionID {
		return nil
	}
	if binding.ThreadID != "" && strings.TrimSpace(event.ThreadID) != "" && binding.ThreadID != event.ThreadID {
		return nil
	}
	binding.ThreadID = firstNonEmpty(binding.ThreadID, strings.TrimSpace(event.ThreadID))
	binding.TurnID = strings.TrimSpace(event.TurnID)
	binding.Status = compactTurnStatusRunning
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	return s.emitCompactOwnerRunning(surface, binding)
}

func (s *Service) completeCompactTurn(instanceID string, event agentproto.Event) []eventcontract.Event {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(event.TurnID) == "" {
		return nil
	}
	binding := s.turns.compactTurns[instanceID]
	if binding == nil || strings.TrimSpace(binding.TurnID) == "" || binding.TurnID != strings.TrimSpace(event.TurnID) {
		return nil
	}
	if binding.ThreadID != "" && strings.TrimSpace(event.ThreadID) != "" && binding.ThreadID != event.ThreadID {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	delete(s.turns.compactTurns, instanceID)
	if surface == nil {
		return nil
	}
	events := []eventcontract.Event{}
	if !binding.CompletionSeen {
		if turnCompletedSuccessfully(event) {
			events = append(events, s.emitCompactOwnerCompleted(surface, binding)...)
		} else {
			detail := strings.TrimSpace(event.ErrorMessage)
			if event.Problem != nil {
				detail = compactFailureText(*event.Problem, detail)
			}
			if detail == "" {
				detail = "上下文整理在完成前中断。"
			}
			events = append(events, s.emitCompactOwnerFailed(surface, binding, detail)...)
		}
	}
	events = append(events, s.dispatchNext(surface)...)
	return append(events, s.finishSurfaceAfterWork(surface)...)
}

func (s *Service) failCompactTurn(instanceID string, fallback string, problem *agentproto.ErrorInfo, resumeSurface bool) []eventcontract.Event {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	binding := s.turns.compactTurns[instanceID]
	if binding == nil {
		return nil
	}
	delete(s.turns.compactTurns, instanceID)
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		return nil
	}
	detail := strings.TrimSpace(fallback)
	if problem != nil {
		detail = compactFailureText(*problem, detail)
	}
	if detail == "" {
		detail = "上下文整理未完成。"
	}
	events := s.emitCompactOwnerFailed(surface, binding, detail)
	if resumeSurface {
		events = append(events, s.dispatchNext(surface)...)
		events = append(events, s.finishSurfaceAfterWork(surface)...)
	}
	return events
}

func (s *Service) restorePendingCompactDispatch(surfaceID, commandID, noticeCode string, err error) []eventcontract.Event {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" || strings.TrimSpace(commandID) == "" {
		return nil
	}
	binding := s.turns.compactTurns[surface.AttachedInstanceID]
	if binding == nil || binding.SurfaceSessionID != surfaceID || binding.CommandID != commandID {
		return nil
	}
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
		Code:             noticeCode,
		Layer:            "daemon",
		Stage:            "dispatch_command",
		Operation:        "thread.compact.start",
		Message:          "上下文整理请求未成功发送到本地 Codex。",
		SurfaceSessionID: surfaceID,
		CommandID:        commandID,
		ThreadID:         binding.ThreadID,
	})
	return s.failCompactTurn(surface.AttachedInstanceID, "上下文整理请求未成功发送到本地 Codex。", &problem, true)
}

func (s *Service) restorePendingCompactCommand(instanceID, commandID string, problem agentproto.ErrorInfo) []eventcontract.Event {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(commandID) == "" {
		return nil
	}
	binding := s.turns.compactTurns[instanceID]
	if binding == nil || binding.CommandID != commandID {
		return nil
	}
	problem = problem.WithDefaults(agentproto.ErrorInfo{
		Code:             "command_rejected",
		Layer:            "wrapper",
		Stage:            "command_ack",
		Operation:        "thread.compact.start",
		Message:          "本地 Codex 拒绝了这次上下文整理请求。",
		SurfaceSessionID: binding.SurfaceSessionID,
		CommandID:        commandID,
		ThreadID:         binding.ThreadID,
	})
	return s.failCompactTurn(instanceID, "本地 Codex 拒绝了这次上下文整理请求。", &problem, true)
}

func (s *Service) handleCompactProblem(instanceID string, problem agentproto.ErrorInfo) []eventcontract.Event {
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(problem.Operation) != "thread.compact.start" {
		return nil
	}
	binding := s.turns.compactTurns[instanceID]
	if binding == nil || strings.TrimSpace(binding.TurnID) != "" {
		return nil
	}
	if strings.TrimSpace(problem.SurfaceSessionID) != "" && binding.SurfaceSessionID != problem.SurfaceSessionID {
		return nil
	}
	if strings.TrimSpace(problem.ThreadID) != "" && binding.ThreadID != "" && binding.ThreadID != problem.ThreadID {
		return nil
	}
	return s.failCompactTurn(instanceID, "上下文整理在启动前中断。", &problem, true)
}
