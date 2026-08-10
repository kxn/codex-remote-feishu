package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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
