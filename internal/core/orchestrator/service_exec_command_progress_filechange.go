package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleFileChangeProgressStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Status = normalizeExecCommandProgressStatus(event.Status, false)
	if !upsertFileChangeProgressEntries(progress, event, false) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleFileChangeProgressCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Status = normalizeExecCommandProgressStatus(event.Status, true)
	if !upsertFileChangeProgressEntries(progress, event, true) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func cloneExecCommandProgressFileChange(record *state.ExecCommandProgressFileChangeRecord) *control.ExecCommandProgressFileChange {
	if record == nil {
		return nil
	}
	return &control.ExecCommandProgressFileChange{
		Path:         record.Path,
		MovePath:     record.MovePath,
		Kind:         record.Kind,
		Diff:         record.Diff,
		AddedLines:   record.AddedLines,
		RemovedLines: record.RemovedLines,
	}
}

func upsertFileChangeProgressEntries(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) bool {
	if progress == nil || len(event.FileChanges) == 0 {
		return false
	}
	changed := false
	for _, change := range event.FileChanges {
		if !upsertFileChangeProgressEntry(progress, event.ItemID, event.Status, change, final) {
			continue
		}
		changed = true
	}
	return changed
}

func upsertFileChangeProgressEntry(progress *state.ExecCommandProgressRecord, itemID, status string, change agentproto.FileChangeRecord, final bool) bool {
	fileChange, ok := buildExecCommandProgressFileChange(change)
	if !ok {
		return false
	}
	entryID := fileChangeProgressEntryID(itemID, *fileChange)
	upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:     entryID,
		Kind:       "file_change",
		Label:      "修改",
		Summary:    fileChangeProgressSummary(*fileChange),
		Status:     normalizeExecCommandProgressStatus(status, final),
		FileChange: fileChange,
	})
	return true
}

func buildExecCommandProgressFileChange(change agentproto.FileChangeRecord) (*state.ExecCommandProgressFileChangeRecord, bool) {
	path := strings.TrimSpace(change.Path)
	movePath := strings.TrimSpace(change.MovePath)
	if path == "" && movePath == "" {
		return nil, false
	}
	added, removed := fileChangeLineCounts(change)
	return &state.ExecCommandProgressFileChangeRecord{
		Path:         path,
		MovePath:     movePath,
		Kind:         strings.TrimSpace(string(change.Kind)),
		Diff:         strings.TrimSpace(change.Diff),
		AddedLines:   added,
		RemovedLines: removed,
	}, true
}

func fileChangeProgressEntryID(itemID string, change state.ExecCommandProgressFileChangeRecord) string {
	parts := []string{"file_change"}
	if trimmed := strings.TrimSpace(itemID); trimmed != "" {
		parts = append(parts, trimmed)
	}
	if path := strings.TrimSpace(change.Path); path != "" {
		parts = append(parts, path)
	}
	if movePath := strings.TrimSpace(change.MovePath); movePath != "" {
		parts = append(parts, "->", movePath)
	}
	if len(parts) == 1 {
		return "file_change::unknown"
	}
	return strings.Join(parts, "::")
}

func fileChangeProgressSummary(change state.ExecCommandProgressFileChangeRecord) string {
	path := strings.TrimSpace(change.Path)
	movePath := strings.TrimSpace(change.MovePath)
	switch {
	case path != "" && movePath != "":
		return path + " -> " + movePath
	case path != "":
		return path
	default:
		return movePath
	}
}
