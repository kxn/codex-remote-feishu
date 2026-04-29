package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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
	if surface == nil {
		return state.DefaultClaudeProfileID
	}
	profileID := strings.TrimSpace(surface.ClaudeProfileID)
	mode := state.NormalizeProductMode(surface.ProductMode)
	backend := state.NormalizeSurfaceBackend(mode, surface.Backend)
	if profileID == "" && mode == state.ProductModeNormal && backend == agentproto.BackendClaude {
		profileID = state.DefaultClaudeProfileID
	}
	if profileID == "" {
		surface.ClaudeProfileID = ""
		return ""
	}
	surface.ClaudeProfileID = state.NormalizeClaudeProfileID(profileID)
	return surface.ClaudeProfileID
}

func (s *Service) setSurfaceClaudeProfileID(surface *state.SurfaceConsoleRecord, profileID string) {
	if surface == nil {
		return
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		surface.ClaudeProfileID = ""
		_ = s.surfaceClaudeProfileID(surface)
		return
	}
	surface.ClaudeProfileID = state.NormalizeClaudeProfileID(profileID)
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
		AccessMode:      surface.PromptOverride.AccessMode,
		PlanMode:        surface.PlanMode,
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
	surface.PlanMode = state.PlanModeSettingOff
	if s.root != nil && s.root.ClaudeWorkspaceProfileSnapshots != nil {
		if record, ok := s.root.ClaudeWorkspaceProfileSnapshots[key]; ok {
			record = state.NormalizeClaudeWorkspaceProfileSnapshotRecord(record)
			surface.PromptOverride.ReasoningEffort = record.ReasoningEffort
			surface.PromptOverride.AccessMode = record.AccessMode
			surface.PlanMode = record.PlanMode
		}
	}
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
}

func (s *Service) applyCurrentClaudeProfileToHeadlessCommand(surface *state.SurfaceConsoleRecord, command *control.DaemonCommand) {
	if surface == nil || command == nil {
		return
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal || s.surfaceBackend(surface) != agentproto.BackendClaude {
		command.ClaudeProfileID = ""
		return
	}
	command.ClaudeProfileID = s.surfaceClaudeProfileID(surface)
}
