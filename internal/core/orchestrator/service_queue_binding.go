package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) normalizeTurnInitiator(instanceID string, event agentproto.Event) agentproto.Initiator {
	if event.Initiator.Kind != agentproto.InitiatorLocalUI && event.Initiator.Kind != agentproto.InitiatorUnknown {
		return event.Initiator
	}
	if binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID); binding != nil {
		return agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: binding.SurfaceSessionID}
	}
	return event.Initiator
}

func queuedItemMatchesTurn(item *state.QueueItemRecord, threadID string) bool {
	if item == nil {
		return false
	}
	if item.FrozenThreadID != "" {
		return threadID == "" || threadID == item.FrozenThreadID
	}
	return threadID == ""
}

func (s *Service) pendingRemoteBinding(instanceID, threadID string) *remoteTurnBinding {
	binding := s.pendingRemote[instanceID]
	if binding == nil {
		return nil
	}
	surface := s.root.Surfaces[binding.SurfaceSessionID]
	if surface == nil {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	item := surface.QueueItems[binding.QueueItemID]
	if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
		delete(s.pendingRemote, instanceID)
		return nil
	}
	if !queuedItemMatchesTurn(item, threadID) {
		return nil
	}
	return binding
}

func (s *Service) promotePendingRemote(instanceID string, initiator agentproto.Initiator, threadID, turnID string) *remoteTurnBinding {
	binding := s.pendingRemoteBindingForInitiator(instanceID, initiator, threadID)
	if binding == nil {
		return s.activeRemoteBinding(instanceID, turnID)
	}
	delete(s.pendingRemote, instanceID)
	if threadID != "" {
		binding.ThreadID = threadID
	}
	binding.TurnID = turnID
	binding.Status = string(state.QueueItemRunning)
	s.activeRemote[instanceID] = binding
	return binding
}

func (s *Service) pendingRemoteBindingForInitiator(instanceID string, initiator agentproto.Initiator, threadID string) *remoteTurnBinding {
	if initiator.Kind == agentproto.InitiatorRemoteSurface && strings.TrimSpace(initiator.SurfaceSessionID) != "" {
		binding := s.pendingRemote[instanceID]
		if binding == nil {
			return nil
		}
		surface := s.root.Surfaces[binding.SurfaceSessionID]
		if surface == nil {
			delete(s.pendingRemote, instanceID)
			return nil
		}
		item := surface.QueueItems[binding.QueueItemID]
		if item == nil || (item.Status != state.QueueItemDispatching && item.Status != state.QueueItemRunning) {
			delete(s.pendingRemote, instanceID)
			return nil
		}
		if binding.SurfaceSessionID == initiator.SurfaceSessionID {
			return binding
		}
	}
	return s.pendingRemoteBinding(instanceID, threadID)
}

func (s *Service) activeRemoteBinding(instanceID, turnID string) *remoteTurnBinding {
	binding := s.activeRemote[instanceID]
	if binding == nil {
		return nil
	}
	if turnID != "" && binding.TurnID != "" && binding.TurnID != turnID {
		return nil
	}
	return binding
}

func (s *Service) lookupRemoteTurn(instanceID, threadID, turnID string) *remoteTurnBinding {
	if binding := s.activeRemoteBinding(instanceID, turnID); binding != nil {
		if threadID == "" || binding.ThreadID == "" || binding.ThreadID == threadID {
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
	return s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID) != nil
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
		delete(s.activeRemote, instanceID)
	}
	if binding := s.pendingRemote[instanceID]; binding != nil && (turnID == "" || binding.TurnID == turnID) {
		delete(s.pendingRemote, instanceID)
	}
}

func (s *Service) clearRemoteOwnership(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.pendingRemote, surface.AttachedInstanceID)
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.activeRemote, surface.AttachedInstanceID)
	}
}

func (s *Service) remoteBindingForSurface(surface *state.SurfaceConsoleRecord) *remoteTurnBinding {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	if binding := s.pendingRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		return binding
	}
	return nil
}

func (s *Service) interruptibleSurfaceTurn(surface *state.SurfaceConsoleRecord) (threadID, turnID string, ok bool) {
	if surface == nil || surface.AttachedInstanceID == "" {
		return "", "", false
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if binding := s.activeRemote[surface.AttachedInstanceID]; binding != nil && binding.SurfaceSessionID == surface.SurfaceSessionID {
		turnID = strings.TrimSpace(binding.TurnID)
		if turnID != "" {
			activeThreadID := ""
			if inst != nil {
				activeThreadID = inst.ActiveThreadID
			}
			return strings.TrimSpace(firstNonEmpty(binding.ThreadID, activeThreadID)), turnID, true
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
	for _, binding := range s.pendingSteers {
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
