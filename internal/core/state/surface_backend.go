package state

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type SurfaceBackendContract struct {
	ProductMode     ProductMode
	Backend         agentproto.Backend
	CodexProviderID string
	ClaudeProfileID string
}

type InstanceBackendContract struct {
	Backend         agentproto.Backend
	CodexProviderID string
	ClaudeProfileID string
}

type HeadlessLaunchContract struct {
	Backend               agentproto.Backend
	CodexProviderID       string
	ClaudeProfileID       string
	ClaudeReasoningEffort string
}

func VSCodeSurfaceBackendContract() SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode: ProductModeVSCode,
		Backend:     agentproto.BackendCodex,
	}
}

func HeadlessCodexSurfaceBackendContract(providerID string) SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendCodex,
		CodexProviderID: NormalizeDesiredCodexProviderID(providerID),
	}
}

func HeadlessClaudeSurfaceBackendContract(profileID string) SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: NormalizeDesiredClaudeProfileID(profileID),
	}
}

func PersistedSurfaceBackendContract(mode ProductMode, backend agentproto.Backend, codexProviderID, claudeProfileID string) SurfaceBackendContract {
	mode = NormalizeProductMode(mode)
	codexProviderID = strings.TrimSpace(codexProviderID)
	claudeProfileID = strings.TrimSpace(claudeProfileID)
	normalizedBackend := agentproto.NormalizeBackend(backend)
	if IsHeadlessProductMode(mode) &&
		claudeProfileID != "" &&
		(strings.TrimSpace(string(backend)) == "" ||
			(normalizedBackend == agentproto.BackendCodex && codexProviderID == "")) {
		return HeadlessClaudeSurfaceBackendContract(claudeProfileID)
	}
	return NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:     mode,
		Backend:         backend,
		CodexProviderID: codexProviderID,
		ClaudeProfileID: claudeProfileID,
	})
}

func CodexInstanceBackendContract(providerID string) InstanceBackendContract {
	return InstanceBackendContract{
		Backend:         agentproto.BackendCodex,
		CodexProviderID: NormalizeCodexProviderID(providerID),
	}
}

func ClaudeInstanceBackendContract(profileID string) InstanceBackendContract {
	return InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: NormalizeClaudeProfileID(profileID),
	}
}

func HeadlessCodexLaunchContract(providerID string) HeadlessLaunchContract {
	return HeadlessLaunchContract{
		Backend:         agentproto.BackendCodex,
		CodexProviderID: NormalizeCodexProviderID(providerID),
	}
}

func HeadlessClaudeLaunchContract(profileID, reasoningEffort string) HeadlessLaunchContract {
	return HeadlessLaunchContract{
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       NormalizeClaudeProfileID(profileID),
		ClaudeReasoningEffort: NormalizeClaudeReasoningEffort(reasoningEffort),
	}
}

func NormalizeHeadlessBackend(backend agentproto.Backend) agentproto.Backend {
	return agentproto.NormalizeBackend(backend)
}

func NormalizeSurfaceBackend(mode ProductMode, backend agentproto.Backend) agentproto.Backend {
	if !IsHeadlessProductMode(mode) {
		return agentproto.BackendCodex
	}
	return NormalizeHeadlessBackend(backend)
}

func NormalizeSurfaceBackendContract(contract SurfaceBackendContract) SurfaceBackendContract {
	contract.ProductMode = NormalizeProductMode(contract.ProductMode)
	if !IsHeadlessProductMode(contract.ProductMode) {
		return VSCodeSurfaceBackendContract()
	}
	switch NormalizeHeadlessBackend(contract.Backend) {
	case agentproto.BackendClaude:
		return HeadlessClaudeSurfaceBackendContract(contract.ClaudeProfileID)
	default:
		return HeadlessCodexSurfaceBackendContract(contract.CodexProviderID)
	}
}

func SurfaceDesiredBackendContract(surface *SurfaceConsoleRecord) SurfaceBackendContract {
	if surface == nil {
		return HeadlessCodexSurfaceBackendContract("")
	}
	return NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:     surface.ProductMode,
		Backend:         surface.Backend,
		CodexProviderID: surface.CodexProviderID,
		ClaudeProfileID: surface.ClaudeProfileID,
	})
}

func NormalizeObservedInstanceBackendContract(contract InstanceBackendContract) InstanceBackendContract {
	contract.Backend = NormalizeHeadlessBackend(contract.Backend)
	switch contract.Backend {
	case agentproto.BackendClaude:
		return ClaudeInstanceBackendContract(contract.ClaudeProfileID)
	default:
		return CodexInstanceBackendContract(contract.CodexProviderID)
	}
}

