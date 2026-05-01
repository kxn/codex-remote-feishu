package orchestrator

import (
	"sort"
	"strings"

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
	return state.EffectiveSurfaceCodexProviderID(s.surfaceDesiredContract(surface))
}

func (s *Service) setSurfaceCodexProviderID(surface *state.SurfaceConsoleRecord, providerID string) {
	if surface == nil {
		return
	}
	surface.CodexProviderID = state.NormalizeDesiredCodexProviderID(providerID)
}
