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

func activeProgressMessageID(progress *control.ExecCommandProgress) string {
	if progress == nil {
		return ""
	}
	if progress.ActiveSegmentID != "" {
		for _, segment := range progress.Segments {
			if segment.SegmentID == progress.ActiveSegmentID {
				return segment.MessageID
			}
		}
	}
	if len(progress.Segments) == 0 {
		return ""
	}
	return progress.Segments[len(progress.Segments)-1].MessageID
}

func activeProgressStartSeq(progress *control.ExecCommandProgress) int {
	if progress == nil {
		return 0
	}
	if progress.ActiveSegmentID != "" {
		for _, segment := range progress.Segments {
			if segment.SegmentID == progress.ActiveSegmentID {
				return segment.StartSeq
			}
		}
	}
	if len(progress.Segments) == 0 {
		return 0
	}
	return progress.Segments[len(progress.Segments)-1].StartSeq
}

func timelineItemAt(t *testing.T, progress *control.ExecCommandProgress, index int) control.ExecCommandProgressTimelineItem {
	t.Helper()
	if progress == nil {
		t.Fatal("expected exec command progress payload")
	}
	if index < 0 || index >= len(progress.Timeline) {
		t.Fatalf("expected timeline index %d within %#v", index, progress.Timeline)
	}
	return progress.Timeline[index]
}

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
	if progress.ItemID != "cmd-1" || progress.Verbosity != string(state.SurfaceVerbosityVerbose) {
		t.Fatalf("unexpected start progress payload: %#v", progress)
	}
	first := timelineItemAt(t, progress, 0)
	if len(progress.Timeline) != 1 || first.Kind != "command_execution" || first.Label != "执行" || first.Summary != "npm test" || first.Status != "running" {
		t.Fatalf("expected command entry on shared progress card, got %#v", progress.Timeline)
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected second start to update same card, got %#v", progress)
	}
	if len(progress.Timeline) != 2 || progress.Timeline[0].Summary != "npm test" || progress.Timeline[1].Summary != "go test ./..." {
		t.Fatalf("expected command timeline to accumulate, got %#v", progress.Timeline)
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
	svc.RecordExecCommandProgressSegmentWindow("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-2", 7, 0)

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
	if activeProgressMessageID(progress) != "om-progress-2" || activeProgressStartSeq(progress) != 7 {
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
	if len(progress.Timeline) != 2 || progress.Timeline[0].Kind != "file_change" || progress.Timeline[1].Kind != "file_change" {
		t.Fatalf("expected file changes to enter canonical shared progress timeline, got %#v", progress.Timeline)
	}
	if progress.Timeline[0].Label != "修改" || progress.Timeline[0].FileChange == nil {
		t.Fatalf("expected first file change timeline item to stay structured, got %#v", progress.Timeline)
	}
	if progress.Timeline[0].FileChange.Path != "internal/core/orchestrator/service.go" || progress.Timeline[0].FileChange.AddedLines != 1 || progress.Timeline[0].FileChange.RemovedLines != 1 {
		t.Fatalf("unexpected first file change payload: %#v", progress.Timeline[0].FileChange)
	}
	if progress.Timeline[1].FileChange == nil || progress.Timeline[1].FileChange.MovePath != "docs/guide-v2.md" {
		t.Fatalf("expected rename payload to stay structured, got %#v", progress.Timeline[1].FileChange)
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
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "file-1", "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected completed file_change to reuse existing card, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].FileChange == nil {
		t.Fatalf("expected updated file_change timeline item, got %#v", progress.Timeline)
	}
	if progress.Timeline[0].FileChange.AddedLines != 2 || progress.Timeline[0].FileChange.RemovedLines != 1 {
		t.Fatalf("expected completed file_change to refresh line counts, got %#v", progress.Timeline[0].FileChange)
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
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected web search to reuse command progress card, got %#v", progress)
	}
	if len(progress.Timeline) != 2 {
		t.Fatalf("expected command and web search items on same card, got %#v", progress.Timeline)
	}
	if progress.Timeline[0].Label != "执行" || progress.Timeline[0].Summary != "npm test" {
		t.Fatalf("expected first timeline item to stay command execution, got %#v", progress.Timeline)
	}
	if progress.Timeline[1].Label != "搜索" || progress.Timeline[1].Summary != "正在搜索网络" {
		t.Fatalf("expected second timeline item to be web search, got %#v", progress.Timeline)
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "delegated_task" || progress.Timeline[0].Summary != "Explore · Audit the repository" {
		t.Fatalf("unexpected delegated task timeline item: %#v", progress.Timeline)
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
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "task-1", "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected delegated task completion to reuse same card, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "delegated_task" || progress.Timeline[0].Status != "completed" {
		t.Fatalf("unexpected delegated task completion item: %#v", progress.Timeline)
	}
	if progress.Timeline[0].Summary != "Explore · Audit the repository" {
		t.Fatalf("expected delegated task completion to keep summary, got %#v", progress.Timeline[0])
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" || progress.Timeline[0].Status != "running" {
		t.Fatalf("expected dynamic tool read to enter timeline, got %#v", progress.Timeline)
	}
	if len(progress.Timeline[0].Items) != 1 || progress.Timeline[0].Items[0] != "a.cpp" {
		t.Fatalf("unexpected dynamic tool first exploration row: %#v", progress.Timeline[0])
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected dynamic tool update to reuse same card, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected merged exploration timeline item, got %#v", progress.Timeline)
	}
	items := progress.Timeline[0].Items
	if len(items) != 2 || items[0] != "a.cpp" || items[1] != "b.cpp" {
		t.Fatalf("expected same tool to merge into one read row, got %#v", progress.Timeline[0])
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
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", itemID, "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected failure to update existing progress card, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].Status != "failed" {
		t.Fatalf("expected failed dynamic tool exploration item, got %#v", progress.Timeline)
	}
	if len(progress.Timeline[0].Items) != 1 || progress.Timeline[0].Items[0] != "a.cpp" {
		t.Fatalf("expected failed item to keep read row, got %#v", progress.Timeline)
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" || progress.Timeline[0].Status != "running" {
		t.Fatalf("expected exploration read item after read start, got %#v", progress.Timeline)
	}
	if len(progress.Timeline[0].Items) != 1 || progress.Timeline[0].Items[0] != "internal/core/control/types.go" {
		t.Fatalf("unexpected read exploration row: %#v", progress.Timeline[0])
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

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
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected search start to update same card, got %#v", progress)
	}
	if len(progress.Timeline) != 2 {
		t.Fatalf("expected shared exploration timeline with two rows, got %#v", progress.Timeline)
	}
	if progress.Timeline[1].Kind != "search" || progress.Timeline[1].Summary != "compact" || progress.Timeline[1].Secondary != "internal/" {
		t.Fatalf("unexpected search exploration row: %#v", progress.Timeline)
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
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected first exploration completion to update its row status, got %#v", completed)
	}
	completedRows := completed[0].ExecCommandProgress.Timeline
	if len(completedRows) != 2 || completedRows[0].Status != "completed" || completedRows[1].Status != "running" {
		t.Fatalf("expected per-row exploration lifecycle, got %#v", completedRows)
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
	if len(finished[0].ExecCommandProgress.Timeline) == 0 || finished[0].ExecCommandProgress.Timeline[0].Status != "completed" {
		t.Fatalf("expected exploration timeline to flip completed, got %#v", finished[0].ExecCommandProgress.Timeline)
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected first read row, got %#v", progress.Timeline)
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

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
	if len(progress.Timeline) != 2 {
		t.Fatalf("expected read + list rows, got %#v", progress.Timeline)
	}
	if progress.Timeline[1].Kind != "list" || progress.Timeline[1].Summary != "ls -la" {
		t.Fatalf("expected upstream-style list summary, got %#v", progress.Timeline)
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
	if len(progress.Timeline) != 3 {
		t.Fatalf("expected separated read groups around list row, got %#v", progress.Timeline)
	}
	rows := progress.Timeline
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected first read block row, got %#v", progress.Timeline)
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", progress.ItemID, "om-progress-1")

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
	if len(progress.Timeline) != 2 || progress.Timeline[1].Kind != "command_execution" || progress.Timeline[1].Summary != "npm test" {
		t.Fatalf("expected exec entry barrier, got %#v", progress.Timeline)
	}
	if progress.Timeline[1].LastSeq != 2 {
		t.Fatalf("expected exec entry to carry visible order seq, got %#v", progress.Timeline)
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
	if len(progress.Timeline) != 3 {
		t.Fatalf("expected exec entry to break read merge, got %#v", progress.Timeline)
	}
	rows := progress.Timeline
	if rows[0].Kind != "read" || len(rows[0].Items) != 1 || rows[0].Items[0] != "foo.txt" {
		t.Fatalf("unexpected first read row: %#v", rows)
	}
	if rows[1].Kind != "command_execution" || rows[1].Summary != "npm test" {
		t.Fatalf("unexpected exec barrier row: %#v", rows)
	}
	if rows[2].Kind != "read" || len(rows[2].Items) != 1 || rows[2].Items[0] != "bar.txt" {
		t.Fatalf("unexpected second read row: %#v", rows)
	}
	if rows[0].LastSeq != 1 || rows[2].LastSeq != 3 {
		t.Fatalf("expected read rows to preserve visible order seq across entry barrier, got %#v", rows)
	}
}

func TestCommandExecutionExplorationProgressMergesReadAcrossShellCommands(t *testing.T) {
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
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "read" {
		t.Fatalf("expected first read row, got %#v", progress.Timeline)
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
	if len(progress.Timeline) != 1 {
		t.Fatalf("expected visible read semantics to merge across shell commands, got %#v", progress.Timeline)
	}
	rows := progress.Timeline
	if rows[0].Kind != "read" || len(rows[0].Items) != 2 || rows[0].Items[0] != "foo.txt" || rows[0].Items[1] != "bar.txt" {
		t.Fatalf("unexpected merged read row: %#v", rows)
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
	if len(progress.Timeline) != 1 {
		t.Fatalf("expected single exploration block row, got %#v", progress.Timeline)
	}
	row := progress.Timeline[0]
	if row.Kind != "search" || row.Summary != "surfaceProgressLabel|renderSurfaceProgressBlockRow" || row.Secondary != "web/src/routes/admin/helpers.ts" {
		t.Fatalf("unexpected regex search exploration row: %#v", row)
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

func TestExecCommandProgressSealsOnFirstAssistantTextDelta(t *testing.T) {
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
		t.Fatalf("expected first non-empty assistant text delta to seal exec progress immediately, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected assistant text completion to stay pending until next visible event, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text completion to leave the old progress sealed, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
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
	if len(completed) != 1 || completed[0].Kind != eventcontract.KindBlockCommitted || completed[0].Block == nil {
		t.Fatalf("expected command completion to flush pending assistant text before sealing progress, got %#v", completed)
	}
	if completed[0].Block.Text != "先给你结果。" {
		t.Fatalf("unexpected pending assistant text flush: %#v", completed[0].Block)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected visible assistant text flush to terminate exec progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestCommandExecutionOutputDeltaDoesNotEmitLivePatch(t *testing.T) {
	now := time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC)
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
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected command start progress event, got %#v", started)
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution_output",
		Delta:    "line 1\n",
	}); len(events) != 0 {
		t.Fatalf("expected command output delta not to emit live Feishu patch, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution_output",
		Delta:    "line 2\n",
	}); len(events) != 0 {
		t.Fatalf("expected repeated command output delta not to emit live Feishu patch, got %#v", events)
	}
	if progress := svc.root.Surfaces["surface-1"].ActiveExecProgress; progress == nil || len(progress.Entries) != 1 || progress.Entries[0].Kind != "command_execution" {
		t.Fatalf("expected output deltas to leave command progress state unchanged, got %#v", progress)
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
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

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
			if len(event.ExecCommandProgress.Timeline) != 1 || event.ExecCommandProgress.Timeline[0].Status != "failed" {
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

func TestExplorationProgressRowsFinalizeOnTurnFailure(t *testing.T) {
	now := time.Date(2026, 8, 12, 15, 0, 0, 0, time.UTC)
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
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected exploration progress, got %#v", started)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "failed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	for _, event := range finished {
		if event.ExecCommandProgress == nil {
			continue
		}
		if len(event.ExecCommandProgress.Timeline) != 1 || event.ExecCommandProgress.Timeline[0].Status != "failed" {
			t.Fatalf("expected active exploration row to finalize failed, got %#v", event.ExecCommandProgress)
		}
		return
	}
	t.Fatalf("expected final exploration progress event, got %#v", finished)
}
