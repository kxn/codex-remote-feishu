package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func normalizeUnspecifiedInitiator(initiator agentproto.Initiator) agentproto.Initiator {
	if strings.TrimSpace(string(initiator.Kind)) == "" {
		initiator.Kind = agentproto.InitiatorUnknown
	}
	return initiator
}

func (s *Service) normalizeTurnInitiator(instanceID string, event agentproto.Event) agentproto.Initiator {
	event.Initiator = normalizeUnspecifiedInitiator(event.Initiator)
	if event.Initiator.Kind != agentproto.InitiatorLocalUI && event.Initiator.Kind != agentproto.InitiatorUnknown {
		return event.Initiator
	}
	if binding := s.lookupRemoteTurnForEvent(instanceID, event); binding != nil {
		return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: binding.SurfaceSessionID}
	}
	return event.Initiator
}

func queuedItemMatchesTurn(item *state.QueueItemRecord, threadID string) bool {
	if item == nil {
		return false
	}
	if executionThreadID := queuedItemExecutionThreadID(item); executionThreadID != "" {
		return threadID == "" || threadID == executionThreadID
	}
	if item.RouteModeAtEnqueue == state.RouteModeNewThreadReady {
		return true
	}
	return threadID == ""
}

func queuedItemSourceThreadID(item *state.QueueItemRecord) string {
	return queuedItemPromptDispatchPlan(item).EffectiveSourceThreadID()
}

func queuedItemSurfaceBindingPolicy(item *state.QueueItemRecord) agentproto.SurfaceBindingPolicy {
	return queuedItemPromptDispatchPlan(item).SurfaceBindingPolicy
}

func queuedItemExecutionThreadID(item *state.QueueItemRecord) string {
	return queuedItemPromptDispatchPlan(item).ExecutionThreadID
}

func setQueuedItemPromptDispatchPlan(item *state.QueueItemRecord, plan agentproto.PromptDispatchPlan) {
	if item == nil {
		return
	}
	item.FrozenDispatchPlan = agentproto.NormalizePromptDispatchPlan(plan)
}

func setQueuedItemExecutionThreadID(item *state.QueueItemRecord, threadID string) {
	if item == nil {
		return
	}
	plan := queuedItemPromptDispatchPlan(item)
	plan.ExecutionThreadID = strings.TrimSpace(threadID)
	setQueuedItemPromptDispatchPlan(item, plan)
}

func remoteBindingExecutionThreadID(binding *remoteTurnBinding) string {
	return remoteBindingPromptDispatchPlan(binding).EffectiveExecutionThreadID()
}

func remoteBindingSourceThreadID(binding *remoteTurnBinding) string {
	return remoteBindingPromptDispatchPlan(binding).EffectiveSourceThreadID()
}

func remoteBindingSurfaceBindingPolicy(binding *remoteTurnBinding) agentproto.SurfaceBindingPolicy {
	return remoteBindingPromptDispatchPlan(binding).SurfaceBindingPolicy
}

func remoteBindingKeepsSurfaceSelection(binding *remoteTurnBinding) bool {
	return remoteBindingSurfaceBindingPolicy(binding) == agentproto.SurfaceBindingPolicyKeepSurfaceSelection
}

func remoteBindingSurfaceThreadID(binding *remoteTurnBinding) string {
	return remoteBindingPromptDispatchPlan(binding).EffectiveSurfaceThreadID()
}

func setRemoteBindingPromptDispatchPlan(binding *remoteTurnBinding, plan agentproto.PromptDispatchPlan) {
	if binding == nil {
		return
	}
	binding.DispatchPlan = agentproto.NormalizePromptDispatchPlan(plan)
}

func setRemoteBindingExecutionThreadID(binding *remoteTurnBinding, threadID string) {
	if binding == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	binding.ThreadID = threadID
	if threadID == "" {
		return
	}
	plan := remoteBindingPromptDispatchPlan(binding)
	plan.ExecutionThreadID = threadID
	if plan.SurfaceBindingPolicy == agentproto.SurfaceBindingPolicyFollowExecutionThread || strings.TrimSpace(plan.SourceThreadID) == "" {
		plan.SourceThreadID = threadID
	}
	setRemoteBindingPromptDispatchPlan(binding, plan)
}

func autoContinueEpisodePromptDispatchPlan(episode *state.PendingAutoContinueEpisodeRecord) agentproto.PromptDispatchPlan {
	if episode == nil {
		return agentproto.DefaultPromptDispatchPlanForExecutionThread("")
	}
	return agentproto.NormalizePromptDispatchPlan(episode.FrozenDispatchPlan)
}

