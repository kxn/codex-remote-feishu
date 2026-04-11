package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) finalizeDetachedSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	instanceID := surface.AttachedInstanceID
	s.clearRemoteOwnership(surface)
	s.releaseSurfaceWorkspaceClaim(surface)
	s.releaseSurfaceThreadClaim(surface)
	s.releaseSurfaceInstanceClaim(surface)
	s.clearPreparedNewThread(surface)
	surface.AttachedInstanceID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.Abandoning = false
	surface.DispatchMode = state.DispatchModeNormal
	surface.ActiveTurnOrigin = ""
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	surface.LastSelection = nil
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	if inst := s.root.Instances[instanceID]; inst == nil || !inst.Online {
		return nil
	}
	return s.reevaluateFollowSurfaces(instanceID)
}

func (s *Service) finishSurfaceAfterWork(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if surface.Abandoning && !s.surfaceNeedsDelayedDetach(surface, inst) {
		events := s.finalizeDetachedSurface(surface)
		return append(events, notice(surface, "detached", s.detachedText(surface))...)
	}
	if surface.RouteMode == state.RouteModeFollowLocal && !s.surfaceHasLiveRemoteWork(surface) {
		return s.reevaluateFollowSurface(surface)
	}
	return nil
}

func (s *Service) followLocal(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
		if strings.TrimSpace(surface.AttachedInstanceID) == "" {
			return notice(surface, "follow_deprecated_normal", "normal 模式不再支持 /follow。请先 /list 选择工作区，再 /use 或 /new；如需跟随 VS Code，请先 /mode vscode。")
		}
		return notice(surface, "follow_deprecated_normal", "normal 模式不再支持 /follow。请继续 /use 选择当前工作区会话，或 /new 准备新会话；如需跟随 VS Code，请先 /mode vscode。")
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surfaceHasRouteMutationRequestState(surface) &&
		(surface.RouteMode != state.RouteModeFollowLocal || s.followLocalWouldRetarget(surface, inst)) {
		if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
			return blocked
		}
	}
	events := []control.UIEvent{}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
		events = append(events, s.discardDrafts(surface)...)
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)...)
	s.clearPreparedNewThread(surface)
	surface.RouteMode = state.RouteModeFollowLocal
	reevaluated := s.reevaluateFollowSurface(surface)
	events = append(events, reevaluated...)
	if len(reevaluated) == 0 && surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
		thread := s.ensureThread(inst, surface.SelectedThreadID)
		events = append(events, s.threadSelectionEvents(
			surface,
			surface.SelectedThreadID,
			string(state.RouteModeFollowLocal),
			displayThreadTitle(inst, thread, surface.SelectedThreadID),
			threadPreview(thread),
		)...)
	}
	if len(events) != 0 {
		return events
	}
	return notice(surface, "follow_local_enabled", "已进入跟随模式。后续会尝试跟随当前 VS Code 会话。")
}

func (s *Service) followLocalWouldRetarget(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	if surface == nil || inst == nil || surface.RouteMode != state.RouteModeFollowLocal {
		return true
	}
	if s.surfaceHasLiveRemoteWork(surface) {
		return false
	}
	if inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID) {
		return false
	}
	targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
	if targetThreadID == "" || !threadVisible(inst.Threads[targetThreadID]) {
		return surface.SelectedThreadID != ""
	}
	if owner := s.threadClaimSurface(targetThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return surface.SelectedThreadID != ""
	}
	return surface.SelectedThreadID != targetThreadID || !s.surfaceOwnsThread(surface, targetThreadID)
}

func (s *Service) reevaluateFollowSurfaces(instanceID string) []control.UIEvent {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.reevaluateFollowSurface(surface)...)
	}
	return events
}

