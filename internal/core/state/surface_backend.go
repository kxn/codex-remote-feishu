package state

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type SurfaceBackendContract struct {
	ProductMode       ProductMode
	Backend           agentproto.Backend
	CodexProfileID    string
	ClaudeProfileID   string
	OpenCodeProfileID string
}

type InstanceBackendContract struct {
	Backend           agentproto.Backend
	CodexProfileID    string
	ClaudeProfileID   string
	OpenCodeProfileID string
}

type HeadlessLaunchContract struct {
	Backend                 agentproto.Backend
	CodexProfileID          string
	CodexAdmissionRef       *CodexAdmissionRef
	CodexConnectionContract *CodexConnectionContract
	CodexThreadPolicy       *CodexThreadPolicy
	ClaudeProfileID         string
	ClaudeReasoningEffort   string
	OpenCodeProfileID       string
	OpenCodeAdmissionRef    *OpenCodeAdmissionRef
}

func VSCodeSurfaceBackendContract() SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode: ProductModeVSCode,
		Backend:     agentproto.BackendCodex,
	}
}

func HeadlessCodexSurfaceBackendContract(profileID string) SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode:    ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: NormalizeDesiredCodexProfileID(profileID),
	}
}

func HeadlessClaudeSurfaceBackendContract(profileID string) SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: NormalizeDesiredClaudeProfileID(profileID),
	}
}

func HeadlessOpenCodeSurfaceBackendContract(profileID string) SurfaceBackendContract {
	return SurfaceBackendContract{
		ProductMode:       ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: NormalizeDesiredOpenCodeProfileID(profileID),
	}
}

func PersistedSurfaceBackendContract(mode ProductMode, backend agentproto.Backend, codexProfileID, claudeProfileID, openCodeProfileID string) SurfaceBackendContract {
	mode = NormalizeProductMode(mode)
	codexProfileID = strings.TrimSpace(codexProfileID)
	claudeProfileID = strings.TrimSpace(claudeProfileID)
	openCodeProfileID = strings.TrimSpace(openCodeProfileID)
	normalizedBackend := agentproto.NormalizeBackend(backend)
	if IsHeadlessProductMode(mode) &&
		claudeProfileID != "" &&
		(strings.TrimSpace(string(backend)) == "" ||
			(normalizedBackend == agentproto.BackendCodex && codexProfileID == "")) {
		return HeadlessClaudeSurfaceBackendContract(claudeProfileID)
	}
	return NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:       mode,
		Backend:           backend,
		CodexProfileID:    codexProfileID,
		ClaudeProfileID:   claudeProfileID,
		OpenCodeProfileID: openCodeProfileID,
	})
}

func CodexInstanceBackendContract(profileID string) InstanceBackendContract {
	return InstanceBackendContract{
		Backend:        agentproto.BackendCodex,
		CodexProfileID: NormalizeCodexProfileID(profileID),
	}
}

func ClaudeInstanceBackendContract(profileID string) InstanceBackendContract {
	return InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: NormalizeClaudeProfileID(profileID),
	}
}

func OpenCodeInstanceBackendContract(profileID string) InstanceBackendContract {
	return InstanceBackendContract{
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: NormalizeOpenCodeProfileID(profileID),
	}
}

func HeadlessCodexLaunchContract(profileID string) HeadlessLaunchContract {
	return HeadlessLaunchContract{
		Backend:        agentproto.BackendCodex,
		CodexProfileID: NormalizeCodexProfileID(profileID),
	}
}

func HeadlessClaudeLaunchContract(profileID, reasoningEffort string) HeadlessLaunchContract {
	return HeadlessLaunchContract{
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       NormalizeClaudeProfileID(profileID),
		ClaudeReasoningEffort: NormalizeClaudeReasoningEffort(reasoningEffort),
	}
}

func HeadlessOpenCodeLaunchContract(profileID string) HeadlessLaunchContract {
	return HeadlessLaunchContract{
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: NormalizeOpenCodeProfileID(profileID),
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
	case agentproto.BackendOpenCode:
		return HeadlessOpenCodeSurfaceBackendContract(contract.OpenCodeProfileID)
	default:
		return HeadlessCodexSurfaceBackendContract(contract.CodexProfileID)
	}
}

func SurfaceDesiredBackendContract(surface *SurfaceConsoleRecord) SurfaceBackendContract {
	if surface == nil {
		return HeadlessCodexSurfaceBackendContract("")
	}
	return NormalizeSurfaceBackendContract(SurfaceBackendContract{
		ProductMode:       surface.ProductMode,
		Backend:           surface.Backend,
		CodexProfileID:    surface.CodexProfileID,
		ClaudeProfileID:   surface.ClaudeProfileID,
		OpenCodeProfileID: surface.OpenCodeProfileID,
	})
}

func NormalizeObservedInstanceBackendContract(contract InstanceBackendContract) InstanceBackendContract {
	contract.Backend = NormalizeHeadlessBackend(contract.Backend)
	switch contract.Backend {
	case agentproto.BackendClaude:
		return ClaudeInstanceBackendContract(contract.ClaudeProfileID)
	case agentproto.BackendOpenCode:
		return OpenCodeInstanceBackendContract(contract.OpenCodeProfileID)
	default:
		return CodexInstanceBackendContract(contract.CodexProfileID)
	}
}

