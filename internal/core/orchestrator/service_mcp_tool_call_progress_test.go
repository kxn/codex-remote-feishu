package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestMCPToolCallProgressUsesSharedProcessCard(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityVerbose

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "inProgress",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server": "docs",
			"tool":   "lookup",
		},
	})
	if len(started) != 1 || started[0].Kind != eventcontract.KindExecCommandProgress || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected one shared progress start event, got %#v", started)
	}
	if started[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected mcp progress to reply to original source message, got %#v", started[0])
	}
	progress := started[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "mcp_tool_call" || progress.Entries[0].Label != "MCP" || progress.Entries[0].Summary != "docs.lookup" {
		t.Fatalf("unexpected shared start progress payload: %#v", progress)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "mcp-1", "om-progress-1")

	duplicate := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "inProgress",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server": "docs",
			"tool":   "lookup",
		},
	})
	if len(duplicate) != 0 {
		t.Fatalf("expected duplicate mcp start event to be suppressed, got %#v", duplicate)
	}

	failed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "failed",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server":       "docs",
			"tool":         "lookup",
			"errorMessage": "connector timeout",
			"durationMs":   12,
		},
	})
	if len(failed) != 1 || failed[0].Kind != eventcontract.KindExecCommandProgress || failed[0].ExecCommandProgress == nil {
		t.Fatalf("expected one shared progress failure event, got %#v", failed)
	}
	progress = failed[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected failed mcp update to reuse shared card, got %#v", progress)
	}
	if len(progress.Entries) != 1 || progress.Entries[0].Summary != "docs.lookup（失败：connector timeout）" {
		t.Fatalf("unexpected failed shared progress payload: %#v", progress.Entries)
	}
}

func TestMCPToolCallProgressDoesNotReviveAfterAssistantText(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityVerbose

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "inProgress",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server": "docs",
			"tool":   "lookup",
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected shared progress start event, got %#v", started)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Delta:    "先给你结果。",
	}); len(events) != 0 {
		t.Fatalf("expected assistant text to not emit extra progress events, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text to terminate shared progress, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "completed",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server":     "docs",
			"tool":       "lookup",
			"durationMs": 12,
		},
	})
	if len(completed) != 0 {
		t.Fatalf("expected completed mcp tool call not to revive shared progress after assistant text, got %#v", completed)
	}
}

func TestMCPToolCallProgressNormalVerbosityShowsSharedProcessCard(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)
	svc.root.Surfaces["surface-1"].Verbosity = state.SurfaceVerbosityNormal

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "inProgress",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server": "docs",
			"tool":   "lookup",
		},
	})
	if len(started) != 1 || started[0].Kind != eventcontract.KindExecCommandProgress || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected normal verbosity to show mcp progress, got %#v", started)
	}
	progress := started[0].ExecCommandProgress
	if len(progress.Entries) != 1 || progress.Entries[0].Kind != "mcp_tool_call" || progress.Entries[0].Summary != "docs.lookup" {
		t.Fatalf("unexpected normal mcp progress payload: %#v", progress)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "mcp-1", "om-progress-1")

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "mcp-1",
		ItemKind: "mcp_tool_call",
		Status:   "completed",
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
		Metadata: map[string]any{
			"server":     "docs",
			"tool":       "lookup",
			"durationMs": 12,
		},
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected normal verbosity to update mcp progress on completion, got %#v", completed)
	}
	progress = completed[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "om-progress-1" || len(progress.Entries) != 1 || progress.Entries[0].Summary != "docs.lookup（12 ms）" {
		t.Fatalf("unexpected normal completion payload: %#v", progress)
	}
}
