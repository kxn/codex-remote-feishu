package orchestrator

import (
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type PrivateCodexContract struct {
	Admission  *state.CodexAdmissionRef
	Connection *state.CodexConnectionContract
	Thread     *state.CodexThreadPolicy
}

// ApplyPrivateCodexContracts changes only one owner's projection, then invokes
// the existing busy-aware contract reconciliation for that surface.
func (s *Service) ApplyPrivateCodexContracts(surfaceID, version string, profiles map[string]PrivateCodexContract) ([]eventcontract.Event, bool, error) {
	surface := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || surface == nil || version == "" {
		return nil, false, fmt.Errorf("invalid private codex projection")
	}
	runtime := s.ensureSurfaceUIRuntime(surface)
	contract := state.SurfaceDesiredBackendContract(surface)
	projectionKey := version + ":" + string(contract.Backend) + ":" + string(contract.ProductMode) + ":" + state.EffectiveSurfaceCodexProfileID(contract)
	if runtime.PrivateCodexContractVersion == projectionKey {
		return nil, false, nil
	}
	applicable := state.IsHeadlessProductMode(contract.ProductMode) && contract.Backend == agentproto.BackendCodex
	if applicable {
		profile := state.EffectiveSurfaceCodexProfileID(contract)
		projected := profiles[profile]
		surface.CodexAdmissionRef = state.NormalizeCodexAdmissionRef(projected.Admission)
		surface.CodexConnectionContract = state.CloneCodexConnectionContract(projected.Connection)
		surface.CodexThreadPolicy = state.CloneCodexThreadPolicy(projected.Thread)
	}
	runtime.PrivateCodexContractVersion = projectionKey
	if !applicable {
		return nil, true, nil
	}
	if _, held := s.privateRecoveryHolds[surfaceID]; held {
		surface.ContractRefreshPending = true
		return nil, true, nil
	}
	return s.reconcileHeadlessSurfaceContract(surface), true, nil
}

// PrivateCodexContractsCurrent checks the profile/backend-specific projection key.
func (s *Service) PrivateCodexContractsCurrent(surfaceID, version string) bool {
	surface := s.root.Surfaces[surfaceID]
	if !s.config.PrivateApplication || surface == nil {
		return false
	}
	runtime := s.surfaceUIRuntimeByID(surfaceID)
	if runtime == nil {
		return false
	}
	contract := state.SurfaceDesiredBackendContract(surface)
	key := version + ":" + string(contract.Backend) + ":" + string(contract.ProductMode) + ":" + state.EffectiveSurfaceCodexProfileID(contract)
	return runtime.PrivateCodexContractVersion == key
}