func ObservedInstanceBackendContract(inst *InstanceRecord) InstanceBackendContract {
	if inst == nil {
		return NormalizeObservedInstanceBackendContract(InstanceBackendContract{})
	}
	return NormalizeObservedInstanceBackendContract(InstanceBackendContract{
		Backend:           inst.Backend,
		CodexProfileID:    inst.CodexProfileID,
		ClaudeProfileID:   inst.ClaudeProfileID,
		OpenCodeProfileID: inst.OpenCodeProfileID,
	})
}

func NormalizeHeadlessLaunchContract(contract HeadlessLaunchContract) HeadlessLaunchContract {
	contract.Backend = NormalizeHeadlessBackend(contract.Backend)
	switch contract.Backend {
	case agentproto.BackendClaude:
		return HeadlessClaudeLaunchContract(contract.ClaudeProfileID, contract.ClaudeReasoningEffort)
	case agentproto.BackendOpenCode:
		normalized := HeadlessOpenCodeLaunchContract(contract.OpenCodeProfileID)
		normalized.OpenCodeAdmissionRef = NormalizeOpenCodeAdmissionRef(contract.OpenCodeAdmissionRef)
		return normalized
	default:
		normalized := HeadlessCodexLaunchContract(contract.CodexProfileID)
		normalized.CodexAdmissionRef = NormalizeCodexAdmissionRef(contract.CodexAdmissionRef)
		normalized.CodexConnectionContract = CloneCodexConnectionContract(contract.CodexConnectionContract)
		normalized.CodexThreadPolicy = CloneCodexThreadPolicy(contract.CodexThreadPolicy)
		return normalized
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
	if desired.Backend == agentproto.BackendOpenCode {
		launch := HeadlessOpenCodeLaunchContract(EffectiveSurfaceOpenCodeProfileID(desired))
		if surface != nil {
			launch.OpenCodeAdmissionRef = NormalizeOpenCodeAdmissionRef(surface.OpenCodeAdmissionRef)
		}
		return NormalizeHeadlessLaunchContract(launch)
	}
	return HeadlessCodexLaunchContract(EffectiveSurfaceCodexProfileID(desired))
}

func HeadlessLaunchContractFromPending(pending *HeadlessLaunchRecord) HeadlessLaunchContract {
	if pending == nil {
		return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{})
	}
	return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{
		Backend:                 pending.Backend,
		CodexProfileID:          pending.CodexProfileID,
		CodexAdmissionRef:       pending.CodexAdmissionRef,
		CodexConnectionContract: pending.CodexConnectionContract,
		CodexThreadPolicy:       pending.CodexThreadPolicy,
		ClaudeProfileID:         pending.ClaudeProfileID,
		ClaudeReasoningEffort:   pending.ClaudeReasoningEffort,
		OpenCodeProfileID:       pending.OpenCodeProfileID,
		OpenCodeAdmissionRef:    pending.OpenCodeAdmissionRef,
	})
}

func HeadlessLaunchContractFromInstance(inst *InstanceRecord) HeadlessLaunchContract {
	if inst == nil {
		return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{})
	}
	return NormalizeHeadlessLaunchContract(HeadlessLaunchContract{
		Backend:                 inst.Backend,
		CodexProfileID:          inst.CodexProfileID,
		CodexAdmissionRef:       inst.CodexAdmissionRef,
		CodexConnectionContract: inst.CodexConnectionContract,
		CodexThreadPolicy:       inst.CodexThreadPolicy,
		ClaudeProfileID:         inst.ClaudeProfileID,
		ClaudeReasoningEffort:   inst.ClaudeReasoningEffort,
		OpenCodeProfileID:       inst.OpenCodeProfileID,
		OpenCodeAdmissionRef:    inst.OpenCodeAdmissionRef,
	})
}

func EffectiveSurfaceCodexProfileID(contract SurfaceBackendContract) string {
	contract = NormalizeSurfaceBackendContract(contract)
	if !IsHeadlessProductMode(contract.ProductMode) || contract.Backend != agentproto.BackendCodex {
		return ""
	}
	if strings.TrimSpace(contract.CodexProfileID) == "" {
		return DefaultCodexProfileID
	}
	return NormalizeCodexProfileID(contract.CodexProfileID)
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

func EffectiveSurfaceOpenCodeProfileID(contract SurfaceBackendContract) string {
	contract = NormalizeSurfaceBackendContract(contract)
	if !IsHeadlessProductMode(contract.ProductMode) || contract.Backend != agentproto.BackendOpenCode {
		return ""
	}
	if strings.TrimSpace(contract.OpenCodeProfileID) == "" {
		return DefaultOpenCodeProfileID
	}
	return NormalizeOpenCodeProfileID(contract.OpenCodeProfileID)
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

func NormalizeDesiredCodexProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ""
	}
	return NormalizeCodexProfileID(profileID)
}

func NormalizeDesiredClaudeProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ""
	}
	return NormalizeClaudeProfileID(profileID)
}

func NormalizeOpenCodeProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return DefaultOpenCodeProfileID
	}
	return profileID
}

func NormalizeDesiredOpenCodeProfileID(profileID string) string {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return ""
	}
	return NormalizeOpenCodeProfileID(profileID)
}

// SurfaceModeAlias projects the stored runtime shape + backend pair back to the
// current user-visible mode names.
func SurfaceModeAlias(mode ProductMode, backend agentproto.Backend) string {
	if !IsHeadlessProductMode(mode) {
		return string(ProductModeVSCode)
	}
	switch NormalizeHeadlessBackend(backend) {
	case agentproto.BackendClaude:
		return "claude"
	case agentproto.BackendOpenCode:
		return "opencode"
	default:
		return "codex"
	}
}
