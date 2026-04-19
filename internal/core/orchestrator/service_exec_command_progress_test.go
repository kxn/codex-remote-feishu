package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExecCommandProgressVerboseEmitsStartAndTracksCommandHistory(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "npm test",
			"cwd":     "/data/dl/droid",
		},
	})
	if len(started) != 1 || started[0].Kind != control.UIEventExecCommandProgress || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected exec progress start event, got %#v", started)
	}
	if started[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected progress card to reply to source message, got %#v", started[0])
	}
	progress := started[0].ExecCommandProgress
	if progress.Command != "npm test" || progress.CWD != "/data/dl/droid" || progress.Status != "running" || progress.Final {
		t.Fatalf("unexpected start progress payload: %#v", progress)
	}
	if len(progress.Entries) != 1 || progress.Entries[0].Label != "执行" || progress.Entries[0].Summary != "npm test" {
		t.Fatalf("expected command entry on shared progress card, got %#v", progress.Entries)
	}
	if len(progress.Commands) != 1 || progress.Commands[0] != "npm test" {
		t.Fatalf("expected first command history, got %#v", progress)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

	secondStarted := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "go test ./...",
		},
	})
	if len(secondStarted) != 1 || secondStarted[0].Kind != control.UIEventExecCommandProgress || secondStarted[0].ExecCommandProgress == nil {
		t.Fatalf("expected second exec progress update, got %#v", secondStarted)
	}
	progress = secondStarted[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected second start to update same card, got %#v", progress)
	}
	if len(progress.Entries) != 2 || progress.Entries[0].Summary != "npm test" || progress.Entries[1].Summary != "go test ./..." {
		t.Fatalf("expected shared progress entries to accumulate, got %#v", progress.Entries)
	}
	if len(progress.Commands) != 2 || progress.Commands[0] != "npm test" || progress.Commands[1] != "go test ./..." {
		t.Fatalf("expected accumulated command history, got %#v", progress)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": "go test ./...",
		},
	})
	if len(completed) != 0 {
		t.Fatalf("expected completion not to refresh exec progress card, got %#v", completed)
	}
}

func TestRecordExecCommandProgressMessageStartSeqAdvancesActiveCardWindow(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial progress event, got %#v", started)
	}
	svc.RecordExecCommandProgressMessageStartSeq("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-2", 7)

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "go test ./...",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected follow-up progress event, got %#v", second)
	}
	progress := second[0].ExecCommandProgress
	if progress.MessageID != "om-progress-2" || progress.CardStartSeq != 7 {
		t.Fatalf("expected active progress card state to keep new message and window start, got %#v", progress)
	}
}

func TestExecCommandProgressQuietVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
			"cwd":     "/data/dl/droid",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected quiet verbosity to suppress exec progress, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected quiet verbosity not to retain exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestExecCommandProgressNormalVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
			"cwd":     "/data/dl/droid",
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected normal verbosity to suppress exec progress card, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected normal verbosity not to retain exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestWebSearchProgressNormalVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "查一下", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "web-1",
		ItemKind: "web_search",
		Status:   "running",
	})
	if len(events) != 0 {
		t.Fatalf("expected normal verbosity to suppress web search progress, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected normal verbosity not to retain web search progress, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestWebSearchSharesExecCommandProgressCardInVerbose(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial command progress event, got %#v", started)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

	searchStarted := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "web-1",
		ItemKind: "web_search",
		Status:   "running",
	})
	if len(searchStarted) != 1 || searchStarted[0].ExecCommandProgress == nil {
		t.Fatalf("expected shared progress update for web search, got %#v", searchStarted)
	}
	progress := searchStarted[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected web search to reuse command progress card, got %#v", progress)
	}
	if len(progress.Entries) != 2 {
		t.Fatalf("expected command and web search entries on same card, got %#v", progress.Entries)
	}
	if progress.Entries[0].Label != "执行" || progress.Entries[0].Summary != "npm test" {
		t.Fatalf("expected first shared entry to stay command execution, got %#v", progress.Entries)
	}
	if progress.Entries[1].Label != "搜索" || progress.Entries[1].Summary != "正在搜索网络" {
		t.Fatalf("expected second shared entry to be web search, got %#v", progress.Entries)
	}
}

func TestWebSearchProgressQuietVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "查一下", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "web-1",
		ItemKind: "web_search",
		Status:   "running",
	})
	if len(events) != 0 {
		t.Fatalf("expected quiet verbosity to suppress web search progress, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected quiet verbosity not to retain shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestDynamicToolCallProgressVerboseMergesSameToolRows(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "读两个文件", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "Read",
			"arguments": map[string]any{
				"path": "a.cpp",
			},
		},
	})
	if len(first) != 1 || first[0].Kind != control.UIEventExecCommandProgress || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected dynamic tool progress start, got %#v", first)
	}
	progress := first[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || progress.Blocks[0].Kind != "exploration" || progress.Blocks[0].Status != "running" {
		t.Fatalf("expected dynamic tool read to enter exploration block, got %#v", progress.Blocks)
	}
	if len(progress.Blocks[0].Rows) != 1 || progress.Blocks[0].Rows[0].Kind != "read" || len(progress.Blocks[0].Rows[0].Items) != 1 || progress.Blocks[0].Rows[0].Items[0] != "a.cpp" {
		t.Fatalf("unexpected dynamic tool first exploration row: %#v", progress.Blocks[0].Rows)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-2",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "read",
			"arguments": map[string]any{
				"path": "b.cpp",
			},
		},
	})
	if len(second) != 1 || second[0].Kind != control.UIEventExecCommandProgress || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected dynamic tool merged update, got %#v", second)
	}
	progress = second[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected dynamic tool update to reuse same card, got %#v", progress)
	}
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 1 {
		t.Fatalf("expected merged exploration block, got %#v", progress.Blocks)
	}
	items := progress.Blocks[0].Rows[0].Items
	if len(items) != 2 || items[0] != "a.cpp" || items[1] != "b.cpp" {
		t.Fatalf("expected same tool to merge into one read row, got %#v", progress.Blocks[0].Rows)
	}
}

func TestDynamicToolCallProgressNormalVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "读文件", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "Read",
			"arguments": map[string]any{
				"path": "a.cpp",
			},
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected normal verbosity to suppress dynamic tool progress, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected normal verbosity not to retain progress, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestDynamicToolCallProgressFailedStatusMarksMergedRow(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "读文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool": "Read",
			"arguments": map[string]any{
				"path": "a.cpp",
			},
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected started event, got %#v", started)
	}
	itemID := started[0].ExecCommandProgress.ItemID
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", itemID, "om-progress-1")

	failed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "failed",
		Metadata: map[string]any{
			"tool": "Read",
			"arguments": map[string]any{
				"path": "a.cpp",
			},
		},
	})
	if len(failed) != 1 || failed[0].ExecCommandProgress == nil {
		t.Fatalf("expected failure update event, got %#v", failed)
	}
	progress := failed[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected failure to update existing progress card, got %#v", progress)
	}
	if len(progress.Blocks) != 1 || progress.Blocks[0].Status != "failed" {
		t.Fatalf("expected failed dynamic tool exploration block, got %#v", progress.Blocks)
	}
	if len(progress.Blocks[0].Rows) != 1 || len(progress.Blocks[0].Rows[0].Items) != 1 || progress.Blocks[0].Rows[0].Items[0] != "a.cpp" {
		t.Fatalf("expected failed block to keep read row, got %#v", progress.Blocks)
	}
}

