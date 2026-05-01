package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExecCommandProgressVerboseEmitsStartAndTracksCommandHistory(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	if len(started) != 1 || started[0].Kind != eventcontract.KindExecCommandProgress || started[0].ExecCommandProgress == nil {
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
	if len(secondStarted) != 1 || secondStarted[0].Kind != eventcontract.KindExecCommandProgress || secondStarted[0].ExecCommandProgress == nil {
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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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

func TestFileChangeProgressNormalVerbosityShowsSharedProgressCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "改一下文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "in_progress",
		FileChanges: []agentproto.FileChangeRecord{
			{
				Path: "internal/core/orchestrator/service.go",
				Kind: agentproto.FileChangeUpdate,
				Diff: "@@ -1 +1 @@\n-old\n+new",
			},
			{
				Path:     "docs/guide.md",
				MovePath: "docs/guide-v2.md",
				Kind:     agentproto.FileChangeUpdate,
				Diff:     "@@ -1 +1 @@\n-old title\n+new title",
			},
		},
	})
	if len(started) != 1 || started[0].Kind != eventcontract.KindExecCommandProgress || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected file_change to emit shared progress in normal verbosity, got %#v", started)
	}
	progress := started[0].ExecCommandProgress
	if progress.Verbosity != string(state.SurfaceVerbosityNormal) || progress.ItemID != "file-1" {
		t.Fatalf("unexpected file_change progress payload: %#v", progress)
	}
	if len(progress.Entries) != 2 {
		t.Fatalf("expected one shared progress entry per changed file, got %#v", progress.Entries)
	}
	if progress.Entries[0].Kind != "file_change" || progress.Entries[0].Label != "修改" || progress.Entries[0].FileChange == nil {
		t.Fatalf("expected first file change entry to stay structured, got %#v", progress.Entries)
	}
	if progress.Entries[0].FileChange.Path != "internal/core/orchestrator/service.go" || progress.Entries[0].FileChange.AddedLines != 1 || progress.Entries[0].FileChange.RemovedLines != 1 {
		t.Fatalf("unexpected first file change payload: %#v", progress.Entries[0].FileChange)
	}
	if progress.Entries[1].FileChange == nil || progress.Entries[1].FileChange.MovePath != "docs/guide-v2.md" {
		t.Fatalf("expected rename payload to stay structured, got %#v", progress.Entries[1].FileChange)
	}
	if len(progress.Timeline) != 2 || progress.Timeline[0].Kind != "file_change" || progress.Timeline[1].Kind != "file_change" {
		t.Fatalf("expected file changes to enter canonical shared progress timeline, got %#v", progress.Timeline)
	}
}

func TestFileChangeProgressCompletedReusesExistingSharedProgressCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "改一下文件", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "in_progress",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "service.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected started file_change event, got %#v", started)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "file-1", "om-progress-1")

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "service.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +2 @@\n-old\n+new\n+newer",
		}},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected completed file_change to refresh shared progress, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected completed file_change to reuse existing card, got %#v", progress)
	}
	if len(progress.Entries) != 1 || progress.Entries[0].FileChange == nil {
		t.Fatalf("expected updated file_change entry, got %#v", progress.Entries)
	}
	if progress.Entries[0].FileChange.AddedLines != 2 || progress.Entries[0].FileChange.RemovedLines != 1 {
		t.Fatalf("expected completed file_change to refresh line counts, got %#v", progress.Entries[0].FileChange)
	}
}

func TestFileChangeProgressQuietVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "改一下文件", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "in_progress",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "service.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	if len(events) != 0 {
		t.Fatalf("expected quiet verbosity to suppress file_change progress, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected quiet verbosity not to retain file_change progress, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestWebSearchProgressNormalVerbositySuppressesCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityQuiet

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "查一下", "turn-1")

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

func TestProcessPlanProgressNormalVerbosityEmitsStructuredBlock(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "做计划", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "plan-1",
		ItemKind: "process_plan",
		Status:   "completed",
		Metadata: map[string]any{
			"planSnapshot": map[string]any{
				"explanation": "Gathering evidence",
				"steps": []any{
					map[string]any{"step": "Gather evidence", "status": "in_progress"},
					map[string]any{"step": "Write summary", "status": "pending"},
				},
			},
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected process plan progress card, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Blocks) != 1 || progress.Blocks[0].Kind != "process_plan" || len(progress.Blocks[0].Rows) != 3 {
		t.Fatalf("expected structured process plan block, got %#v", progress.Blocks)
	}
}

