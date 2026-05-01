package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type surfaceInstanceCompatibility struct {
	Visible    bool
	Compatible bool
}

func (s *Service) surfaceInstanceCompatibility(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) surfaceInstanceCompatibility {
	if inst == nil {
		return surfaceInstanceCompatibility{}
	}
	observed := state.ObservedInstanceBackendContract(inst)
	if observed.Backend == "" {
		return surfaceInstanceCompatibility{}
	}
	if surface == nil {
		return surfaceInstanceCompatibility{Visible: true, Compatible: true}
	}
	desired := s.surfaceDesiredContract(surface)
	if !state.IsHeadlessProductMode(desired.ProductMode) {
		return surfaceInstanceCompatibility{Visible: true, Compatible: true}
	}
	if observed.Backend != agentproto.NormalizeBackend(desired.Backend) {
		return surfaceInstanceCompatibility{}
	}
	result := surfaceInstanceCompatibility{Visible: true, Compatible: true}
	switch observed.Backend {
	case agentproto.BackendCodex:
		result.Compatible = state.NormalizeCodexProviderID(observed.CodexProviderID) == state.EffectiveSurfaceCodexProviderID(desired)
	case agentproto.BackendClaude:
		result.Compatible = state.NormalizeClaudeProfileID(observed.ClaudeProfileID) == state.EffectiveSurfaceClaudeProfileID(desired)
	}
	return result
}

func (s *Service) surfaceInstanceVisibleForSelection(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	return s.surfaceInstanceCompatibility(surface, inst).Visible
}

func (s *Service) surfaceInstanceCompatibleForAttach(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	return s.surfaceInstanceCompatibility(surface, inst).Compatible
}

func (s *Service) mergedThreadViewHasCompatibleVisibleInstance(surface *state.SurfaceConsoleRecord, view *mergedThreadView) bool {
	if view == nil {
		return false
	}
	if view.CompatibleFreeVisibleInst != nil || view.CompatibleAnyVisibleInst != nil {
		return true
	}
	if view.CurrentVisible && s.currentVisibleThreadEligible(surface, view.ThreadID) {
		inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
		return inst != nil && s.surfaceInstanceCompatibleForAttach(surface, inst)
	}
	return false
}
