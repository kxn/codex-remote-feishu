package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) surfaceBackend(surface *state.SurfaceConsoleRecord) agentproto.Backend {
	if surface == nil {
		return agentproto.BackendCodex
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	return state.EffectiveSurfaceBackend(surface, inst)
}

func (s *Service) surfaceDesiredContract(surface *state.SurfaceConsoleRecord) state.SurfaceBackendContract {
	return state.SurfaceDesiredBackendContract(surface)
}

func (s *Service) setSurfaceDesiredContract(surface *state.SurfaceConsoleRecord, contract state.SurfaceBackendContract) {
	if surface == nil {
		return
	}
	contract = state.NormalizeSurfaceBackendContract(contract)
	surface.ProductMode = contract.ProductMode
	surface.Backend = contract.Backend
	surface.CodexProviderID = contract.CodexProviderID
	surface.ClaudeProfileID = contract.ClaudeProfileID
}

func (s *Service) headlessLaunchContract(surface *state.SurfaceConsoleRecord) state.HeadlessLaunchContract {
	return state.HeadlessLaunchContractFromSurface(surface)
}

func (s *Service) headlessLaunchContractWithOverride(surface *state.SurfaceConsoleRecord, override state.ModelConfigRecord) state.HeadlessLaunchContract {
	contract := s.headlessLaunchContract(surface)
	contract.ClaudeReasoningEffort = override.ReasoningEffort
	return state.NormalizeHeadlessLaunchContract(contract)
}

func (s *Service) applyHeadlessLaunchContract(command *control.DaemonCommand, contract state.HeadlessLaunchContract) {
	if command == nil {
		return
	}
	contract = state.NormalizeHeadlessLaunchContract(contract)
	command.Backend = contract.Backend
	command.CodexProviderID = contract.CodexProviderID
	command.ClaudeProfileID = contract.ClaudeProfileID
	command.ClaudeReasoningEffort = contract.ClaudeReasoningEffort
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