func setAutoContinueEpisodePromptDispatchPlan(episode *state.PendingAutoContinueEpisodeRecord, plan agentproto.PromptDispatchPlan) {
	if episode == nil {
		return
	}
	episode.FrozenDispatchPlan = agentproto.NormalizePromptDispatchPlan(plan)
}

func setAutoContinueEpisodeExecutionThreadID(episode *state.PendingAutoContinueEpisodeRecord, threadID string) {
	if episode == nil {
		return
	}
	plan := autoContinueEpisodePromptDispatchPlan(episode)
	plan.ExecutionThreadID = strings.TrimSpace(threadID)
	setAutoContinueEpisodePromptDispatchPlan(episode, plan)
}

func queuedItemPromptDispatchPlan(item *state.QueueItemRecord) agentproto.PromptDispatchPlan {
	if item == nil {
		return agentproto.DefaultPromptDispatchPlanForExecutionThread("")
	}
	return agentproto.NormalizePromptDispatchPlan(item.FrozenDispatchPlan)
}

func remoteBindingPromptDispatchPlan(binding *remoteTurnBinding) agentproto.PromptDispatchPlan {
	if binding == nil {
		return agentproto.DefaultPromptDispatchPlanForExecutionThread("")
	}
	plan := agentproto.NormalizePromptDispatchPlan(binding.DispatchPlan)
	if strings.TrimSpace(binding.ThreadID) != "" {
		plan.ExecutionThreadID = strings.TrimSpace(binding.ThreadID)
	}
	return agentproto.NormalizePromptDispatchPlan(plan)
}

func shouldRestorePreparedNewThread(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, binding *remoteTurnBinding) bool {
	if surface == nil || item == nil {
		return false
	}
	if item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		return false
	}
	if binding == nil {
		return true
	}
	return binding.BootstrapNewThread && !binding.ThreadCommitted
}

func matchesRemoteBindingThread(binding *remoteTurnBinding, threadID string) bool {
	if binding == nil {
		return false
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return true
	}
	executionThreadID := remoteBindingExecutionThreadID(binding)
	if executionThreadID == "" || executionThreadID == threadID {
		return true
	}
	sourceThreadID := remoteBindingSourceThreadID(binding)
	return sourceThreadID != "" && sourceThreadID == threadID
}

func (s *Service) pendingRemoteBindingRecord(instanceID string) (*remoteTurnBinding, *state.SurfaceConsoleRecord, *state.QueueItemRecord) {
	binding := s.turns.pendingRemote[instanceID]
	if binding == nil {
		return nil, nil, nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		s.clearPendingRemoteTurn(instanceID)
		return nil, nil, nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
		s.clearPendingRemoteTurn(instanceID)
		return nil, nil, nil
	}
	return binding, surface, item
}

func (s *Service) pendingRemoteBinding(instanceID, threadID string) *remoteTurnBinding {
	binding, _, item := s.pendingRemoteBindingRecord(instanceID)
	if binding == nil {
		return nil
	}
	if !queuedItemMatchesTurn(item, threadID) {
		return nil
	}
	return binding
}

func (s *Service) pendingRemoteBindingByCommand(instanceID, commandID string) *remoteTurnBinding {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return nil
	}
	binding, _, _ := s.pendingRemoteBindingRecord(instanceID)
	if binding == nil || strings.TrimSpace(binding.CommandID) != commandID {
		return nil
	}
	return binding
}

func (s *Service) pendingRemoteBindingBySurface(instanceID, surfaceSessionID string) *remoteTurnBinding {
	surfaceSessionID = strings.TrimSpace(surfaceSessionID)
	if surfaceSessionID == "" {
		return nil
	}
	binding, _, _ := s.pendingRemoteBindingRecord(instanceID)
	if binding == nil || strings.TrimSpace(binding.SurfaceSessionID) != surfaceSessionID {
		return nil
	}
	return binding
}

