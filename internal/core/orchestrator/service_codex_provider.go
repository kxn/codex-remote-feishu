package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) MaterializeCodexProviders(records []state.CodexProviderRecord) {
	if s.root == nil {
		return
	}
	s.root.CodexProviders = map[string]state.CodexProviderRecord{}
	defaultRecord := state.NormalizeCodexProviderRecord(state.CodexProviderRecord{
		ID:      state.DefaultCodexProviderID,
		Name:    state.DefaultCodexProviderName,
		BuiltIn: true,
	})
	s.root.CodexProviders[defaultRecord.ID] = defaultRecord
	for _, record := range records {
		current := state.NormalizeCodexProviderRecord(record)
		if current.ID == "" {
			continue
		}
		s.root.CodexProviders[current.ID] = current
	}
}

func (s *Service) CodexProviders() []state.CodexProviderRecord {
	if s.root == nil || len(s.root.CodexProviders) == 0 {
		return []state.CodexProviderRecord{state.NormalizeCodexProviderRecord(state.CodexProviderRecord{
			ID:      state.DefaultCodexProviderID,
			Name:    state.DefaultCodexProviderName,
			BuiltIn: true,
		})}
	}
	providers := make([]state.CodexProviderRecord, 0, len(s.root.CodexProviders))
	for _, record := range s.root.CodexProviders {
		providers = append(providers, state.NormalizeCodexProviderRecord(record))
	}
	sort.SliceStable(providers, func(i, j int) bool {
		left := providers[i]
		right := providers[j]
		if left.BuiltIn != right.BuiltIn {
			return left.BuiltIn
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return providers
}

func (s *Service) codexProviderRecord(providerID string) state.CodexProviderRecord {
	providerID = state.NormalizeCodexProviderID(providerID)
	if s.root != nil && s.root.CodexProviders != nil {
		if record, ok := s.root.CodexProviders[providerID]; ok {
			return state.NormalizeCodexProviderRecord(record)
		}
	}
	return state.NormalizeCodexProviderRecord(state.CodexProviderRecord{
		ID:      providerID,
		Name:    providerID,
		BuiltIn: providerID == state.DefaultCodexProviderID,
	})
}

func (s *Service) codexProviderDisplayName(providerID string) string {
	return s.codexProviderRecord(providerID).Name
}

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
