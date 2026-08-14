package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTypedReadProgressMergesAcrossCommandAndDynamicToolCarriers(t *testing.T) {
	now := time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"a.go"},
		}}},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected typed command read progress, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"b.go"},
		}}},
		Metadata: map[string]any{"tool": "read"},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected typed dynamic read progress, got %#v", second)
	}
	timeline := second[0].ExecCommandProgress.Timeline
	if len(timeline) != 1 || timeline[0].Kind != "read" || len(timeline[0].Items) != 2 || timeline[0].Items[0] != "a.go" || timeline[0].Items[1] != "b.go" {
		t.Fatalf("expected one cross-carrier read group, got %#v", timeline)
	}
}

func TestGenericDynamicToolProgressOnlyMergesConsecutiveCalls(t *testing.T) {
	now := time.Date(2026, 8, 12, 14, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "调用工具", "turn-1")

	tool := func(kind agentproto.EventKind, itemID, target, status string) []eventcontract.Event {
		return svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     kind,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   itemID,
			ItemKind: "dynamic_tool_call",
			Status:   status,
			Metadata: map[string]any{
				"tool":      "fetch",
				"arguments": map[string]any{"target": target},
			},
		})
	}
	if events := tool(agentproto.EventItemStarted, "tool-1", "first", ""); len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected first generic tool group, got %#v", events)
	}
	command := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(command) != 1 || command[0].ExecCommandProgress == nil {
		t.Fatalf("expected command barrier, got %#v", command)
	}
	second := tool(agentproto.EventItemStarted, "tool-2", "second", "")
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected second generic tool group, got %#v", second)
	}
	assertRows := func(timeline []control.ExecCommandProgressTimelineItem, firstStatus string) {
		t.Helper()
		if len(timeline) != 3 || timeline[0].Kind != "dynamic_tool_call" || timeline[0].Summary != "first" || timeline[0].Status != firstStatus || timeline[1].Kind != "command_execution" || timeline[2].Kind != "dynamic_tool_call" || timeline[2].Summary != "second" {
			t.Fatalf("expected dynamic-command-dynamic groups in arrival order, got %#v", timeline)
		}
	}
	assertRows(second[0].ExecCommandProgress.Timeline, "started")

	completed := tool(agentproto.EventItemCompleted, "tool-1", "first", "completed")
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected old tool completion update, got %#v", completed)
	}
	assertRows(completed[0].ExecCommandProgress.Timeline, "completed")
}

func TestGenericDynamicToolGroupStaysActiveUntilAllCallsComplete(t *testing.T) {
	now := time.Date(2026, 8, 12, 14, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "调用工具", "turn-1")

	apply := func(kind agentproto.EventKind, itemID, target, status string) []eventcontract.Event {
		return svc.ApplyAgentEvent("inst-1", agentproto.Event{
			Kind:     kind,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   itemID,
			ItemKind: "dynamic_tool_call",
			Status:   status,
			Metadata: map[string]any{
				"tool":      "fetch",
				"arguments": map[string]any{"target": target},
			},
		})
	}
	apply(agentproto.EventItemStarted, "tool-1", "first", "")
	second := apply(agentproto.EventItemStarted, "tool-2", "second", "")
	if len(second) != 1 || second[0].ExecCommandProgress == nil || len(second[0].ExecCommandProgress.Timeline) != 1 {
		t.Fatalf("expected one merged active group, got %#v", second)
	}

	firstCompleted := apply(agentproto.EventItemCompleted, "tool-1", "first", "completed")
	if len(firstCompleted) != 0 {
		t.Fatalf("expected first completion not to finalize a group with another active call, events=%#v groups=%#v", firstCompleted, surface.ActiveExecProgress.DynamicToolGroups)
	}
	progress := execprogress.Snapshot(surface.ActiveExecProgress)
	if progress == nil || len(progress.Timeline) != 1 || progress.Timeline[0].Status != "started" {
		t.Fatalf("expected merged group to stay active, got %#v", progress)
	}

	lastCompleted := apply(agentproto.EventItemCompleted, "tool-2", "second", "completed")
	if len(lastCompleted) != 1 || lastCompleted[0].ExecCommandProgress == nil || lastCompleted[0].ExecCommandProgress.Timeline[0].Status != "completed" {
		t.Fatalf("expected final call completion to finalize the group, got %#v", lastCompleted)
	}
}