func (s *Service) pendingRemoteBindingForEvent(instanceID string, event agentproto.Event) *remoteTurnBinding {
	if binding := s.pendingRemoteBindingByCommand(instanceID, event.CommandID); binding != nil {
		return binding
	}
	initiator := normalizeUnspecifiedInitiator(event.Initiator)
	if initiator.Kind == agentproto.InitiatorRemoteSurface && strings.TrimSpace(initiator.SurfaceSessionID) != "" {
		if binding := s.pendingRemoteBindingBySurface(instanceID, initiator.SurfaceSessionID); binding != nil {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, event.ThreadID)
}

func (s *Service) promotePendingRemote(instanceID string, event agentproto.Event) *remoteTurnBinding {
	binding := s.pendingRemoteBindingForEvent(instanceID, event)
	if binding == nil {
		return s.activeRemoteBinding(instanceID, event.TurnID)
	}
	s.clearPendingRemoteTurn(instanceID)
	threadID := strings.TrimSpace(event.ThreadID)
	if threadID != "" {
		setRemoteBindingExecutionThreadID(binding, threadID)
	}
	binding.TurnID = strings.TrimSpace(event.TurnID)
	binding.Status = string(state.QueueItemRunning)
	s.bindActiveRemoteTurn(instanceID, binding)
	return binding
}

func (s *Service) activeRemoteBinding(instanceID, turnID string) *remoteTurnBinding {
	binding := s.turns.activeRemote[instanceID]
	if binding == nil {
		return nil
	}
	if turnID != "" && binding.TurnID != "" && binding.TurnID != turnID {
		return nil
	}
	return binding
}

func (s *Service) lookupRemoteTurnForEvent(instanceID string, event agentproto.Event) *remoteTurnBinding {
	if binding := s.activeRemoteBinding(instanceID, event.TurnID); binding != nil {
		if matchesRemoteBindingThread(binding, event.ThreadID) {
			return binding
		}
	}
	return s.pendingRemoteBindingForEvent(instanceID, event)
}

func (s *Service) lookupRemoteTurn(instanceID, threadID, turnID string) *remoteTurnBinding {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		if matchesRemoteBindingThread(binding, threadID) {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, threadID)
}

func (s *Service) shouldTrackInstanceActiveTurn(instanceID string, event agentproto.Event) bool {
	if isInternalHelperEvent(event) {
		return false
	}
	if event.Initiator.Kind == agentproto.InitiatorLocalUI {
		return true
	}
	if inst := s.root.Instances[instanceID]; inst != nil && threadIsReview(inst.Threads[event.ThreadID]) {
		return true
	}
	binding := s.lookupRemoteTurnForEvent(instanceID, event)
	if binding == nil {
		return false
	}
	return !remoteBindingKeepsSurfaceSelection(binding)
}

func shouldClearTrackedInstanceActiveTurn(inst *state.InstanceRecord, threadID, turnID string) bool {
	if inst == nil {
		return false
	}
	activeTurnID := strings.TrimSpace(inst.ActiveTurnID)
	if activeTurnID == "" || activeTurnID != strings.TrimSpace(turnID) {
		return false
	}
	activeThreadID := strings.TrimSpace(inst.ActiveThreadID)
	targetThreadID := strings.TrimSpace(threadID)
	return activeThreadID == "" || targetThreadID == "" || activeThreadID == targetThreadID
}

func (s *Service) clearRemoteTurn(instanceID, turnID string) {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		s.clearActiveRemoteTurn(instanceID)
	}
	if binding := s.turns.pendingRemote[instanceID]; binding != nil && (turnID == "" || binding.TurnID == turnID) {
		s.clearPendingRemoteTurn(instanceID)
	}
}

func (s *Service) markRemoteTurnInterruptRequested(instanceID, threadID, turnID string) {
	binding := s.lookupRemoteTurn(instanceID, threadID, turnID)
	if binding == nil {
		return
	}
	binding.InterruptRequested = true
	if binding.InterruptRequestedAt.IsZero() {
		binding.InterruptRequestedAt = s.now().UTC()
	}
}

func (s *Service) clearRemoteOwnership(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return
	}
	if binding := s.turns.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		s.clearPendingRemoteTurn(surface.AttachedInstanceID)
	}
	if binding := s.turns.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		s.clearActiveRemoteTurn(surface.AttachedInstanceID)
	}
}

func (s *Service) remoteBindingForSurface(surface *state.SurfaceConsoleRecord) *remoteTurnBinding {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if binding := s.turns.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	if binding := s.turns.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	return nil
}

func (s *Service) interruptibleSurfaceTurn(surface *state.SurfaceConsoleRecord) (threadID, turnID string, ok bool) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return "", "", false
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if binding := s.turns.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		turnID = strings.TrimSpace(binding.TurnID)
		if turnID != "" {
			activeThreadID := ""
			if inst != nil {
				activeThreadID = inst.ActiveThreadID
			}
			return strings.TrimSpace(firstNonEmpty(remoteBindingExecutionThreadID(binding), activeThreadID)), turnID, true
		}
	}
	if inst == nil {
		return "", "", false
	}
	turnID = strings.TrimSpace(inst.ActiveTurnID)
	if turnID == "" {
		return "", "", false
	}
	return strings.TrimSpace(inst.ActiveThreadID), turnID, true
}

func (s *Service) surfaceHasPendingSteer(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	for _, binding := range s.turns.pendingSteers {
		if binding == nil || binding.SurfaceSessionID != surface.SurfaceSessionID {
			continue
		}
		for _, queueItemID := range pendingSteerQueueItemIDs(binding) {
			item := surface.QueueItems[queueItemID]
			if item == nil || item.Status != state.QueueItemSteering {
				continue
			}
			return true
		}
	}
	return false
}
