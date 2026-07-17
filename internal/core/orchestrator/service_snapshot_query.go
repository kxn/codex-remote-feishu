package orchestrator

import (
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) Instance(instanceID string) *state.InstanceRecord {
	return s.root.Instances[instanceID]
}

func (s *Service) Surface(surfaceID string) *state.SurfaceConsoleRecord {
	return s.root.Surfaces[surfaceID]
}

func (s *Service) Instances() []*state.InstanceRecord {
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, instance := range s.root.Instances {
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].WorkspaceKey == instances[j].WorkspaceKey {
			return instances[i].InstanceID < instances[j].InstanceID
		}
		return instances[i].WorkspaceKey < instances[j].WorkspaceKey
	})
	return instances
}

func (s *Service) Surfaces() []*state.SurfaceConsoleRecord {
	surfaces := make([]*state.SurfaceConsoleRecord, 0, len(s.root.Surfaces))
	for _, surface := range s.root.Surfaces {
		surfaces = append(surfaces, surface)
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].SurfaceSessionID < surfaces[j].SurfaceSessionID
	})
	return surfaces
}

func (s *Service) SurfaceUIRuntime(surfaceID string) SurfaceUIRuntimeSummary {
	return s.SurfaceUIRuntimeSummary(surfaceID)
}

type RemoteTurnStatus struct {
	InstanceID       string `json:"instanceId"`
	SurfaceSessionID string `json:"surfaceSessionId"`
	QueueItemID      string `json:"queueItemId"`
	SourceMessageID  string `json:"sourceMessageId,omitempty"`
	CommandID        string `json:"commandId,omitempty"`
	ThreadID         string `json:"threadId,omitempty"`
	TurnID           string `json:"turnId,omitempty"`
	Status           string `json:"status"`
}

func (s *Service) PendingRemoteTurns() []RemoteTurnStatus {
	values := []RemoteTurnStatus{}
	s.turns.forEachPendingRemote(func(binding *remoteTurnBinding) {
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	})
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].QueueItemID < values[j].QueueItemID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) ActiveRemoteTurns() []RemoteTurnStatus {
	values := []RemoteTurnStatus{}
	s.turns.forEachActiveRemote(func(binding *remoteTurnBinding) {
		values = append(values, RemoteTurnStatus{
			InstanceID:       binding.InstanceID,
			SurfaceSessionID: binding.SurfaceSessionID,
			QueueItemID:      binding.QueueItemID,
			SourceMessageID:  binding.SourceMessageID,
			CommandID:        binding.CommandID,
			ThreadID:         binding.ThreadID,
			TurnID:           binding.TurnID,
			Status:           binding.Status,
		})
	})
	sort.Slice(values, func(i, j int) bool {
		if values[i].InstanceID == values[j].InstanceID {
			return values[i].TurnID < values[j].TurnID
		}
		return values[i].InstanceID < values[j].InstanceID
	})
	return values
}

func (s *Service) SurfaceHasPendingSteer(surfaceID string) bool {
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return false
	}
	return s.surfaceHasPendingSteer(surface)
}

func (s *Service) InstanceHasPendingCompact(instanceID string) bool {
	return s.progress.instanceHasCompact(instanceID)
}

func (s *Service) InstanceHasPendingSteer(instanceID string) bool {
	for _, binding := range s.turns.pendingSteers {
		if binding == nil || binding.InstanceID != instanceID {
			continue
		}
		surface := s.root.Surfaces[binding.SurfaceSessionID]
		if surface == nil {
			continue
		}
		if s.surfaceHasPendingSteer(surface) {
			return true
		}
	}
	return false
}
