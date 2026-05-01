package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) instanceMatchesSurfaceCodexProvider(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	if surface == nil || inst == nil {
		return true
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal || s.surfaceBackend(surface) != agentproto.BackendCodex {
		return true
	}
	if state.EffectiveInstanceBackend(inst) != agentproto.BackendCodex {
		return true
	}
	return state.NormalizeCodexProviderID(inst.CodexProviderID) == s.surfaceCodexProviderID(surface)
}

func (s *Service) SurfaceCodexProviderID(surfaceID string) string {
	if s.root == nil {
		return ""
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return ""
	}
	return s.surfaceCodexProviderID(surface)
}

func (s *Service) surfaceCodexProviderID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return state.DefaultCodexProviderID
	}
	providerID := strings.TrimSpace(surface.CodexProviderID)
	mode := state.NormalizeProductMode(surface.ProductMode)
	backend := state.NormalizeSurfaceBackend(mode, surface.Backend)
	if providerID == "" && state.IsHeadlessProductMode(mode) && backend == agentproto.BackendCodex {
		providerID = state.DefaultCodexProviderID
	}
	if providerID == "" {
		surface.CodexProviderID = ""
		return ""
	}
	surface.CodexProviderID = state.NormalizeCodexProviderID(providerID)
	return surface.CodexProviderID
}

func (s *Service) setSurfaceCodexProviderID(surface *state.SurfaceConsoleRecord, providerID string) {
	if surface == nil {
		return
	}
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		surface.CodexProviderID = ""
		_ = s.surfaceCodexProviderID(surface)
		return
	}
	surface.CodexProviderID = state.NormalizeCodexProviderID(providerID)
}

func (s *Service) applyCurrentCodexProviderToHeadlessCommand(surface *state.SurfaceConsoleRecord, command *control.DaemonCommand) {
	if surface == nil || command == nil {
		return
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal || s.surfaceBackend(surface) != agentproto.BackendCodex {
		command.CodexProviderID = ""
		return
	}
	command.CodexProviderID = s.surfaceCodexProviderID(surface)
}
