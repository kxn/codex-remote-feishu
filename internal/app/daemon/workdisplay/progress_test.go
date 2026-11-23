package workdisplay

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRolloverKeepsRunningProgressOnCurrentCard(t *testing.T) {
	display := workstore.PrivateExecutionDisplay{Progress: state.ExecCommandProgressRecord{
		Verbosity: state.SurfaceVerbosityVerbose, LastVisibleSeq: 3,
		Entries: []state.ExecCommandProgressEntryRecord{
			{ItemID: "active", Kind: "command_execution", Label: "执行", Summary: "go test active", Status: "running", LastSeq: 1},
			{ItemID: "done", Kind: "delegated_task", Label: "Task", Summary: "old completed task", Status: "completed", LastSeq: 2},
			{ItemID: "new", Kind: "delegated_task", Label: "Task", Summary: "new task", Status: "completed", LastSeq: 3},
		},
	}}
	recordProgressSegment(&display, "second-card", 3, true)
	if display.Progress.Entries[0].LastSeq < 3 || display.Progress.Entries[1].LastSeq != 2 {
		t.Fatalf("wrong rollover ownership: %+v", display.Progress.Entries)
	}
	card, _, _ := feishu.PrivateExecutionProgress(*execprogress.Snapshot(&display.Progress), nil)
	if !strings.Contains(string(card), "go test active") || strings.Contains(string(card), "old completed task") {
		t.Fatalf("wrong current card after rollover: %s", card)
	}
}
