package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (r *serviceProgressRuntime) recordTurnFileChanges(instanceID string, event agentproto.Event) {
	if event.ItemKind != "file_change" {
		return
	}
	if normalizeFileChangeStatus(event.Status) != "completed" {
		return
	}
	if len(event.FileChanges) == 0 {
		return
	}
	key := turnRenderKey(instanceID, event.ThreadID, event.TurnID)
	summary := r.turnFileChanges[key]
	if summary == nil {
		summary = &turnFileChangeSummary{Files: map[string]*turnFileChangeEntry{}}
		r.turnFileChanges[key] = summary
	}
	for _, change := range event.FileChanges {
		path := strings.TrimSpace(change.Path)
		movePath := strings.TrimSpace(change.MovePath)
		entryKey := path
		if entryKey == "" {
			entryKey = movePath
		}
		if entryKey == "" {
			continue
		}
		entry := summary.Files[entryKey]
		if entry == nil {
			entry = &turnFileChangeEntry{
				Path:     path,
				MovePath: movePath,
			}
			summary.Files[entryKey] = entry
		}
		if entry.Path == "" {
			entry.Path = path
		}
		if movePath != "" {
			entry.MovePath = movePath
		}
		added, removed := fileChangeLineCounts(change)
		entry.AddedLines += added
		entry.RemovedLines += removed
	}
	if len(summary.Files) == 0 {
		delete(r.turnFileChanges, key)
	}
}

func (r *serviceProgressRuntime) takeTurnFileChangeSummary(instanceID, threadID, turnID string) *control.FileChangeSummary {
	key := turnRenderKey(instanceID, threadID, turnID)
	summary := r.turnFileChanges[key]
	if summary == nil || len(summary.Files) == 0 {
		delete(r.turnFileChanges, key)
		return nil
	}
	delete(r.turnFileChanges, key)
	inst := r.service.root.Instances[instanceID]
	var thread *state.ThreadRecord
	if inst != nil && threadID != "" {
		thread = inst.Threads[threadID]
	}
	files := make([]control.FileChangeSummaryEntry, 0, len(summary.Files))
	totalAdded := 0
	totalRemoved := 0
	for _, entry := range summary.Files {
		if entry == nil {
			continue
		}
		files = append(files, control.FileChangeSummaryEntry{
			Path:         entry.Path,
			MovePath:     entry.MovePath,
			AddedLines:   entry.AddedLines,
			RemovedLines: entry.RemovedLines,
		})
		totalAdded += entry.AddedLines
		totalRemoved += entry.RemovedLines
	}
	if len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		left := strings.TrimSpace(files[i].Path)
		if left == "" {
			left = strings.TrimSpace(files[i].MovePath)
		}
		right := strings.TrimSpace(files[j].Path)
		if right == "" {
			right = strings.TrimSpace(files[j].MovePath)
		}
		return strings.Compare(left, right) < 0
	})

	return &control.FileChangeSummary{
		ThreadID:     threadID,
		ThreadTitle:  displayThreadTitle(inst, thread),
		FileCount:    len(files),
		AddedLines:   totalAdded,
		RemovedLines: totalRemoved,
		Files:        files,
	}
}

func normalizeFileChangeStatus(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized
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
