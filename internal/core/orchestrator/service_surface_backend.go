package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) surfaceBackend(surface *state.SurfaceConsoleRecord) agentproto.Backend {
	if surface == nil {
		return agentproto.BackendCodex
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	backend := state.EffectiveSurfaceBackend(surface, inst)
	surface.Backend = backend
	return backend
}

func (s *Service) surfaceModeAlias(surface *state.SurfaceConsoleRecord) string {
	mode := s.normalizeSurfaceProductMode(surface)
	return state.SurfaceModeAlias(mode, s.surfaceBackend(surface))
}

func (s *Service) SurfaceBackend(surfaceID string) agentproto.Backend {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	return s.surfaceBackend(surface)
}

func (s *Service) surfaceWorkspaceDefaultsBackend(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) agentproto.Backend {
	if surface != nil {
		return state.EffectiveSurfaceBackend(surface, inst)
	}
	if inst != nil {
		return state.EffectiveInstanceBackend(inst)
	}
	return agentproto.BackendCodex
}

func (s *Service) workspaceDefaultsStorageKey(workspaceKey string, backend agentproto.Backend) string {
	return state.WorkspaceDefaultsStorageKey(workspaceKey, backend)
}
