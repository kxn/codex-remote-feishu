package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func normalizePathPickerDropdownCursor(cursor int, optionCount int) int {
	if optionCount <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= optionCount {
		return optionCount - 1
	}
	return cursor
}

func pathPickerEntryIndexByKind(entries []control.FeishuPathPickerEntry, kind control.PathPickerEntryKind, selectedPath string) int {
	selectedPath = strings.TrimSpace(selectedPath)
	index := 0
	for _, entry := range entries {
		if entry.Disabled || entry.Kind != kind {
			continue
		}
		if entry.Selected && selectedPath != "" {
			return index
		}
		index++
	}
	return -1
}

func pathPickerEntriesByKind(entries []control.FeishuPathPickerEntry, kind control.PathPickerEntryKind) []control.FeishuPathPickerEntry {
	filtered := make([]control.FeishuPathPickerEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Disabled || entry.Kind != kind {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
