package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
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
	s.root.ClaudeProfiles = materializeProfileCatalogRecords(records, defaultClaudeProfileRecord(), state.NormalizeClaudeProfileRecord, claudeProfileRecordID)
}

func (s *Service) ClaudeProfiles() []state.ClaudeProfileRecord {
	if s.root == nil || len(s.root.ClaudeProfiles) == 0 {
		return []state.ClaudeProfileRecord{defaultClaudeProfileRecord()}
	}
	return sortedProfileCatalogRecords(s.root.ClaudeProfiles, state.NormalizeClaudeProfileRecord, claudeProfileCatalogSortKey)
}

func defaultClaudeProfileRecord() state.ClaudeProfileRecord {
	return state.NormalizeClaudeProfileRecord(state.ClaudeProfileRecord{
		ID:      state.DefaultClaudeProfileID,
		Name:    state.DefaultClaudeProfileName,
		BuiltIn: true,
	})
}

func claudeProfileRecordID(record state.ClaudeProfileRecord) string {
	return record.ID
}

func claudeProfileCatalogSortKey(record state.ClaudeProfileRecord) profileCatalogSortKey {
	return profileCatalogSortKey{BuiltIn: record.BuiltIn, Name: record.Name, ID: record.ID}
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
	override := surface.PromptOverride
	if settings := state.EffectiveSurfaceCapabilitySettings(s.root, surface); settings.Source == state.SurfaceCapabilitySettingsSourceBot {
		override = settings.PromptOverride
	}
	return state.NormalizeClaudeWorkspaceProfileSnapshotRecord(state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: override.ReasoningEffort,
		AccessMode:      override.AccessMode,
	})
}

func (s *Service) persistCurrentClaudeWorkspaceProfileSnapshot(surface *state.SurfaceConsoleRecord) {
	if surface == nil || s.root == nil {
		return
	}
	if s.botCapabilitySettingsInvalid(surface) {
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

func (s *Service) restoreCurrentClaudeWorkspaceProfileSnapshot(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.botCapabilitySettingsInvalid(surface) {
		return s.botCapabilitySettingsInvalidEvents(surface)
	}
	key := s.currentClaudeWorkspaceProfileSnapshotKey(surface)
	if key == "" {
		return nil
	}
	override := state.ModelConfigRecord{}
	if s.root != nil && s.root.ClaudeWorkspaceProfileSnapshots != nil {
		if record, ok := s.root.ClaudeWorkspaceProfileSnapshots[key]; ok {
			record = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(record)
			override.ReasoningEffort = record.ReasoningEffort
			override.AccessMode = record.AccessMode
		}
	}
	override = compactPromptOverride(override)
	reasoning := override.ReasoningEffort
	accessMode := override.AccessMode
	if !s.applySurfaceCapabilityLifecycleMutation(surface, func(record *state.BotCapabilitySettingsRecord) {
		// reasoning 仍为机器人级；access/plan 为会话级，只写 surface。
		record.PromptOverride = state.ModelConfigRecord{ReasoningEffort: reasoning}
	}, func(local *state.SurfaceConsoleRecord) {
		local.PromptOverride = state.ModelConfigRecord{ReasoningEffort: reasoning}
	}) {
		return s.botCapabilitySettingsInvalidEvents(surface)
	}
	// access/plan 会话级：无论走 bot record 还是 local 分支，都直接落在 surface。
	surface.PromptOverride.AccessMode = accessMode
	clearSurfacePlanModeOverride(surface)
	return nil
}
