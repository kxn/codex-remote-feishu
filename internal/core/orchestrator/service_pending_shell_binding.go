package orchestrator

import (
	"sort"
	"strings"
)

func (s *Service) bindPendingShell(key string, binding *pendingShellBinding) {
	if binding != nil && binding.Sequence == 0 {
		s.nextShellBindingSequence++
		binding.Sequence = s.nextShellBindingSequence
	}
	s.turns.bindPendingShell(key, binding)
}

func (s *Service) bindPendingShellCommand(surfaceID, commandID string) bool {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return false
	}
	type candidate struct {
		key     string
		binding *pendingShellBinding
	}
	candidates := []candidate{}
	s.turns.forEachPendingShell(func(key string, binding *pendingShellBinding) {
		if binding.SurfaceSessionID != surfaceID || binding.CommandID != "" {
			return
		}
		candidates = append(candidates, candidate{key: key, binding: binding})
	})
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].binding.Sequence != candidates[j].binding.Sequence {
			return candidates[i].binding.Sequence < candidates[j].binding.Sequence
		}
		return candidates[i].key < candidates[j].key
	})
	if len(candidates) == 0 {
		return false
	}
	candidates[0].binding.CommandID = commandID
	return true
}

func (s *Service) pendingShellForCommand(instanceID, commandID string) (string, *pendingShellBinding) {
	commandID = strings.TrimSpace(commandID)
	instanceID = strings.TrimSpace(instanceID)
	if commandID == "" {
		return "", nil
	}
	var key string
	var matched *pendingShellBinding
	s.turns.forEachPendingShell(func(candidateKey string, binding *pendingShellBinding) {
		if matched != nil || binding.CommandID != commandID {
			return
		}
		if instanceID != "" && binding.InstanceID != instanceID {
			return
		}
		key, matched = candidateKey, binding
	})
	return key, matched
}

func (s *Service) clearPendingShell(key string) { s.turns.clearPendingShell(key) }

func (s *Service) pendingShellBinding(key string) *pendingShellBinding {
	return s.turns.pendingShellBinding(key)
}

func (s *Service) pendingShellKeysForInstance(instanceID string) []string {
	instanceID = strings.TrimSpace(instanceID)
	var keys []string
	s.turns.forEachPendingShell(func(key string, binding *pendingShellBinding) {
		if binding.InstanceID == instanceID {
			keys = append(keys, key)
		}
	})
	sort.Strings(keys)
	return keys
}
