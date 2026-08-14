package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExecCommandProgressSnapshotBuildsSingleTimelineAcrossExplorationAndEntries(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
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

func TestRolloverCarryoverOnlyAdvancesActiveExplorationRows(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		LastVisibleSeq: 2,
		Exploration: &state.ExecCommandProgressExplorationRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []state.ExecCommandProgressBlockRowRecord{
					{RowID: "read::1", Kind: "read", Items: []string{"done.go"}, Status: "completed", LastSeq: 1},
					{RowID: "read::2", Kind: "read", Items: []string{"active.go"}, Status: "running", LastSeq: 2},
				},
			},
		},
	}

	execprogress.RolloverCarryoverEntries(progress, 3)

	rows := progress.Exploration.Block.Rows
	if rows[0].LastSeq != 1 {
		t.Fatalf("expected completed exploration history to stay frozen, got %#v", rows)
	}
	if rows[1].LastSeq != 3 || progress.LastVisibleSeq != 3 {
		t.Fatalf("expected only active exploration row to carry into the new segment, got %#v", progress)
	}
}

func TestRolloverCarryoverKeepsActiveReasoningSegmentOwnership(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		Verbosity:      state.SurfaceVerbosityChatty,
		LastVisibleSeq: 1,
		Entries: []state.ExecCommandProgressEntryRecord{{
			ItemID:  "reasoning-1::segment::1",
			Kind:    "reasoning_summary",
			Summary: "Checking",
			Status:  "running",
			LastSeq: 1,
		}},
		Reasoning: &state.ExecCommandProgressReasoningRecord{
			ItemID:          "reasoning-1",
			VisibleEntryID:  "reasoning-1::segment::1",
			VisibleAfterSeq: 1,
			Active:          true,
		},
	}

	execprogress.RolloverCarryoverEntries(progress, 2)

	if progress.Entries[0].LastSeq != 2 || progress.Reasoning.VisibleAfterSeq != 2 {
		t.Fatalf("expected active reasoning ownership to follow its carried row, got %#v", progress)
	}
}

func TestRolloverCarryoverKeepsActiveReadAsCurrentMergeGroup(t *testing.T) {
	progress := &state.ExecCommandProgressRecord{
		LastVisibleSeq: 2,
		Exploration: &state.ExecCommandProgressExplorationRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []state.ExecCommandProgressBlockRowRecord{
					{RowID: "read::1", Kind: "read", Items: []string{"active.go"}, Status: "running", LastSeq: 1},
					{RowID: "list::2", Kind: "list", Summary: "internal", Status: "completed", LastSeq: 2},
				},
			},
			ActiveItemIDs: map[string]bool{"cmd-1": true},
			ItemRowIDs:    map[string][]string{"cmd-1": {"read::1"}},
		},
	}
	execprogress.RolloverCarryoverEntries(progress, 3)

	result := execprogress.ResolveExplorationProgressForCommandExecution(progress, agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"next.go"},
		}}},
	}, false)
	if result.Disposition != execprogress.ExplorationDispositionStructured {
		t.Fatalf("expected structured read, got %#v", result)
	}
	rows := progress.Exploration.Block.Rows
	if len(rows) != 2 || len(rows[0].Items) != 2 || rows[0].Items[1] != "next.go" {
		t.Fatalf("expected post-rollover read to merge into the current visible active group, got %#v", rows)
	}
}
