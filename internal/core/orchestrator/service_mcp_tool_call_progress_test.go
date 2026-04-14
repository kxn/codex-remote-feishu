package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestMCPToolCallProgressEmitsStartedAndFailedEvents(t *testing.T) {
	svc := prepareRemotePlanTurnForTest(t)

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
	if len(started) != 1 || started[0].Kind != control.UIEventMCPToolCallProgress || started[0].MCPToolCallProgress == nil {
		t.Fatalf("expected one mcp progress start event, got %#v", started)
	}
	if started[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected mcp progress to reply to original source message, got %#v", started[0])
	}
	if started[0].MCPToolCallProgress.Status != "started" || started[0].MCPToolCallProgress.Server != "docs" || started[0].MCPToolCallProgress.Tool != "lookup" {
		t.Fatalf("unexpected started progress payload: %#v", started[0].MCPToolCallProgress)
	}

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
	if len(failed) != 1 || failed[0].Kind != control.UIEventMCPToolCallProgress || failed[0].MCPToolCallProgress == nil {
		t.Fatalf("expected one mcp progress failure event, got %#v", failed)
	}
	if failed[0].MCPToolCallProgress.Status != "failed" || failed[0].MCPToolCallProgress.ErrorMessage != "connector timeout" || failed[0].MCPToolCallProgress.DurationMS != 12 {
		t.Fatalf("unexpected failed progress payload: %#v", failed[0].MCPToolCallProgress)
	}
}
