package orchestrator

import (
	"testing"

	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExecCommandProgressSnapshotBuildsSingleTimelineAcrossExplorationAndEntries(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Commands: []string{
			`bash -lc "cat foo.txt"`,
			`bash -lc "cat bar.txt"`,
		},
		Entries: []state.ExecCommandProgressEntryRecord{
			{
				ItemID:  "compact-1",
				Kind:    "context_compaction",
				Label:   "压缩",
				Summary: "上下文已压缩。",
				Status:  "completed",
				LastSeq: 2,
			},
		},
		Exploration: &state.ExecCommandProgressExplorationRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []state.ExecCommandProgressBlockRowRecord{
					{RowID: "read-1", Kind: "read", Items: []string{"foo.txt"}, LastSeq: 1},
					{RowID: "read-2", Kind: "read", Items: []string{"bar.txt"}, LastSeq: 3},
				},
			},
		},
	}

	snapshot := execprogress.Snapshot(progress)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if len(snapshot.Timeline) != 3 {
		t.Fatalf("expected unified timeline with three items, got %#v", snapshot.Timeline)
	}
	first := snapshot.Timeline[0]
	second := snapshot.Timeline[1]
	third := snapshot.Timeline[2]
	if first.Kind != "read" || len(first.Items) != 1 || first.Items[0] != "foo.txt" || first.LastSeq != 1 {
		t.Fatalf("unexpected first timeline item: %#v", first)
	}
	if second.Kind != "context_compaction" || second.Summary != "上下文已压缩。" || second.LastSeq != 2 {
		t.Fatalf("unexpected second timeline item: %#v", second)
	}
	if third.Kind != "read" || len(third.Items) != 1 || third.Items[0] != "bar.txt" || third.LastSeq != 3 {
		t.Fatalf("unexpected third timeline item: %#v", third)
	}
	for _, item := range snapshot.Timeline {
		if item.Kind == "command_execution" {
			t.Fatalf("expected command fallback rows to stay out of structured timeline, got %#v", snapshot.Timeline)
		}
	}
}

func TestExecCommandProgressSnapshotFallsBackToCommandsOnlyWhenNoStructuredTimelineExists(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Commands: []string{
			`/bin/bash -lc "npm test"`,
			`bash -lc 'go test ./...'`,
		},
	}

	snapshot := execprogress.Snapshot(progress)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if len(snapshot.Timeline) != 2 {
		t.Fatalf("expected command fallback timeline, got %#v", snapshot.Timeline)
	}
	if snapshot.Timeline[0].Kind != "command_execution" || snapshot.Timeline[0].Summary != `/bin/bash -lc "npm test"` {
		t.Fatalf("unexpected first fallback item: %#v", snapshot.Timeline[0])
	}
	if snapshot.Timeline[1].Kind != "command_execution" || snapshot.Timeline[1].Summary != `bash -lc 'go test ./...'` {
		t.Fatalf("unexpected second fallback item: %#v", snapshot.Timeline[1])
	}
}
