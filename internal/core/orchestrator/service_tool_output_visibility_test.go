package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDynamicToolCallProgressSuppressFinalTextIgnoresMetadataText(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "运行工具", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Metadata: map[string]any{
			"tool":              "custom_tool",
			"suppressFinalText": true,
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected dynamic tool progress start, got %#v", started)
	}

	rawText := "raw tool result that must stay out of user-visible progress"
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "tool-1",
		ItemKind: "dynamic_tool_call",
		Status:   "completed",
		Metadata: map[string]any{
			"tool":              "custom_tool",
			"text":              rawText,
			"suppressFinalText": true,
		},
	})
	for _, event := range completed {
		if event.Block != nil && strings.Contains(event.Block.Text, rawText) {
			t.Fatalf("suppressed dynamic tool text leaked into final block: %#v", completed)
		}
		if event.ExecCommandProgress != nil {
			for _, item := range event.ExecCommandProgress.Timeline {
				if strings.Contains(item.Summary, rawText) {
					t.Fatalf("suppressed dynamic tool text leaked into progress summary: %#v", event.ExecCommandProgress)
				}
			}
		}
	}
}