func TestDelegatedTaskProgressNormalVerbosityEmitsEntry(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "开子任务", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "task-1",
		ItemKind: "delegated_task",
		Metadata: map[string]any{
			"description":  "Audit the repository",
			"subagentType": "Explore",
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected delegated task progress card, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "delegated_task" || progress.Entries[0].Summary != "Explore · Audit the repository" {
		t.Fatalf("unexpected delegated task entry: %#v", progress.Entries)
	}
}

func TestDelegatedTaskProgressCompletionUpdatesSameEntry(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "开子任务", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "task-1",
		ItemKind: "delegated_task",
		Metadata: map[string]any{
			"description":  "Audit the repository",
			"subagentType": "Explore",
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected delegated task start progress, got %#v", started)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "task-1", "om-progress-1")

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "task-1",
		ItemKind: "delegated_task",
		Status:   "completed",
		Metadata: map[string]any{
			"description":  "Audit the repository",
			"subagentType": "Explore",
		},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected delegated task completion progress, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected delegated task completion to reuse same card, got %#v", progress)
	}
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "delegated_task" || progress.Entries[0].Status != "completed" {
		t.Fatalf("unexpected delegated task completion entry: %#v", progress.Entries)
	}
	if progress.Entries[0].Summary != "Explore · Audit the repository" {
		t.Fatalf("expected delegated task completion to keep summary, got %#v", progress.Entries[0])
	}
}

func TestDynamicToolCallProgressVerboseMergesSameToolRows(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读两个文件", "turn-1")

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
	if len(first) != 1 || first[0].Kind != eventcontract.KindExecCommandProgress || first[0].ExecCommandProgress == nil {
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
	if len(second) != 1 || second[0].Kind != eventcontract.KindExecCommandProgress || second[0].ExecCommandProgress == nil {
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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityNormal

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "读文件", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "先看看代码", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "按顺序看看", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "按顺序看一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "看下两个文件", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "搜一下", "turn-1")

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
	action, ok := execprogress.ParseCommandExecutionExplorationAction(`/bin/bash -lc "rg -n \"func execCommandMetadata|dynamicToolProgressArguments|dynamicToolProgressSummaryFromMetadata|metadataString\" internal/core/orchestrator -g '"'!**/*_test.go'"'"`)
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
			action, ok := execprogress.ParseCommandExecutionExplorationAction(tt.command)
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
	if action, ok := execprogress.ParseCommandExecutionExplorationAction(`bash -lc 'journalctl --user -u codex-remote.service -n 400 --no-pager | rg -n "rg |command_execution|tool_call|exec|progress"'`); ok {
		t.Fatalf("expected piped command not to parse as exploration search, got %#v", action)
	}
}

func TestExecCommandProgressStopsAfterAssistantTextAppears(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	var sawFinalProgress bool
	for _, event := range finished {
		if event.Kind == eventcontract.KindExecCommandProgress && event.ExecCommandProgress != nil {
			sawFinalProgress = true
			if len(event.ExecCommandProgress.Entries) != 1 || event.ExecCommandProgress.Entries[0].Status != "failed" {
				t.Fatalf("expected final progress update to mark command failed, got %#v", event.ExecCommandProgress)
			}
		}
	}
	if !sawFinalProgress {
		t.Fatalf("expected turn completion to emit one final exec progress update before clearing, got %#v", finished)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected turn completion to clear exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressVerboseEmitsEnglishTimelineEntry(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

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
	if len(events) != 1 || events[0].Kind != eventcontract.KindExecCommandProgress || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected one reasoning progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "reasoning_summary" || progress.Entries[0].Summary != "Considering Git commands" {
		t.Fatalf("expected reasoning to surface as english timeline entry, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "reasoning_summary" || progress.Timeline[0].Summary != "Considering Git commands" {
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

func TestReasoningSummaryProgressAccumulatesPlainTextDeltas(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Considering",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first reasoning delta event, got %#v", first)
	}
	if got := first[0].ExecCommandProgress.Timeline[0].Summary; got != "Considering" {
		t.Fatalf("expected first reasoning frame to surface first fragment, got %#v", first[0].ExecCommandProgress.Timeline)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " possible fixes",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected second reasoning delta inside throttle window to be coalesced, got %#v", second)
	}
	if record := svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning; record == nil || record.Text != "Considering possible fixes" {
		t.Fatalf("expected reasoning record to keep accumulated plain-text summary, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning)
	}

	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")
	now = now.Add(execCommandProgressReasoningFlushInterval)
	tick := svc.Tick(now)
	if len(tick) != 1 || tick[0].ExecCommandProgress == nil {
		t.Fatalf("expected tick to flush coalesced reasoning delta after throttle window, got %#v", tick)
	}
	progress := tick[0].ExecCommandProgress
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected tick flush to update existing progress card, got %#v", progress)
	}
	if got := progress.Timeline[0].Summary; got != "Considering possible fixes" {
		t.Fatalf("expected coalesced reasoning summary on tick flush, got %#v", progress.Timeline)
	}
}

func TestReasoningSummaryProgressKeepsCheckingPhraseInEnglish(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

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
	if len(events[0].ExecCommandProgress.Timeline) != 1 || events[0].ExecCommandProgress.Timeline[0].Summary != "Checking workflow progress" {
		t.Fatalf("expected checking phrase to stay in english timeline, got %#v", events[0].ExecCommandProgress.Timeline)
	}
}

func TestReasoningSummaryProgressKeepsDifferentSummaryIndexesAsTimelineRows(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Reviewing existing flow",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first reasoning summary event, got %#v", first)
	}
	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning a safer update",
		Metadata: map[string]any{
			"summaryIndex": 2,
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected second reasoning summary inside throttle window to be coalesced, got %#v", second)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")
	now = now.Add(execCommandProgressReasoningFlushInterval)
	tick := svc.Tick(now)
	if len(tick) != 1 || tick[0].ExecCommandProgress == nil {
		t.Fatalf("expected tick to flush second reasoning row, got %#v", tick)
	}
	timeline := tick[0].ExecCommandProgress.Timeline
	if len(timeline) != 2 ||
		timeline[0].Kind != "reasoning_summary" ||
		timeline[0].Summary != "Reviewing existing flow" ||
		timeline[1].Kind != "reasoning_summary" ||
		timeline[1].Summary != "Planning a safer update" {
		t.Fatalf("expected separate summary indexes to persist as separate timeline rows, got %#v", timeline)
	}
}

func TestReasoningSummaryProgressDoesNotAnimateWithoutNewDelta(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

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
	if len(first[0].ExecCommandProgress.Timeline) != 1 || first[0].ExecCommandProgress.Timeline[0].Summary != "Thinking" {
		t.Fatalf("expected reasoning timeline to keep raw text, got %#v", first[0].ExecCommandProgress.Timeline)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	now = now.Add(10 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no synthetic reasoning animation update, got %#v", tick)
	}
}

func TestReasoningSummaryProgressPersistsBeforeOrdinaryProgressEntries(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " changes",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta to be coalesced before ordinary progress, got %#v", coalesced)
	}

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
	if len(progress.Timeline) != 2 ||
		progress.Timeline[0].Kind != "reasoning_summary" ||
		progress.Timeline[0].Summary != "Planning changes" ||
		progress.Timeline[1].Kind != "command_execution" ||
		progress.Timeline[1].Summary != "npm test" {
		t.Fatalf("expected reasoning timeline entry to persist before command progress, got %#v", progress.Timeline)
	}
}

func TestReasoningSummaryProgressPersistsBeforeExplorationRows(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "cat docs/README.md",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected exploration progress update, got %#v", second)
	}
	timeline := second[0].ExecCommandProgress.Timeline
	if len(timeline) != 2 ||
		timeline[0].Kind != "reasoning_summary" ||
		timeline[0].Summary != "Planning" ||
		timeline[1].Kind != "read" ||
		len(timeline[1].Items) != 1 ||
		timeline[1].Items[0] != "docs/README.md" {
		t.Fatalf("expected reasoning timeline entry to persist before exploration row, got %#v", timeline)
	}
}

func TestReasoningSummaryProgressIsNotClearedBeforeAssistantTextStartsNewCard(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Thinking",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " about response shape",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta before assistant text to be coalesced, got %#v", coalesced)
	}

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	})
	if len(started) != 0 {
		t.Fatalf("expected assistant message start not to retract reasoning progress, got %#v", started)
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
	}); len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected assistant text delta to flush dirty reasoning before clearing progress, got %#v", events)
	} else if progress := events[0].ExecCommandProgress; progress.MessageID != "om-progress-1" ||
		len(progress.Timeline) != 1 ||
		progress.Timeline[0].Summary != "Thinking about response shape" {
		t.Fatalf("expected assistant text delta to flush latest reasoning snapshot, got %#v", progress)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text delta to terminate shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressPersistsOnTurnCompletion(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressMessage("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " final answer",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta before turn completion to be coalesced, got %#v", coalesced)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	var progressEvent *control.ExecCommandProgress
	for _, event := range finished {
		if event.Kind == eventcontract.KindExecCommandProgress {
			progressEvent = event.ExecCommandProgress
			break
		}
	}
	if progressEvent == nil {
		t.Fatalf("expected turn completion to finalize reasoning progress, got %#v", finished)
	}
	progress := progressEvent
	if progress.MessageID != "om-progress-1" {
		t.Fatalf("expected final progress snapshot on completion, got %#v", progress)
	}
	if len(progress.Timeline) != 1 ||
		progress.Timeline[0].Kind != "reasoning_summary" ||
		progress.Timeline[0].Summary != "Planning final answer" ||
		progress.Timeline[0].Status != "completed" {
		t.Fatalf("expected reasoning entry to persist and finalize on completion, got %#v", progress.Timeline)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected turn completion to clear shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}