func ObservedInstanceBackendContract(inst *InstanceRecord) InstanceBackendContract {
	if inst == nil {
		return NormalizeObservedInstanceBackendContract(InstanceBackendContract{})
	}
	return NormalizeObservedInstanceBackendContract(InstanceBackendContract{
		Backend:         inst.Backend,
		CodexProviderID: inst.CodexProviderID,
		ClaudeProfileID: inst.ClaudeProfileID,
	})
}

func NormalizeHeadlessLaunchContract(contract HeadlessLaunchContract) HeadlessLaunchContract {
	contract.Backend = NormalizeHeadlessBackend(contract.Backend)
	switch contract.Backend {
	case agentproto.BackendClaude:
		return HeadlessClaudeLaunchContract(contract.ClaudeProfileID, contract.ClaudeReasoningEffort)
	default:
		return HeadlessCodexLaunchContract(contract.CodexProviderID)
	}
}

func HeadlessLaunchContractFromSurface(surface *SurfaceConsoleRecord) HeadlessLaunchContract {
	desired := SurfaceDesiredBackendContract(surface)
	reasoning := ""
	if surface != nil {
		reasoning = surface.PromptOverride.ReasoningEffort
	}
	if desired.Backend == agentproto.BackendClaude {
		return HeadlessClaudeLaunchContract(EffectiveSurfaceClaudeProfileID(desired), reasoning)
	}
	return HeadlessCodexLaunchContract(EffectiveSurfaceCodexProviderID(desired))
}

func HeadlessLaunchContractFromPending(pending *HeadlessLaunchRecord) HeadlessLaunchContract {
	if pending == nil {
		return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{})
	}
	return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{
		Backend:               pending.Backend,
		CodexProviderID:       pending.CodexProviderID,
		ClaudeProfileID:       pending.ClaudeProfileID,
		ClaudeReasoningEffort: pending.ClaudeReasoningEffort,
	})
}

func HeadlessLaunchContractFromInstance(inst *InstanceRecord) HeadlessLaunchContract {
	if inst == nil {
		return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{})
	}
	return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{
		Backend:               inst.Backend,
		CodexProviderID:       inst.CodexProviderID,
		ClaudeProfileID:       inst.ClaudeProfileID,
		ClaudeReasoningEffort: inst.ClaudeReasoningEffort,
	})
}

func DesiredSurfaceBackend(surface *SurfaceConsoleRecord) agentproto.Backend {
	return SurfaceDesiredBackendContract(surface).Backend
}

func EffectiveSurfaceCodexProviderID(contract SurfaceBackendContract) string {
	contract = NormalizeSurfaceBackendContract(contract)
	if !IsHeadlessProductMode(contract.ProductMode) || contract.Backend != agentproto.BackendCodex {
		return ""
	}
	if strings.TrimSpace(contract.CodexProviderID) == "" {
		return DefaultCodexProviderID
	}
	return NormalizeCodexProviderID(contract.CodexProviderID)
}

func EffectiveSurfaceClaudeProfileID(contract SurfaceBackendContract) string {
	contract = NormalizeSurfaceBackendContract(contract)
	if !IsHeadlessProductMode(contract.ProductMode) || contract.Backend != agentproto.BackendClaude {
		return ""
	}
	if strings.TrimSpace(contract.ClaudeProfileID) == "" {
		return DefaultClaudeProfileID
	}
	return NormalizeClaudeProfileID(contract.ClaudeProfileID)
}

func EffectiveSurfaceBackend(surface *SurfaceConsoleRecord, inst *InstanceRecord) agentproto.Backend {
	desired := SurfaceDesiredBackendContract(surface)
	if !IsHeadlessProductMode(desired.ProductMode) {
		return agentproto.BackendCodex
	}
	if inst != nil {
		return NormalizeSurfaceBackend(desired.ProductMode, EffectiveInstanceBackend(inst))
	}
	return desired.Backend
}

func NormalizeDesiredCodexProviderID(providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return ""
	}
	return NormalizeCodexProviderID(providerID)
}

func NormalizeDesiredClaudeProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ""
	}
	return NormalizeClaudeProfileID(profileID)
}

// SurfaceModeAlias projects the stored runtime shape + backend pair back to the
// current user-visible mode names.
func SurfaceModeAlias(mode ProductMode, backend agentproto.Backend) string {
	if !IsHeadlessProductMode(mode) {
		return "vscode"
	}
	switch NormalizeHeadlessBackend(backend) {
	case agentproto.BackendClaude:
		return "claude"
	default:
		return "codex"
	}
}
