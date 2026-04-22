package execprogress

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func CloneFileChange(record *state.ExecCommandProgressFileChangeRecord) *control.ExecCommandProgressFileChange {
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

func UpsertFileChangeProgressEntries(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) bool {
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
	UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:     entryID,
		Kind:       "file_change",
		Label:      "修改",
		Summary:    fileChangeProgressSummary(*fileChange),
		Status:     NormalizeStatus(status, final),
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

func fileChangeLineCounts(change agentproto.FileChangeRecord) (int, int) {
	added, removed := unifiedDiffLineCounts(change.Diff)
	if added != 0 || removed != 0 {
		return added, removed
	}
	switch change.Kind {
	case agentproto.FileChangeAdd:
		return logicalLineCount(change.Diff), 0
	case agentproto.FileChangeDelete:
		return 0, logicalLineCount(change.Diff)
	default:
		return 0, 0
	}
}

func logicalLineCount(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func unifiedDiffLineCounts(diff string) (int, int) {
	if strings.TrimSpace(diff) == "" {
		return 0, 0
	}
	added := 0
	removed := 0
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"), strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}
