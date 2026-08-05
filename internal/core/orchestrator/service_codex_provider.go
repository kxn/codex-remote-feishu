package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/codexcatalog"
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

func (s *Service) MaterializeCodexProfiles(records []state.CodexProfileSummary) {
	if s.root == nil {
		return
	}
	s.root.CodexProfiles = map[string]state.CodexProfileSummary{}
	for _, record := range records {
		current := normalizeCodexProfileSummary(record)
		if current.ID == "" {
			continue
		}
		s.root.CodexProfiles[current.ID] = current
	}
	if _, ok := s.root.CodexProfiles[state.NativeCodexProfileID]; !ok {
		s.root.CodexProfiles[state.NativeCodexProfileID] = normalizeCodexProfileSummary(state.CodexProfileSummary{
			ID:              state.NativeCodexProfileID,
			Kind:            state.CodexProfileKindNative,
			Name:            "本机默认",
			Available:       true,
			ContextEditable: true,
		})
	}
}

func (s *Service) CodexProfiles() []state.CodexProfileSummary {
	if s.root == nil || len(s.root.CodexProfiles) == 0 {
		return []state.CodexProfileSummary{normalizeCodexProfileSummary(state.CodexProfileSummary{
			ID:              state.NativeCodexProfileID,
			Kind:            state.CodexProfileKindNative,
			Name:            "本机默认",
			Available:       true,
			ContextEditable: true,
		})}
	}
	profiles := make([]state.CodexProfileSummary, 0, len(s.root.CodexProfiles))
	for _, record := range s.root.CodexProfiles {
		profiles = append(profiles, normalizeCodexProfileSummary(record))
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		left := profiles[i]
		right := profiles[j]
		leftRank := codexProfileKindRank(left.Kind)
		rightRank := codexProfileKindRank(right.Kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return profiles
}

func (s *Service) CodexProfileContextEvidence(profileID string, preferenceRevision uint64) (int64, int64, string, bool) {
	profileID = strings.TrimSpace(profileID)
	if s == nil || s.root == nil || profileID == "" {
		return 0, 0, "", false
	}
	var selected *state.ThreadRecord
	for _, inst := range s.root.Instances {
		if inst == nil {
			continue
		}
		ref := state.NormalizeCodexAdmissionRef(inst.CodexAdmissionRef)
		if ref == nil || ref.ProfileRef.ID != profileID {
			continue
		}
		if preferenceRevision != 0 && ref.ContextPreferenceRef.Revision != preferenceRevision {
			continue
		}
		for _, thread := range inst.Threads {
			if thread == nil || thread.CodexEffectiveThread == nil {
				continue
			}
			effective := thread.CodexEffectiveThread
			if effective.RequestedContextWindow == 0 && effective.EffectiveContextWindow == 0 && strings.TrimSpace(effective.ContextStatus) == "" {
				continue
			}
			if selected == nil || thread.LastUsedAt.After(selected.LastUsedAt) {
				selected = thread
			}
		}
	}
	if selected == nil {
		return 0, 0, "", false
	}
	effective := selected.CodexEffectiveThread
	return effective.RequestedContextWindow, effective.EffectiveContextWindow, strings.TrimSpace(effective.ContextStatus), true
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

func normalizeCodexProfileSummary(value state.CodexProfileSummary) state.CodexProfileSummary {
	value.ID = strings.TrimSpace(value.ID)
	value.Name = strings.TrimSpace(value.Name)
	value.Model = strings.TrimSpace(value.Model)
	value.ReasoningEffort = normalizeModelReasoningEffort(value.ReasoningEffort)
	if value.Kind == "" {
		value.Kind = state.CodexProfileKindAPI
	}
	switch value.ID {
	case state.NativeCodexProfileID:
		value.Kind = state.CodexProfileKindNative
		value.Name = "本机默认"
		value.Available = true
	case state.OAuthCodexProfileID:
		value.Kind = state.CodexProfileKindOAuth
		if value.Name == "" {
			value.Name = "ChatGPT 登录"
		}
	default:
		if value.Name == "" {
			value.Name = value.ID
		}
	}
	return value
}

func codexProfileKindRank(kind state.CodexProfileKind) int {
	switch kind {
	case state.CodexProfileKindNative:
		return 0
	case state.CodexProfileKindOAuth:
		return 1
	default:
		return 2
	}
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

func (s *Service) surfaceCodexProfileID(surface *state.SurfaceConsoleRecord) string {
	if s != nil && s.root != nil {
		if record, status := state.LookupSurfaceBotCapabilitySettings(s.root, surface); status == state.BotCapabilitySettingsLookupValid {
			if profileID := strings.TrimSpace(record.CodexProfileID); profileID != "" {
				return profileID
			}
		}
	}
	return state.CodexProfileIDFromLegacyProviderID(s.surfaceCodexProviderID(surface))
}

func (s *Service) codexProfileSummaryByID(profileID string) (state.CodexProfileSummary, bool) {
	profileID = strings.TrimSpace(profileID)
	if s == nil || s.root == nil || profileID == "" {
		return state.CodexProfileSummary{}, false
	}
	if s.root.CodexProfiles != nil {
		if profile, ok := s.root.CodexProfiles[profileID]; ok {
			return normalizeCodexProfileSummary(profile), true
		}
	}
	for _, profile := range s.CodexProfiles() {
		if strings.EqualFold(strings.TrimSpace(profile.ID), profileID) {
			return normalizeCodexProfileSummary(profile), true
		}
	}
	return state.CodexProfileSummary{}, false
}

func (s *Service) surfaceCodexProfileSummary(surface *state.SurfaceConsoleRecord) (state.CodexProfileSummary, bool) {
	return s.codexProfileSummaryByID(s.surfaceCodexProfileID(surface))
}

func fixedCodexAPIProfileModel(profile state.CodexProfileSummary) (string, bool) {
	profile = normalizeCodexProfileSummary(profile)
	if profile.Kind != state.CodexProfileKindAPI {
		return "", false
	}
	model := strings.TrimSpace(profile.Model)
	if model == "" || isDynamicCodexProfileModel(profile.BaseURL, model) {
		return "", false
	}
	return model, true
}

func fixedCodexAPIProfileReasoning(profile state.CodexProfileSummary) string {
	if _, ok := fixedCodexAPIProfileModel(profile); !ok {
		return ""
	}
	return normalizeModelReasoningEffort(profile.ReasoningEffort)
}

func isDynamicCodexProfileModel(baseURL, model string) bool {
	if codexcatalog.IsDeepSeekProfile(baseURL, model) {
		return true
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return true
	}
	for _, prefix := range []string{"gpt-", "o1", "o3", "o4", "codex-"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
}

func (s *Service) setSurfaceCodexProviderID(surface *state.SurfaceConsoleRecord, providerID string) {
	if surface == nil {
		return
	}
	surface.CodexProviderID = state.NormalizeDesiredCodexProviderID(providerID)
}
