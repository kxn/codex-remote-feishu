package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCommandExecutionTypedExplorationOverridesLegacyShellParser(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "先看看代码", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"internal/core/orchestrator/service.go"},
		}}},
		Metadata: map[string]any{
			"command": "cat internal/core/orchestrator/service.go && npm test",
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected typed exploration progress, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected typed read to override legacy shell rejection, got %#v", progress.Timeline)
	}
	if len(progress.Timeline[0].Items) != 1 || progress.Timeline[0].Items[0] != "internal/core/orchestrator/service.go" {
		t.Fatalf("unexpected typed read row: %#v", progress.Timeline[0])
	}
}

func TestCommandExecutionTypedExplorationRejectsMixedUnknownAtomically(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "执行命令", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{
			{Kind: agentproto.ExplorationActionRead, Items: []string{"a.go"}},
			{Kind: agentproto.ExplorationActionKind("unknown"), Summary: "npm test"},
		}},
		Metadata: map[string]any{
			"command": "cat a.go",
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected generic command progress, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "command_execution" || progress.Timeline[0].Summary != "cat a.go" {
		t.Fatalf("expected mixed typed actions to fall back atomically, got %#v", progress.Timeline)
	}
}

func TestCommandExecutionTypedEmptyCarrierCompletionForcesGenericFallback(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "执行命令", "turn-1")

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
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first exploration row, got %#v", first)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventItemCompleted,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		ItemID:      "cmd-2",
		ItemKind:    "command_execution",
		Status:      "completed",
		Exploration: &agentproto.ExplorationActions{},
		Metadata:    map[string]any{"command": "cat b.go"},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected authoritative final rejection to emit generic progress, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if len(progress.Timeline) != 2 || progress.Timeline[1].Kind != "command_execution" || progress.Timeline[1].Summary != "cat b.go" {
		t.Fatalf("expected empty carrier to forbid legacy parser and force generic fallback, got %#v", progress.Timeline)
	}
}

func TestCommandExecutionTypedPendingKeepsExistingExplorationRunning(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"a.go"},
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected first exploration row, got %#v", started)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"a.go"},
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(finished) != 1 || finished[0].ExecCommandProgress == nil || finished[0].ExecCommandProgress.Timeline[0].Status != "completed" {
		t.Fatalf("expected first exploration item to complete, got %#v", finished)
	}

	pending := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind: agentproto.ExplorationActionSearch,
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(pending) != 0 {
		t.Fatalf("expected pending details not to emit a new row, got %#v", pending)
	}
	progress := execprogress.Snapshot(surface.ActiveExecProgress)
	if progress == nil || len(progress.Timeline) != 1 || progress.Timeline[0].Status != "running" {
		t.Fatalf("expected pending item to keep existing exploration block running, got %#v", progress)
	}
}

func TestCommandExecutionTypedExplorationCompletionOnlyAppendsAfterExistingRows(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

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
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first exploration row, got %#v", first)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:      agentproto.ExplorationActionSearch,
			Summary:   "needle",
			Secondary: "internal/",
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected completion-only exploration update, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if len(progress.Timeline) != 2 || progress.Timeline[1].Kind != "search" || progress.Timeline[1].Summary != "needle" {
		t.Fatalf("expected completion-only item after existing row, got %#v", progress.Timeline)
	}
}

func TestCommandExecutionTypedExplorationCompletionOnlyWithoutPriorProgress(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:      agentproto.ExplorationActionSearch,
			Summary:   "needle",
			Secondary: "internal/",
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected completion-only typed exploration progress, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "search" || rows[0].Summary != "needle" || rows[0].Status != "completed" {
		t.Fatalf("unexpected completion-only typed exploration row: %#v", rows)
	}
}

func TestCommandExecutionTypedExplorationWritesMultipleActionsInOrder(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{
			{Kind: agentproto.ExplorationActionRead, Items: []string{" a.go ", "", "a.go", "b.go"}},
			{Kind: agentproto.ExplorationActionList, Summary: " internal/ "},
			{Kind: agentproto.ExplorationActionSearch, Summary: " needle ", Secondary: " pkg/ "},
		}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected typed multi-action progress, got %#v", events)
	}
	rows := events[0].ExecCommandProgress.Timeline
	if len(rows) != 3 || rows[0].Kind != "read" || rows[1].Kind != "list" || rows[2].Kind != "search" {
		t.Fatalf("expected ordered read/list/search rows, got %#v", rows)
	}
	if len(rows[0].Items) != 2 || rows[0].Items[0] != "a.go" || rows[0].Items[1] != "b.go" {
		t.Fatalf("expected trimmed unique read items, got %#v", rows[0])
	}
	if rows[1].Summary != "internal/" || rows[2].Summary != "needle" || rows[2].Secondary != "pkg/" {
		t.Fatalf("expected normalized list/search summaries, got %#v", rows)
	}
}

func TestCommandExecutionTypedExplorationRejectsIncompleteBatchAtomically(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{
			{Kind: agentproto.ExplorationActionRead, Items: []string{"a.go"}},
			{Kind: agentproto.ExplorationActionSearch},
		}},
		Metadata: map[string]any{"command": "cat a.go"},
	})
	if len(started) != 0 {
		t.Fatalf("expected incomplete batch to stay pending without partial rows, got %#v", started)
	}
	if progress := execprogress.Snapshot(surface.ActiveExecProgress); progress == nil || len(progress.Timeline) != 0 {
		t.Fatalf("expected no partial exploration row, got %#v", progress)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{
			{Kind: agentproto.ExplorationActionRead, Items: []string{"a.go"}},
			{Kind: agentproto.ExplorationActionSearch},
		}},
		Metadata: map[string]any{"command": "cat a.go"},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected final incomplete batch to fall back to generic, got %#v", completed)
	}
	rows := completed[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "command_execution" || rows[0].Summary != "cat a.go" {
		t.Fatalf("expected atomic generic fallback without read row, got %#v", rows)
	}
}

func TestCommandExecutionExplorationResolutionStaysGenericAfterStart(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "执行命令", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventItemStarted,
		ThreadID:    "thread-1",
		TurnID:      "turn-1",
		ItemID:      "cmd-1",
		ItemKind:    "command_execution",
		Exploration: &agentproto.ExplorationActions{},
		Metadata:    map[string]any{"command": "cat a.go"},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected generic start, got %#v", started)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"a.go"},
		}}},
		Metadata: map[string]any{"command": "cat a.go"},
	})
	if len(completed) != 0 {
		t.Fatalf("expected generic completion not to emit a second representation, got %#v", completed)
	}
	progress := execprogress.Snapshot(surface.ActiveExecProgress)
	if progress == nil || len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "command_execution" || progress.Timeline[0].Status != "completed" {
		t.Fatalf("expected sticky generic resolution, got %#v", progress)
	}
}

func TestCommandExecutionExplorationResolutionStaysStructuredWhenCompletionCarrierMissing(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查看代码", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Exploration: &agentproto.ExplorationActions{Actions: []agentproto.ExplorationAction{{
			Kind:  agentproto.ExplorationActionRead,
			Items: []string{"a.go"},
		}}},
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected structured start, got %#v", started)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{"command": "npm test"},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected structured completion status update, got %#v", completed)
	}
	rows := completed[0].ExecCommandProgress.Timeline
	if len(rows) != 1 || rows[0].Kind != "read" || rows[0].Status != "completed" {
		t.Fatalf("expected sticky structured resolution, got %#v", rows)
	}
}
