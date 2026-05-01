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

type HeadlessLaunchBackendContract struct {
	Backend         agentproto.Backend
	CodexProviderID string
	ClaudeProfileID string
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
	contract.Backend = NormalizeSurfaceBackend(contract.ProductMode, contract.Backend)
	contract.CodexProviderID = NormalizeDesiredCodexProviderID(contract.CodexProviderID)
	contract.ClaudeProfileID = NormalizeDesiredClaudeProfileID(contract.ClaudeProfileID)
	return contract
}

func SurfaceDesiredBackendContract(surface *SurfaceConsoleRecord) SurfaceBackendContract {
	if surface == nil {
		return NormalizeSurfaceBackendContract(SurfaceBackendContract{})
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
	if contract.Backend == agentproto.BackendCodex {
		contract.CodexProviderID = NormalizeCodexProviderID(contract.CodexProviderID)
	} else {
		contract.CodexProviderID = ""
	}
	if contract.Backend == agentproto.BackendClaude {
		contract.ClaudeProfileID = NormalizeClaudeProfileID(contract.ClaudeProfileID)
	} else {
		contract.ClaudeProfileID = ""
	}
	return contract
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

func NormalizeHeadlessLaunchBackendContract(contract HeadlessLaunchBackendContract) HeadlessLaunchBackendContract {
	contract.Backend = NormalizeHeadlessBackend(contract.Backend)
	if contract.Backend == agentproto.BackendCodex {
		contract.CodexProviderID = NormalizeCodexProviderID(contract.CodexProviderID)
	} else {
		contract.CodexProviderID = ""
	}
	if contract.Backend == agentproto.BackendClaude {
		contract.ClaudeProfileID = NormalizeClaudeProfileID(contract.ClaudeProfileID)
	} else {
		contract.ClaudeProfileID = ""
	}
	return contract
}

func HeadlessLaunchContractFromSurface(surface *SurfaceConsoleRecord) HeadlessLaunchBackendContract {
	desired := SurfaceDesiredBackendContract(surface)
	return NormalizeHeadlessLaunchBackendContract(HeadlessLaunchBackendContract{
		Backend:         desired.Backend,
		CodexProviderID: EffectiveSurfaceCodexProviderID(desired),
		ClaudeProfileID: EffectiveSurfaceClaudeProfileID(desired),
	})
}

func HeadlessLaunchContractFromPending(pending *HeadlessLaunchRecord) HeadlessLaunchBackendContract {
	if pending == nil {
		return NormalizeHeadlessLaunchBackendContract(HeadlessLaunchBackendContract{})
	}
	return NormalizeHeadlessLaunchBackendContract(HeadlessLaunchBackendContract{
		Backend:         pending.Backend,
		CodexProviderID: pending.CodexProviderID,
		ClaudeProfileID: pending.ClaudeProfileID,
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