func TestCommandExecutionExplorationProgressBuildsSharedBlock(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "先看看代码", "turn-1")

	readStarted := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat internal/core/control/types.go"`,
		},
	})
	if len(readStarted) != 1 || readStarted[0].ExecCommandProgress == nil {
		t.Fatalf("expected read exploration start, got %#v", readStarted)
	}
	progress := readStarted[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || progress.Blocks[0].Kind != "exploration" || progress.Blocks[0].Status != "running" {
		t.Fatalf("expected exploration block after read start, got %#v", progress.Blocks)
	}
	if len(progress.Blocks[0].Rows) != 1 || progress.Blocks[0].Rows[0].Kind != "read" || len(progress.Blocks[0].Rows[0].Items) != 1 || progress.Blocks[0].Rows[0].Items[0] != "internal/core/control/types.go" {
		t.Fatalf("unexpected read exploration row: %#v", progress.Blocks[0].Rows)
	}
	if len(progress.Entries) != 0 {
		t.Fatalf("expected exploration command to avoid duplicate legacy entries, got %#v", progress.Entries)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

	searchStarted := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "rg compact internal/"`,
		},
	})
	if len(searchStarted) != 1 || searchStarted[0].ExecCommandProgress == nil {
		t.Fatalf("expected search exploration update, got %#v", searchStarted)
	}
	progress = searchStarted[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected search start to update same card, got %#v", progress)
	}
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 2 {
		t.Fatalf("expected shared exploration block with two rows, got %#v", progress.Blocks)
	}
	if progress.Blocks[0].Rows[1].Kind != "search" || progress.Blocks[0].Rows[1].Summary != "compact" || progress.Blocks[0].Rows[1].Secondary != "internal/" {
		t.Fatalf("unexpected search exploration row: %#v", progress.Blocks[0].Rows)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": `bash -lc "cat internal/core/control/types.go"`,
		},
	})
	if len(completed) != 0 {
		t.Fatalf("expected first exploration completion without visible block change to stay quiet, got %#v", completed)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": `bash -lc "rg compact internal/"`,
		},
	})
	if len(finished) != 1 || finished[0].ExecCommandProgress == nil {
		t.Fatalf("expected final exploration completion update, got %#v", finished)
	}
	if finished[0].ExecCommandProgress.Blocks[0].Status != "completed" {
		t.Fatalf("expected exploration block to flip completed, got %#v", finished[0].ExecCommandProgress.Blocks)
	}
}

func TestCommandExecutionExplorationProgressKeepsSeparatedReadGroups(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "按顺序看看", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat foo.txt"`,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first read start, got %#v", first)
	}
	progress := first[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 1 || progress.Blocks[0].Rows[0].Kind != "read" {
		t.Fatalf("expected first read row, got %#v", progress.Blocks)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "ls -la"`,
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected list update, got %#v", second)
	}
	progress = second[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 2 {
		t.Fatalf("expected read + list rows, got %#v", progress.Blocks)
	}
	if progress.Blocks[0].Rows[1].Kind != "list" || progress.Blocks[0].Rows[1].Summary != "ls -la" {
		t.Fatalf("expected upstream-style list summary, got %#v", progress.Blocks[0].Rows)
	}

	third := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-3",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat bar.txt"`,
		},
	})
	if len(third) != 1 || third[0].ExecCommandProgress == nil {
		t.Fatalf("expected second read update, got %#v", third)
	}
	progress = third[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 3 {
		t.Fatalf("expected separated read groups around list row, got %#v", progress.Blocks)
	}
	rows := progress.Blocks[0].Rows
	if rows[0].Kind != "read" || len(rows[0].Items) != 1 || rows[0].Items[0] != "foo.txt" {
		t.Fatalf("unexpected first read row: %#v", rows)
	}
	if rows[1].Kind != "list" || rows[1].Summary != "ls -la" {
		t.Fatalf("unexpected list row: %#v", rows)
	}
	if rows[2].Kind != "read" || len(rows[2].Items) != 1 || rows[2].Items[0] != "bar.txt" {
		t.Fatalf("unexpected second read row: %#v", rows)
	}
}

