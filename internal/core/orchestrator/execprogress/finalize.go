package execprogress

import (
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"strings"
)

func FinalizeReasoning(progress *state.ExecCommandProgressRecord, status string) bool {
	if progress == nil {
		return false
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "completed"
	}
	changed := false
	if progress.Reasoning != nil && progress.Reasoning.Active {
		progress.Reasoning.Active = false
		changed = true
	}
	if progress.Reasoning != nil && strings.TrimSpace(progress.Reasoning.VisibleEntryID) != "" {
		for i := range progress.Entries {
			if progress.Entries[i].ItemID != progress.Reasoning.VisibleEntryID {
				continue
			}
			if progress.Entries[i].Status != status {
				progress.Entries[i].Status = status
				changed = true
			}
		}
	}
	return changed
}

func Finalize(progress *state.ExecCommandProgressRecord, status string) {
	if progress == nil {
		return
	}
	for i := range progress.Entries {
		entry := &progress.Entries[i]
		if strings.TrimSpace(entry.Status) == "running" || strings.TrimSpace(entry.Status) == "started" {
			entry.Status = status
		}
	}
	FinalizeReasoning(progress, status)
	FinalizeExploration(progress, status)
}
