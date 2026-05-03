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
	contract := state.HeadlessLaunchContractFromSurface(surface)
	if contract.Backend == agentproto.BackendClaude {
		contract.ClaudeReasoningEffort = s.effectiveClaudeReasoningEffort(surface, surfacePromptOverride(surface))
	}
	return state.NormalizeHeadlessLaunchContract(contract)
}

func (s *Service) headlessLaunchContractWithOverride(surface *state.SurfaceConsoleRecord, override state.ModelConfigRecord) state.HeadlessLaunchContract {
	contract := s.headlessLaunchContract(surface)
	if contract.Backend == agentproto.BackendClaude {
		contract.ClaudeReasoningEffort = s.effectiveClaudeReasoningEffort(surface, override)
	}
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

func surfacePromptOverride(surface *state.SurfaceConsoleRecord) state.ModelConfigRecord {
	if surface == nil {
		return state.ModelConfigRecord{}
	}
	return surface.PromptOverride
}

func (s *Service) effectiveClaudeReasoningEffort(surface *state.SurfaceConsoleRecord, override state.ModelConfigRecord) string {
	if effort := state.NormalizeClaudeReasoningEffort(override.ReasoningEffort); effort != "" {
		return effort
	}
	if surface == nil {
		return ""
	}
	return s.claudeProfileReasoningEffort(s.surfaceClaudeProfileID(surface))
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

func (s *Service) surfaceWorkspaceDefaultsContract(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) state.InstanceBackendContract {
	backend := s.surfaceWorkspaceDefaultsBackend(surface, inst)
	observed := state.ObservedInstanceBackendContract(inst)
	if surface != nil {
		desired := s.surfaceDesiredContract(surface)
		contract := state.InstanceBackendContract{Backend: backend}
		switch backend {
		case agentproto.BackendClaude:
			if desired.Backend == agentproto.BackendClaude {
				contract.ClaudeProfileID = state.EffectiveSurfaceClaudeProfileID(desired)
			}
			if contract.ClaudeProfileID == "" && observed.Backend == agentproto.BackendClaude {
				contract.ClaudeProfileID = observed.ClaudeProfileID
			}
		default:
			if desired.Backend == agentproto.BackendCodex {
				contract.CodexProviderID = state.EffectiveSurfaceCodexProviderID(desired)
			}
			if contract.CodexProviderID == "" && observed.Backend == agentproto.BackendCodex {
				contract.CodexProviderID = observed.CodexProviderID
			}
		}
		return state.NormalizeObservedInstanceBackendContract(contract)
	}
	return observed
}

func (s *Service) workspaceDefaultsStorageKey(workspaceKey string, contract state.InstanceBackendContract) string {
	return state.WorkspaceDefaultsStorageKey(workspaceKey, contract)
}

func (s *Service) legacyWorkspaceDefaultsStorageKey(workspaceKey string, backend agentproto.Backend) string {
	return state.LegacyWorkspaceDefaultsStorageKey(workspaceKey, backend)
}
