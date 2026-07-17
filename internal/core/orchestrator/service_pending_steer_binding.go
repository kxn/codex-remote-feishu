package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) bindPendingSteer(key string, binding *pendingSteerBinding) {
	s.turns.bindPendingSteer(key, binding)
}

func (s *Service) bindPendingSteerCommand(surfaceID, commandID string) bool {
	if strings.TrimSpace(commandID) == "" {
		return false
	}
	bound := false
	s.turns.forEachPendingSteer(func(_ string, binding *pendingSteerBinding) {
		if bound || binding.SurfaceSessionID != surfaceID || binding.CommandID != "" {
			return
		}
		binding.CommandID = commandID
		bound = true
	})
	return bound
}

func (s *Service) pendingSteerForCommand(instanceID, commandID string) (string, *pendingSteerBinding) {
	if strings.TrimSpace(commandID) == "" {
		return "", nil
	}
	instanceID = strings.TrimSpace(instanceID)
	var matchedKey string
	var matchedBinding *pendingSteerBinding
	s.turns.forEachPendingSteer(func(key string, binding *pendingSteerBinding) {
		if matchedBinding != nil || binding.CommandID != commandID {
			return
		}
		if instanceID != "" && binding.InstanceID != instanceID {
			return
		}
		matchedKey = key
		matchedBinding = binding
	})
	return matchedKey, matchedBinding
}

func (s *Service) clearPendingSteer(key string) {
	s.turns.clearPendingSteer(key)
}

func (s *Service) pendingSteerBinding(key string) *pendingSteerBinding {
	return s.turns.pendingSteerBinding(key)
}

func (s *Service) pendingSteerKeysForInstance(instanceID string) []string {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}
	keys := []string{}
	s.turns.forEachPendingSteer(func(key string, binding *pendingSteerBinding) {
		if binding.InstanceID == instanceID {
			keys = append(keys, key)
		}
	})
	sort.Strings(keys)
	return keys
}

func (s *Service) instanceHasPendingSteer(instanceID string) bool {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return false
	}
	found := false
	s.turns.forEachPendingSteer(func(_ string, binding *pendingSteerBinding) {
		if found || binding.InstanceID != instanceID {
			return
		}
		surface := s.root.Surfaces[binding.SurfaceSessionID]
		if surface != nil && s.surfaceHasPendingSteer(surface) {
			found = true
		}
	})
	return found
}

func (s *Service) surfaceHasPendingSteer(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	found := false
	s.turns.forEachPendingSteer(func(_ string, binding *pendingSteerBinding) {
		if found || binding.SurfaceSessionID != surface.SurfaceSessionID {
			return
		}
		for _, queueItemID := range pendingSteerQueueItemIDs(binding) {
			item := surface.QueueItems[queueItemID]
			if item != nil && item.Status == state.QueueItemSteering {
				found = true
				return
			}
		}
	})
	return found
}
