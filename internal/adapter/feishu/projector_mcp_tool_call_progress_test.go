package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectMCPToolCallProgressStartedCreatesReplyCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventMCPToolCallProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		MCPToolCallProgress: &control.MCPToolCallProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "mcp-1",
			Server:   "docs",
			Tool:     "lookup",
			Status:   "started",
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "om-source-1" {
		t.Fatalf("expected reply card operation, got %#v", op)
	}
	if op.CardTitle != "MCP 调用" || op.CardThemeKey != cardThemeInfo {
		t.Fatalf("unexpected mcp progress title/theme: %#v", op)
	}
	if !strings.Contains(op.CardBody, "开始：") || !strings.Contains(op.CardBody, "docs.lookup") {
		t.Fatalf("expected started mcp progress body, got %#v", op)
	}
}

func TestProjectMCPToolCallProgressCompletedAndFailedThemes(t *testing.T) {
	projector := NewProjector()

	completed := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventMCPToolCallProgress,
		MCPToolCallProgress: &control.MCPToolCallProgress{
			Server:     "docs",
			Tool:       "lookup",
			Status:     "completed",
			DurationMS: 12,
		},
	})
	if len(completed) != 1 || completed[0].CardThemeKey != cardThemeSuccess {
		t.Fatalf("expected completed mcp progress to use success theme, got %#v", completed)
	}
	if !strings.Contains(completed[0].CardBody, "完成：") || !strings.Contains(completed[0].CardBody, "耗时：12 ms") {
		t.Fatalf("expected completed mcp progress body, got %#v", completed[0])
	}

	failed := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventMCPToolCallProgress,
		MCPToolCallProgress: &control.MCPToolCallProgress{
			Server:       "docs",
			Tool:         "lookup",
			Status:       "failed",
			ErrorMessage: "connector timeout",
		},
	})
	if len(failed) != 1 || failed[0].CardThemeKey != cardThemeError {
		t.Fatalf("expected failed mcp progress to use error theme, got %#v", failed)
	}
	if !strings.Contains(failed[0].CardBody, "失败：") || !strings.Contains(failed[0].CardBody, "connector timeout") {
		t.Fatalf("expected failed mcp progress body, got %#v", failed[0])
	}
}
