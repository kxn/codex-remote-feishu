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
	effective := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	if !state.IsHeadlessProductMode(effective.Contract.ProductMode) {
		return agentproto.BackendCodex
	}
	if inst != nil {
		return state.NormalizeSurfaceBackend(effective.Contract.ProductMode, state.EffectiveInstanceBackend(inst))
	}
	return effective.Contract.Backend
}

func (s *Service) surfaceDesiredContract(surface *state.SurfaceConsoleRecord) state.SurfaceBackendContract {
	return state.EffectiveSurfaceCapabilitySettings(s.root, surface).Contract
}

func (s *Service) setSurfaceDesiredContract(surface *state.SurfaceConsoleRecord, contract state.SurfaceBackendContract) {
	if surface == nil {
		return
	}
	contract = state.NormalizeSurfaceBackendContract(contract)
	surface.ProductMode = contract.ProductMode
	surface.Backend = contract.Backend
	switch {
	case !state.IsHeadlessProductMode(contract.ProductMode):
		// VS Code does not activate a headless provider/profile binding. Keep any
		// remembered headless selections untouched so mode switches can reuse them.
	case contract.Backend == agentproto.BackendClaude:
		surface.ClaudeProfileID = contract.ClaudeProfileID
	case contract.Backend == agentproto.BackendOpenCode:
		surface.OpenCodeProfileID = contract.OpenCodeProfileID
	default:
		surface.CodexProviderID = contract.CodexProviderID
	}
}

func (s *Service) headlessLaunchContract(surface *state.SurfaceConsoleRecord) state.HeadlessLaunchContract {
	settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface)
	contract := state.NormalizeSurfaceBackendContract(settings.Contract)
	var launch state.HeadlessLaunchContract
	switch contract.Backend {
	case agentproto.BackendClaude:
		launch = state.HeadlessClaudeLaunchContract(state.EffectiveSurfaceClaudeProfileID(contract), settings.PromptOverride.ReasoningEffort)
	case agentproto.BackendOpenCode:
		launch = state.HeadlessOpenCodeLaunchContract(state.EffectiveSurfaceOpenCodeProfileID(contract))
		if surface != nil {
			launch.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(surface.OpenCodeAdmissionRef)
		}
	default:
		launch = state.HeadlessCodexLaunchContract(state.EffectiveSurfaceCodexProviderID(contract))
	}
	if launch.Backend == agentproto.BackendCodex && surface != nil {
		launch.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(surface.CodexAdmissionRef)
		launch.CodexConnectionContract = state.CloneCodexConnectionContract(surface.CodexConnectionContract)
		launch.CodexThreadPolicy = state.CloneCodexThreadPolicy(surface.CodexThreadPolicy)
	}
	if launch.Backend == agentproto.BackendClaude {
		launch.ClaudeReasoningEffort = s.effectiveClaudeReasoningEffort(surface, settings.PromptOverride)
	}
	return state.NormalizeHeadlessLaunchContract(launch)
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
	command.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(contract.CodexAdmissionRef)
	command.CodexConnectionContract = state.CloneCodexConnectionContract(contract.CodexConnectionContract)
	command.CodexThreadPolicy = state.CloneCodexThreadPolicy(contract.CodexThreadPolicy)
	command.ClaudeProfileID = contract.ClaudeProfileID
	command.ClaudeReasoningEffort = contract.ClaudeReasoningEffort
	command.OpenCodeProfileID = contract.OpenCodeProfileID
	command.OpenCodeAdmissionRef = state.NormalizeOpenCodeAdmissionRef(contract.OpenCodeAdmissionRef)
}

func (s *Service) surfaceModeAlias(surface *state.SurfaceConsoleRecord) string {
	mode := s.normalizeSurfaceProductMode(surface)
	return state.SurfaceModeAlias(mode, s.surfaceBackend(surface))
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

func (s *Service) SurfaceOpenCodeProfileID(surfaceID string) string {
	if s == nil || s.root == nil {
		return ""
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return ""
	}
	return s.surfaceOpenCodeProfileID(surface)
}

func (s *Service) surfaceOpenCodeProfileID(surface *state.SurfaceConsoleRecord) string {
	return state.EffectiveSurfaceOpenCodeProfileID(s.surfaceDesiredContract(surface))
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
		switch backend {
		case agentproto.BackendClaude:
			if desired.Backend == agentproto.BackendClaude && state.EffectiveSurfaceClaudeProfileID(desired) != "" {
				return state.ClaudeInstanceBackendContract(state.EffectiveSurfaceClaudeProfileID(desired))
			}
			if observed.Backend == agentproto.BackendClaude {
				return observed
			}
			return state.ClaudeInstanceBackendContract("")
		case agentproto.BackendOpenCode:
			if desired.Backend == agentproto.BackendOpenCode && state.EffectiveSurfaceOpenCodeProfileID(desired) != "" {
				return state.OpenCodeInstanceBackendContract(state.EffectiveSurfaceOpenCodeProfileID(desired))
			}
			if observed.Backend == agentproto.BackendOpenCode {
				return observed
			}
			return state.OpenCodeInstanceBackendContract("")
		default:
			if desired.Backend == agentproto.BackendCodex && state.EffectiveSurfaceCodexProviderID(desired) != "" {
				return state.CodexInstanceBackendContract(state.EffectiveSurfaceCodexProviderID(desired))
			}
			if observed.Backend == agentproto.BackendCodex {
				return observed
			}
			return state.CodexInstanceBackendContract("")
		}
	}
	return observed
}

func (s *Service) workspaceDefaultsStorageKey(workspaceKey string, contract state.InstanceBackendContract) string {
	return state.WorkspaceDefaultsStorageKey(workspaceKey, contract)
}
