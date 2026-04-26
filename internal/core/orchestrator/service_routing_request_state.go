package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) reconcileInstanceSurfaceThreads(instanceID string) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	var events []eventcontract.Event
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		threadID := strings.TrimSpace(surface.SelectedThreadID)
		if threadID == "" {
			continue
		}
		if threadVisible(inst.Threads[threadID]) && s.surfaceOwnsThread(surface, threadID) {
			continue
		}
		clearSurfaceRequestsForTurn(surface, threadID, "")
		clearAutoContinueRuntime(surface)
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		s.releaseSurfaceThreadClaim(surface)
		switch surface.RouteMode {
		case state.RouteModeFollowLocal:
			events = append(events, s.discardStagedInputsForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)...)
			events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）")...)
			events = append(events, s.reevaluateFollowSurface(surface)...)
		default:
			surface.RouteMode = state.RouteModeUnbound
			events = append(events, s.discardStagedInputsForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeUnbound)...)
			events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeUnbound), "未绑定会话")...)
			events = append(events, eventcontract.Event{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code: "selected_thread_lost",
					Text: "原先绑定的会话已不可用，请重新 /use 选择会话。",
				},
			})
			events = append(events, s.autoPromptUseThread(surface, inst)...)
		}
	}
	return events
}

func clearSurfaceRequests(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	clearSurfaceRequestCapture(surface)
}

func clearSurfaceRequestsForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil {
		return
	}
	if len(surface.PendingRequests) != 0 {
		for requestID, request := range surface.PendingRequests {
			if request == nil {
				delete(surface.PendingRequests, requestID)
				continue
			}
			if turnID != "" && request.TurnID != "" && request.TurnID != turnID {
				continue
			}
			if threadID != "" && request.ThreadID != "" && request.ThreadID != threadID {
				continue
			}
			delete(surface.PendingRequests, requestID)
		}
	}
	clearSurfaceRequestCaptureForTurn(surface, threadID, turnID)
}

func clearSurfaceRequestCapture(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureByRequestID(surface *state.SurfaceConsoleRecord, requestID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	if requestID == "" || surface.ActiveRequestCapture.RequestID != requestID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	capture := surface.ActiveRequestCapture
	if turnID != "" && capture.TurnID != "" && capture.TurnID != turnID {
		return
	}
	if threadID != "" && capture.ThreadID != "" && capture.ThreadID != threadID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func (s *Service) clearRequestsForTurn(instanceID, threadID, turnID string) {
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		clearSurfaceRequestsForTurn(surface, threadID, turnID)
	}
}

func (s *Service) clearTurnArtifacts(instanceID, threadID, turnID string) {
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, threadID, turnID)
	deleteMatchingTurnPlanSnapshots(s.progress.turnPlanSnapshots, instanceID, threadID, turnID)
	deleteMatchingMCPToolCallProgress(s.progress.mcpToolCallProgress, instanceID, threadID, turnID)
	if turnID == "" {
		return
	}
	delete(s.progress.pendingTurnText, turnRenderKey(instanceID, threadID, turnID))
	delete(s.progress.pendingPlanProposal, turnRenderKey(instanceID, threadID, turnID))
	s.clearRequestsForTurn(instanceID, threadID, turnID)
}

func (s *Service) turnSurface(instanceID, threadID, turnID string) *state.SurfaceConsoleRecord {
	if binding := s.lookupRemoteTurn(instanceID, threadID, turnID); binding != nil {
		if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
			return surface
		}
	}
	if surface, _ := s.reviewSessionSurface(instanceID, threadID); surface != nil {
		return surface
	}
	return s.threadClaimSurface(threadID)
}

func (s *Service) surfaceForInitiator(instanceID string, event agentproto.Event) *state.SurfaceConsoleRecord {
	if event.Initiator.Kind == agentproto.InitiatorRemoteSurface && strings.TrimSpace(event.Initiator.SurfaceSessionID) != "" {
		if surface := s.root.Surfaces[event.Initiator.SurfaceSessionID]; surface != nil {
			return surface
		}
	}
	return s.turnSurface(instanceID, event.ThreadID, event.TurnID)
}

func (s *Service) pauseForLocal(instanceID string) []eventcontract.Event {
	var events []eventcontract.Event
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		s.pausedUntil[surface.SurfaceSessionID] = s.now().Add(s.config.LocalPauseMaxWait)
		if surface.DispatchMode == state.DispatchModePausedForLocal {
			continue
		}
		surface.DispatchMode = state.DispatchModePausedForLocal
		events = append(events, notice(surface, "local_activity_detected", "检测到本地 VS Code 正在使用，飞书消息将继续排队。")...)
	}
	return events
}

func (s *Service) enterHandoff(instanceID string) []eventcontract.Event {
	var events []eventcontract.Event
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		if surface.DispatchMode != state.DispatchModePausedForLocal {
			continue
		}
		delete(s.pausedUntil, surface.SurfaceSessionID)
		if len(surface.QueuedQueueItemIDs) == 0 {
			surface.DispatchMode = state.DispatchModeNormal
			delete(s.handoffUntil, surface.SurfaceSessionID)
			continue
		}
		surface.DispatchMode = state.DispatchModeHandoffWait
		s.handoffUntil[surface.SurfaceSessionID] = s.now().Add(s.config.TurnHandoffWait)
	}
	return events
}