func TestCommandExecutionExplorationProgressDoesNotMergeReadAcrossExecEntry(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "按顺序看一下", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat foo.txt"`,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first read row, got %#v", first)
	}
	progress := first[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 1 {
		t.Fatalf("expected first read block row, got %#v", progress.Blocks)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected exec entry update, got %#v", second)
	}
	progress = second[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Summary != "npm test" {
		t.Fatalf("expected exec entry barrier, got %#v", progress.Entries)
	}
	if progress.Entries[0].LastSeq != 2 {
		t.Fatalf("expected exec entry to carry visible order seq, got %#v", progress.Entries)
	}

	third := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-3",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat bar.txt"`,
		},
	})
	if len(third) != 1 || third[0].ExecCommandProgress == nil {
		t.Fatalf("expected second read update, got %#v", third)
	}
	progress = third[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 2 {
		t.Fatalf("expected exec entry to break read merge, got %#v", progress.Blocks)
	}
	rows := progress.Blocks[0].Rows
	if rows[0].Kind != "read" || len(rows[0].Items) != 1 || rows[0].Items[0] != "foo.txt" {
		t.Fatalf("unexpected first read row: %#v", rows)
	}
	if rows[1].Kind != "read" || len(rows[1].Items) != 1 || rows[1].Items[0] != "bar.txt" {
		t.Fatalf("unexpected second read row: %#v", rows)
	}
	if rows[0].LastSeq != 1 || rows[1].LastSeq != 3 {
		t.Fatalf("expected read rows to preserve visible order seq across entry barrier, got %#v", rows)
	}
}

func TestCommandExecutionExplorationProgressOnlyMergesSameReadCommand(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "看下两个文件", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "cat foo.txt"`,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first read row, got %#v", first)
	}
	progress := first[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 1 {
		t.Fatalf("expected first read row, got %#v", progress.Blocks)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc "sed -n '1,20p' bar.txt"`,
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected second read update, got %#v", second)
	}
	progress = second[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 2 {
		t.Fatalf("expected different read commands to stay separated, got %#v", progress.Blocks)
	}
	rows := progress.Blocks[0].Rows
	if rows[0].Kind != "read" || len(rows[0].Items) != 1 || rows[0].Items[0] != "foo.txt" {
		t.Fatalf("unexpected first read row: %#v", rows)
	}
	if rows[1].Kind != "read" || len(rows[1].Items) != 1 || rows[1].Items[0] != "bar.txt" {
		t.Fatalf("unexpected second read row: %#v", rows)
	}
}

func TestCommandExecutionExplorationProgressRecognizesQuotedRgRegexSearch(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "搜一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": `bash -lc 'rg -n "surfaceProgressLabel|renderSurfaceProgressBlockRow" web/src/routes/admin/helpers.ts'`,
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected regex search exploration update, got %#v", started)
	}
	progress := started[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || len(progress.Blocks[0].Rows) != 1 {
		t.Fatalf("expected single exploration block row, got %#v", progress.Blocks)
	}
	row := progress.Blocks[0].Rows[0]
	if row.Kind != "search" || row.Summary != "surfaceProgressLabel|renderSurfaceProgressBlockRow" || row.Secondary != "web/src/routes/admin/helpers.ts" {
		t.Fatalf("unexpected regex search exploration row: %#v", row)
	}
	if len(progress.Entries) != 0 {
		t.Fatalf("expected regex search not to fall back to legacy entry, got %#v", progress.Entries)
	}
}

func TestParseCommandExecutionExplorationActionHandlesBashLCQuotedRgGlob(t *testing.T) {
	action, ok := parseCommandExecutionExplorationAction(`/bin/bash -lc "rg -n \"func execCommandMetadata|dynamicToolProgressArguments|dynamicToolProgressSummaryFromMetadata|metadataString\" internal/core/orchestrator -g '"'!**/*_test.go'"'"`)
	if !ok {
		t.Fatal("expected quoted rg command to parse as exploration search")
	}
	if action.Kind != "search" || action.Summary != `func execCommandMetadata|dynamicToolProgressArguments|dynamicToolProgressSummaryFromMetadata|metadataString` || action.Secondary != "internal/core/orchestrator" {
		t.Fatalf("unexpected quoted rg exploration action: %#v", action)
	}
}

func TestParseCommandExecutionExplorationActionRecognizesListCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantSummary string
	}{
		{
			name:        "rg files",
			command:     `bash -lc "rg --files -g '*.css' -g '*.scss'"`,
			wantSummary: `rg --files -g '*.css' -g '*.scss'`,
		},
		{
			name:        "fd",
			command:     `fd -t f src`,
			wantSummary: `fd -t f src`,
		},
		{
			name:        "find",
			command:     `find internal -maxdepth 1`,
			wantSummary: `find internal -maxdepth 1`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := parseCommandExecutionExplorationAction(tt.command)
			if !ok {
				t.Fatalf("expected %q to parse as list action", tt.command)
			}
			if action.Kind != "list" || action.Summary != tt.wantSummary {
				t.Fatalf("unexpected list action: %#v", action)
			}
		})
	}
}

func TestParseCommandExecutionExplorationActionRejectsPipelineSearch(t *testing.T) {
	if action, ok := parseCommandExecutionExplorationAction(`bash -lc 'journalctl --user -u codex-remote.service -n 400 --no-pager | rg -n "rg |command_execution|tool_call|exec|progress"'`); ok {
		t.Fatalf("expected piped command not to parse as exploration search, got %#v", action)
	}
}

func TestExecCommandProgressStopsAfterAssistantTextAppears(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected command progress start event, got %#v", started)
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events on assistant text start, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Delta:    "先给你结果。",
	}); len(events) != 0 {
		t.Fatalf("expected no progress card event once assistant text starts, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text to terminate exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(completed) != 0 {
		t.Fatalf("expected command completion not to resurrect progress card, got %#v", completed)
	}
}

func TestExecCommandProgressFinalizesOnTurnCompletionWithoutAssistantText(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected command progress start event, got %#v", started)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "failed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	for _, event := range finished {
		if event.Kind == control.UIEventExecCommandProgress {
			t.Fatalf("expected turn completion not to refresh exec progress card, got %#v", finished)
		}
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected turn completion to clear exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressVerboseEmitsEnglishTimelineEntry(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Considering Git commands**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(events) != 1 || events[0].Kind != control.UIEventExecCommandProgress || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected one reasoning progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "reasoning_summary" || progress.Entries[0].Summary != "Considering Git commands." {
		t.Fatalf("expected reasoning to surface as english timeline entry, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "reasoning_summary" || progress.Timeline[0].Summary != "Considering Git commands." {
		t.Fatalf("expected reasoning timeline item, got %#v", progress.Timeline)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress == nil {
		t.Fatal("expected reasoning to retain shared progress state")
	}
	record := svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning
	if record == nil || record.Text != "Considering Git commands" {
		t.Fatalf("expected reasoning record to keep raw english text, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning)
	}
}

func TestReasoningSummaryProgressKeepsCheckingPhraseInEnglish(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Checking workflow progress**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected english checking progress event, got %#v", events)
	}
	if len(events[0].ExecCommandProgress.Timeline) != 1 || events[0].ExecCommandProgress.Timeline[0].Summary != "Checking workflow progress." {
		t.Fatalf("expected checking phrase to stay in english timeline, got %#v", events[0].ExecCommandProgress.Timeline)
	}
}

func TestExecCommandProgressReasoningAnimationTicksSlowly(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Thinking**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning timeline event, got %#v", first)
	}
	if len(first[0].ExecCommandProgress.Timeline) != 1 || first[0].ExecCommandProgress.Timeline[0].Summary != "Thinking." {
		t.Fatalf("expected first frame to start with one dot, got %#v", first[0].ExecCommandProgress.Timeline)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	now = now.Add(execCommandProgressTransientAnimationInterval - time.Millisecond)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no animation tick before interval, got %#v", tick)
	}

	now = now.Add(time.Millisecond)
	second := svc.Tick(now)
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected second animation frame, got %#v", second)
	}
	if len(second[0].ExecCommandProgress.Timeline) != 1 || second[0].ExecCommandProgress.Timeline[0].Summary != "Thinking.." {
		t.Fatalf("expected second frame with two dots, got %#v", second[0].ExecCommandProgress.Timeline)
	}
}

func TestReasoningSummaryProgressIsClearedBeforeOrdinaryProgressEntries(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Planning**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected ordinary progress update, got %#v", second)
	}
	progress := second[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected ordinary progress to reuse the same card, got %#v", progress)
	}
	for _, entry := range progress.Entries {
		if entry.Kind == "reasoning_summary" {
			t.Fatalf("expected reasoning timeline entry to clear before ordinary progress, got %#v", progress.Entries)
		}
	}
	if len(progress.Entries) != 1 || progress.Entries[0].Summary != "npm test" {
		t.Fatalf("expected command entry after reasoning clear, got %#v", progress.Entries)
	}
}

func TestReasoningSummaryProgressClearsBeforeAssistantTextStartsNewCard(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Thinking**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	cleared := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	})
	if len(cleared) != 1 || cleared[0].Kind != control.UIEventExecCommandProgress || cleared[0].ExecCommandProgress == nil {
		t.Fatalf("expected reasoning clear event before assistant text card, got %#v", cleared)
	}
	progress := cleared[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected reasoning clear to stay on the same card, got %#v", progress)
	}
	for _, entry := range progress.Entries {
		if entry.Kind == "reasoning_summary" {
			t.Fatalf("expected reasoning entry to be removed before assistant text card, got %#v", progress.Entries)
		}
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress == nil {
		t.Fatal("expected progress state to remain until assistant text is emitted")
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Delta:    "先给你结论。",
	}); len(events) != 0 {
		t.Fatalf("expected assistant text delta not to emit extra progress after clearing reasoning entry, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text delta to terminate shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressClearsOnTurnCompletion(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoContinueSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Planning**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	var progressEvent *control.ExecCommandProgress
	for _, event := range finished {
		if event.Kind == control.UIEventExecCommandProgress {
			progressEvent = event.ExecCommandProgress
			break
		}
	}
	if progressEvent == nil {
		t.Fatalf("expected turn completion to clear reasoning progress, got %#v", finished)
	}
	progress := progressEvent
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected cleared progress snapshot on completion, got %#v", progress)
	}
	for _, entry := range progress.Entries {
		if entry.Kind == "reasoning_summary" {
			t.Fatalf("expected reasoning entry to be removed on completion, got %#v", progress.Entries)
		}
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected turn completion to clear shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}
