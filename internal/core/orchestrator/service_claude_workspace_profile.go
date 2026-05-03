package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) MaterializeClaudeWorkspaceProfileSnapshots(entries map[string]state.ClaudeWorkspaceProfileSnapshotRecord) {
	if s.root == nil {
		return
	}
	s.root.ClaudeWorkspaceProfileSnapshots = map[string]state.ClaudeWorkspaceProfileSnapshotRecord{}
	for key, entry := range entries {
		key = strings.TrimSpace(key)
		entry = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(entry)
		if key == "" || state.ClaudeWorkspaceProfileSnapshotRecordEmpty(entry) {
			continue
		}
		s.root.ClaudeWorkspaceProfileSnapshots[key] = entry
	}
}

func (s *Service) MaterializeClaudeProfiles(records []state.ClaudeProfileRecord) {
	if s.root == nil {
		return
	}
	s.root.ClaudeProfiles = map[string]state.ClaudeProfileRecord{}
	defaultRecord := state.NormalizeClaudeProfileRecord(state.ClaudeProfileRecord{
		ID:      state.DefaultClaudeProfileID,
		Name:    state.DefaultClaudeProfileName,
		BuiltIn: true,
	})
	s.root.ClaudeProfiles[defaultRecord.ID] = defaultRecord
	for _, record := range records {
		current := state.NormalizeClaudeProfileRecord(record)
		if current.ID == "" {
			continue
		}
		s.root.ClaudeProfiles[current.ID] = current
	}
}

func (s *Service) ClaudeProfiles() []state.ClaudeProfileRecord {
	if s.root == nil || len(s.root.ClaudeProfiles) == 0 {
		return []state.ClaudeProfileRecord{state.NormalizeClaudeProfileRecord(state.ClaudeProfileRecord{
			ID:      state.DefaultClaudeProfileID,
			Name:    state.DefaultClaudeProfileName,
			BuiltIn: true,
		})}
	}
	profiles := make([]state.ClaudeProfileRecord, 0, len(s.root.ClaudeProfiles))
	for _, record := range s.root.ClaudeProfiles {
		profiles = append(profiles, state.NormalizeClaudeProfileRecord(record))
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		left := profiles[i]
		right := profiles[j]
		if left.BuiltIn != right.BuiltIn {
			return left.BuiltIn
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return profiles
}

func (s *Service) ClaudeWorkspaceProfileSnapshots() map[string]state.ClaudeWorkspaceProfileSnapshotRecord {
	if s.root == nil || len(s.root.ClaudeWorkspaceProfileSnapshots) == 0 {
		return map[string]state.ClaudeWorkspaceProfileSnapshotRecord{}
	}
	cloned := make(map[string]state.ClaudeWorkspaceProfileSnapshotRecord, len(s.root.ClaudeWorkspaceProfileSnapshots))
	for key, entry := range s.root.ClaudeWorkspaceProfileSnapshots {
		cloned[key] = entry
	}
	return cloned
}

func (s *Service) claudeProfileRecord(profileID string) state.ClaudeProfileRecord {
	profileID = state.NormalizeClaudeProfileID(profileID)
	if s.root != nil && s.root.ClaudeProfiles != nil {
		if record, ok := s.root.ClaudeProfiles[profileID]; ok {
			return state.NormalizeClaudeProfileRecord(record)
		}
	}
	return state.NormalizeClaudeProfileRecord(state.ClaudeProfileRecord{
		ID:      profileID,
		Name:    profileID,
		BuiltIn: profileID == state.DefaultClaudeProfileID,
	})
}

func (s *Service) claudeProfileReasoningEffort(profileID string) string {
	return s.claudeProfileRecord(profileID).ReasoningEffort
}

func (s *Service) claudeProfileDisplayName(profileID string) string {
	return s.claudeProfileRecord(profileID).Name
}

func (s *Service) SurfaceClaudeProfileID(surfaceID string) string {
	if s.root == nil {
		return ""
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return ""
	}
	return s.surfaceClaudeProfileID(surface)
}

func (s *Service) surfaceClaudeProfileID(surface *state.SurfaceConsoleRecord) string {
	return state.EffectiveSurfaceClaudeProfileID(s.surfaceDesiredContract(surface))
}

func (s *Service) setSurfaceClaudeProfileID(surface *state.SurfaceConsoleRecord, profileID string) {
	if surface == nil {
		return
	}
	surface.ClaudeProfileID = state.NormalizeDesiredClaudeProfileID(profileID)
}

func (s *Service) currentClaudeWorkspaceProfileSnapshotKey(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return ""
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal || s.surfaceBackend(surface) != agentproto.BackendClaude {
		return ""
	}
	workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
	if workspaceKey == "" {
		return ""
	}
	return state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, s.surfaceClaudeProfileID(surface))
}

func (s *Service) currentClaudeWorkspaceProfileSnapshotRecord(surface *state.SurfaceConsoleRecord) state.ClaudeWorkspaceProfileSnapshotRecord {
	if surface == nil {
		return state.ClaudeWorkspaceProfileSnapshotRecord{}
	}
	return state.NormalizeClaudeWorkspaceProfileSnapshotRecord(state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: surface.PromptOverride.ReasoningEffort,
	})
}

func (s *Service) persistCurrentClaudeWorkspaceProfileSnapshot(surface *state.SurfaceConsoleRecord) {
	if surface == nil || s.root == nil {
		return
	}
	key := s.currentClaudeWorkspaceProfileSnapshotKey(surface)
	if key == "" {
		return
	}
	if s.root.ClaudeWorkspaceProfileSnapshots == nil {
		s.root.ClaudeWorkspaceProfileSnapshots = map[string]state.ClaudeWorkspaceProfileSnapshotRecord{}
	}
	record := s.currentClaudeWorkspaceProfileSnapshotRecord(surface)
	if state.ClaudeWorkspaceProfileSnapshotRecordEmpty(record) {
		delete(s.root.ClaudeWorkspaceProfileSnapshots, key)
		return
	}
	s.root.ClaudeWorkspaceProfileSnapshots[key] = record
}

func (s *Service) restoreCurrentClaudeWorkspaceProfileSnapshot(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	key := s.currentClaudeWorkspaceProfileSnapshotKey(surface)
	if key == "" {
		return
	}
	surface.PromptOverride.Model = ""
	surface.PromptOverride.ReasoningEffort = ""
	surface.PromptOverride.AccessMode = ""
	clearSurfacePlanModeOverride(surface)
	if s.root != nil && s.root.ClaudeWorkspaceProfileSnapshots != nil {
		if record, ok := s.root.ClaudeWorkspaceProfileSnapshots[key]; ok {
			record = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(record)
			surface.PromptOverride.ReasoningEffort = record.ReasoningEffort
		}
	}
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
}