func (s *Service) reevaluateFollowSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.Abandoning || surface.AttachedInstanceID == "" || surface.RouteMode != state.RouteModeFollowLocal {
		return nil
	}
	if s.surfaceHasLiveRemoteWork(surface) {
		return nil
	}
	if surfaceHasRouteMutationRequestState(surface) {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return nil
	}
	if inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID) {
		return nil
	}
	targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
	if targetThreadID == "" || !threadVisible(inst.Threads[targetThreadID]) {
		if surface.SelectedThreadID == "" {
			return nil
		}
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
		s.releaseSurfaceThreadClaim(surface)
		return append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
	}
	if owner := s.threadClaimSurface(targetThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		if surface.SelectedThreadID == "" {
			return nil
		}
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
		s.releaseSurfaceThreadClaim(surface)
		return append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
	}
	if surface.SelectedThreadID == targetThreadID && s.surfaceOwnsThread(surface, targetThreadID) {
		return nil
	}
	return s.bindSurfaceToThreadMode(surface, inst, targetThreadID, state.RouteModeFollowLocal)
}

func (s *Service) presentKickThreadPrompt(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, owner *state.SurfaceConsoleRecord) []control.UIEvent {
	thread := inst.Threads[threadID]
	title := displayThreadTitle(inst, thread, threadID)
	subtitle := s.threadSelectionSubtitle(surface, inst, thread)
	return []control.UIEvent{s.feishuDirectSelectionPromptEvent(surface, control.FeishuDirectSelectionPrompt{
		Kind:  control.SelectionPromptKickThread,
		Title: "强踢当前会话？",
		Hint:  "只有对方当前空闲时才能强踢；确认前会再次校验状态。",
		Options: []control.SelectionOption{
			{
				Index:       1,
				OptionID:    "cancel",
				Label:       "保留当前状态，不执行强踢。",
				ButtonLabel: "取消",
			},
			{
				Index:       2,
				OptionID:    threadID,
				Label:       title,
				Subtitle:    subtitle,
				ButtonLabel: "强踢并占用",
			},
		},
	})}
}

func (s *Service) confirmKickThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	events := []control.UIEvent{}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
		events = append(events, s.discardDrafts(surface)...)
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return append(events, notice(surface, "selection_invalid", "缺少目标会话，无法执行强踢。")...)
	}
	owner := s.threadClaimSurface(threadID)
	if owner == nil {
		return append(events, s.useThread(surface, threadID, false)...)
	}
	if owner.SurfaceSessionID == surface.SurfaceSessionID {
		return append(events, s.useThread(surface, threadID, false)...)
	}
	switch s.threadKickStatus(inst, owner, threadID) {
	case threadKickIdle:
		return append(events, s.kickThreadOwner(surface, inst, threadID, owner)...)
	case threadKickQueued:
		return append(events, notice(surface, "thread_busy_queued", "目标会话当前还有排队任务，暂时不能强踢。")...)
	case threadKickRunning:
		return append(events, notice(surface, "thread_busy_running", "目标会话当前正在执行，暂时不能强踢。")...)
	default:
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
}

func (s *Service) kickThreadOwner(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, victim *state.SurfaceConsoleRecord) []control.UIEvent {
	events := s.releaseVictimThread(victim, inst, threadID)
	events = append(events, s.bindSurfaceToThreadMode(surface, inst, threadID, s.surfaceThreadPickRouteMode(surface))...)
	events = append(events, notice(surface, "thread_kicked", "已接管目标会话。原拥有者已退回未绑定状态。")...)
	return events
}

func (s *Service) releaseVictimThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	clearSurfaceRequestsForTurn(surface, threadID, "")
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	routeMode := state.RouteModeUnbound
	title := "未绑定会话"
	events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeUnbound)
	if surface.RouteMode == state.RouteModeFollowLocal {
		routeMode = state.RouteModeFollowLocal
		title = "跟随当前 VS Code（等待中）"
		events = s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
	}
	surface.RouteMode = routeMode
	events = append(events, s.threadSelectionEvents(surface, "", string(routeMode), title, "")...)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "thread_claim_lost",
			Text: "当前会话已被其他飞书会话接管。请重新 /use 选择会话，或等待本地切换。",
		},
	})
	if routeMode == state.RouteModeUnbound {
		events = append(events, s.autoPromptUseThread(surface, inst)...)
	} else {
		events = append(events, s.reevaluateFollowSurface(surface)...)
	}
	return events
}
