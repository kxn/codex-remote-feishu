package execprogress

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFinalizeRetiresVisibleReasoningAndRunningEntries(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		Entries: []state.ExecCommandProgressEntryRecord{
			{ItemID: "reason", Kind: "reasoning_summary", Status: "completed"},
			{ItemID: "task", Kind: "delegated_task", Status: "running"},
			{ItemID: "done", Kind: "file_change", Status: "completed"},
		},
		Reasoning: &state.ExecCommandProgressReasoningRecord{Active: true, VisibleEntryID: "reason"},
	}
	Finalize(progress, "failed")
	if progress.Reasoning.Active || progress.Entries[0].Status != "failed" || progress.Entries[1].Status != "failed" || progress.Entries[2].Status != "completed" {
		t.Fatalf("unexpected finalized progress: %+v", progress)
	}
}