func TestDynamicToolCallProgressReadWaitsForNonEmptyPath(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments":         map[string]any{},
		},
	})
	if len(started) != 0 {
		t.Fatalf("expected empty read arguments not to emit a progress card, got %#v", started)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments": map[string]any{
				"filePath": "/data/dl/msu2_firmware/User/main.c",
			},
			"rawOutput": map[string]any{
				"output": "full file contents that must stay out of progress",
			},
		},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected completed read to emit progress with file path, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected read exploration timeline item, got %#v", progress.Timeline)
	}
	items := progress.Timeline[0].Items
	if len(items) != 1 || items[0] != "/data/dl/msu2_firmware/User/main.c" {
		t.Fatalf("expected completed read to display file path, got %#v", progress.Timeline[0])
	}
}

func TestDynamicToolCallTypedExplorationFinalEmptyFallsBackToGeneric(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind: agentproto.ExplorationActionRead,
		}}},
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments":         map[string]any{},
		},
	})
	if len(started) != 0 {
		t.Fatalf("expected incomplete started action to wait, got %#v", started)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind: agentproto.ExplorationActionRead,
		}}},
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments":         map[string]any{},
		},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected final empty action to fall back to generic progress, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "dynamic_tool_call" || progress.Timeline[0].Summary == "" {
		t.Fatalf("expected non-empty generic tool row, got %#v", progress.Timeline)
	}
}

func TestDynamicToolCallSemanticExplorationWithoutActionFallsBackToGeneric(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "探索代码", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool":              "grep",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments": map[string]any{
				"pattern": "needle",
			},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected unsupported semantic exploration tool to stay visible, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "dynamic_tool_call" || rows[0].Summary != "needle" {
		t.Fatalf("expected generic grep row instead of permanent suppression, got %#v", rows)
	}
}

func TestDynamicToolCallTypedExplorationDoesNotReadRawOutput(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind: agentproto.ExplorationActionRead,
		}}},
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments":         map[string]any{},
		},
	})
	if len(started) != 0 {
		t.Fatalf("expected incomplete start to wait, got %#v", started)
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind: agentproto.ExplorationActionRead,
		}}},
		Metadata: map[string]any{
			"tool":              "read",
			"semanticKind":      "exploration",
			"suppressFinalText": true,
			"arguments":         map[string]any{},
			"rawOutput": map[string]any{
				"output": "/secret/path/from/file-contents.go",
			},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected generic fallback for incomplete typed action, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "dynamic_tool_call" {
		t.Fatalf("expected generic tool row, got %#v", rows)
	}
	secret := "/secret/path/from/file-contents.go"
	if strings.Contains(rows[0].Summary, secret) || strings.Contains(rows[0].Secondary, secret) || strings.Contains(strings.Join(rows[0].Items, " "), secret) {
		t.Fatalf("raw output leaked into progress summary: %#v", rows[0])
	}
}

func TestDynamicToolCallTypedExplorationCompletionOnlyWithoutPriorProgress(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"internal/core/state/types.go"},
		}}},
		Metadata: map[string]any{
			"tool":      "read",
			"arguments": map[string]any{},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected completion-only typed dynamic tool progress, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "read" || rows[0].Status != "completed" {
		t.Fatalf("unexpected completion-only typed dynamic tool row: %#v", rows)
	}
	if len(rows[0].Items) != 1 || rows[0].Items[0] != "internal/core/state/types.go" {
		t.Fatalf("unexpected completion-only read items: %#v", rows[0])
	}
}

func TestDynamicToolCallTypedEmptyCarrierSuppressesLegacyReadParser(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventItemStarted,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		ItemID:      "tool-1",
		ItemKind:    "dynamic_tool_call",
		Exploration: &agentproto.ExplorationActions{},
		Metadata: map[string]any{
			"tool": "read",
			"arguments": map[string]any{
				"filePath": "internal/core/state/types.go",
			},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected authoritative rejection to stay visible as generic, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "dynamic_tool_call" {
		t.Fatalf("expected generic row instead of legacy read exploration, got %#v", rows)
	}
}

func TestDynamicToolCallValidTypedExplorationIgnoresRawOutput(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读取文件", "turn-1")
	secret := "RAW_OUTPUT_SECRET_881"

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"internal/core/state/types.go"},
		}}},
		Metadata: map[string]any{
			"tool":      "read",
			"arguments": map[string]any{},
			"rawOutput": map[string]any{"output": secret},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected valid typed exploration progress, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "read" {
		t.Fatalf("expected structured read row, got %#v", rows)
	}
	visible := rows[0].Summary + " " + rows[0].Secondary + " " + strings.Join(rows[0].Items, " ")
	if strings.Contains(visible, secret) {
		t.Fatalf("raw output leaked into structured exploration row: %#v", rows[0])
	}
}
