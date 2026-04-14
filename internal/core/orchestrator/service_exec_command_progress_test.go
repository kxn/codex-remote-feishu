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
