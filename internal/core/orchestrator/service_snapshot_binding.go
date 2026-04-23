package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) threadFocusEvents(instanceID, threadID string) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	var events []eventcontract.Event
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.maybeRequestThreadRefresh(surface, inst, threadID)...)
	}
	events = append(events, s.reevaluateFollowSurfaces(instanceID)...)
	return events
}

func (s *Service) bindSurfaceToThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []eventcontract.Event {
	return s.bindSurfaceToThreadMode(surface, inst, threadID, state.RouteModePinned)
}

func (s *Service) bindSurfaceToThreadMode(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, routeMode state.RouteMode) []eventcontract.Event {
	if surface == nil || inst == nil || threadID == "" {
		return nil
	}
	thread := s.ensureThread(inst, threadID)
	if !threadVisible(thread) {
		return nil
	}
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimThread(surface, inst, threadID) {
		return nil
	}
	events := s.maybeSealPlanProposalForRouteChange(surface, "当前工作目标已变化，之前的提案计划已失效。")
	events = append(events, s.discardStagedInputsForRouteChange(surface, prevThreadID, prevRouteMode, threadID, routeMode)...)
	surface.SelectedThreadID = threadID
	s.clearPreparedNewThread(surface)
	surface.RouteMode = routeMode
	events = append(events, s.threadSelectionEvents(
		surface,
		threadID,
		string(surface.RouteMode),
		displayThreadTitle(inst, thread, threadID),
		threadPreview(thread),
	)...)
	return events
}

func (s *Service) threadSelectionEvents(surface *state.SurfaceConsoleRecord, threadID, routeMode, title, preview string) []eventcontract.Event {
	firstUserMessage := ""
	lastUserMessage := ""
	lastAssistantMessage := ""
	if surface != nil && strings.TrimSpace(threadID) != "" {
		if inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]; inst != nil {
			if thread := inst.Threads[threadID]; threadVisible(thread) {
				firstUserMessage = threadFirstUserSnippet(thread, 64)
				lastUserMessage = threadLastUserSnippet(thread, 64)
				lastAssistantMessage = threadLastAssistantSnippet(thread, 64)
			}
		}
	}
	if surface.LastSelection != nil &&
		surface.LastSelection.ThreadID == threadID &&
		surface.LastSelection.RouteMode == routeMode {
		surface.LastSelection.Title = title
		surface.LastSelection.Preview = preview
		return nil
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  threadID,
		RouteMode: routeMode,
		Title:     title,
		Preview:   preview,
	}
	return []eventcontract.Event{threadSelectionEvent(surface, threadID, routeMode, title, preview, firstUserMessage, lastUserMessage, lastAssistantMessage)}
}

func notice(surface *state.SurfaceConsoleRecord, code, text string) []eventcontract.Event {
	notice := control.Notice{Code: code, Text: text}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: notice},
		noticeDeliverySemantics(notice, false),
		false,
		"",
		"",
	)}
}

func (s *Service) HandleProblem(instanceID string, problem agentproto.ErrorInfo) []eventcontract.Event {
	return s.handleProblem(instanceID, problem)
}

func (s *Service) handleProblem(instanceID string, problem agentproto.ErrorInfo) []eventcontract.Event {
	problem = problem.Normalize()
	notice := NoticeForProblem(problem)
	surfaces := s.problemTargets(instanceID, problem)
	if len(surfaces) == 0 {
		if inst := s.root.Instances[instanceID]; inst != nil && strings.TrimSpace(problem.ThreadID) != "" {
			s.storeThreadReplayNotice(inst, problem.ThreadID, notice)
		}
		return nil
	}
	if inst := s.root.Instances[instanceID]; inst != nil && strings.TrimSpace(problem.ThreadID) != "" {
		s.clearThreadReplay(inst, problem.ThreadID)
	}
	events := make([]eventcontract.Event, 0, len(surfaces))
	for _, surface := range surfaces {
		if surface == nil {
			continue
		}
		noticeCopy := notice
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			GatewayID:        surface.GatewayID,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           &noticeCopy,
		})
	}
	return events
}

func (s *Service) problemTargets(instanceID string, problem agentproto.ErrorInfo) []*state.SurfaceConsoleRecord {
	if surface := s.root.Surfaces[problem.SurfaceSessionID]; surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if problem.CommandID != "" {
		for _, binding := range s.turns.pendingRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
		for _, binding := range s.turns.activeRemote {
			if binding != nil && binding.CommandID == problem.CommandID {
				if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
					return []*state.SurfaceConsoleRecord{surface}
				}
			}
		}
	}
	if surface := s.turnSurface(instanceID, problem.ThreadID, problem.TurnID); surface != nil {
		return []*state.SurfaceConsoleRecord{surface}
	}
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	return s.findAttachedSurfaces(instanceID)
}

func commandAckProblem(surfaceID string, ack agentproto.CommandAck) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Code:             "command_rejected",
		Layer:            "wrapper",
		Stage:            "command_ack",
		Message:          "本地 Codex 拒绝了这条消息。",
		Details:          strings.TrimSpace(ack.Error),
		SurfaceSessionID: surfaceID,
		CommandID:        ack.CommandID,
	}
	if ack.Problem == nil {
		return defaults.Normalize()
	}
	return ack.Problem.WithDefaults(defaults)
}

func problemFromEvent(event agentproto.Event) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Message:   event.ErrorMessage,
		ThreadID:  event.ThreadID,
		TurnID:    event.TurnID,
		ItemID:    event.ItemID,
		RequestID: event.RequestID,
	}
	if event.Problem == nil {
		return defaults.Normalize()
	}
	return event.Problem.WithDefaults(defaults)
}
