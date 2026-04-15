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
