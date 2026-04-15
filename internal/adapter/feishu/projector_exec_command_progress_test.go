package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectExecCommandProgressCreatesReplyCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Commands: []string{
				`/bin/bash -lc "npm test"`,
				`bash -lc 'go test ./...'`,
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "om-source-1" || !op.CardUpdateMulti {
		t.Fatalf("expected initial exec progress card reply, got %#v", op)
	}
	if op.CardTitle != "处理中" {
		t.Fatalf("expected generic processing title, got %#v", op)
	}
	if !strings.Contains(op.CardBody, "执行：") || !strings.Contains(op.CardBody, "npm test") || !strings.Contains(op.CardBody, "go test ./...") {
		t.Fatalf("expected activity-prefixed command list body, got %#v", op)
	}
	if strings.Contains(op.CardBody, "bash -lc") {
		t.Fatalf("expected command list body to strip shell wrapper, got %#v", op)
	}
	if strings.Contains(op.CardBody, "状态") || strings.Contains(op.CardBody, "目录") {
		t.Fatalf("expected command list body only, got %#v", op)
	}
}

func TestProjectExecCommandProgressUpdatesExistingCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "cmd-1",
			MessageID: "om-progress-1",
			Command:   "npm test",
			Status:    "completed",
			Final:     true,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationUpdateCard || op.MessageID != "om-progress-1" || op.ReplyToMessageID != "" {
		t.Fatalf("expected update operation for existing exec progress card, got %#v", op)
	}
	if op.CardThemeKey != cardThemeInfo {
		t.Fatalf("expected exec progress to use info theme, got %#v", op)
	}
}

func TestProjectExecCommandProgressRendersSharedWebSearchEntries(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "web-1",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: `bash -lc "go test ./..."`},
				{ItemID: "web-1", Kind: "web_search", Label: "搜索", Summary: "上海天气"},
				{ItemID: "web-2", Kind: "web_search", Label: "打开网页", Summary: "https://example.com/weather"},
				{ItemID: "mcp-1", Kind: "mcp_tool_call", Label: "MCP", Summary: "docs.lookup（12 ms）"},
				{ItemID: "dynamic_tool_call::read", Kind: "dynamic_tool_call", Label: "Read", Summary: "a.cpp b.cpp"},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "执行：") || !strings.Contains(body, "搜索：上海天气") || !strings.Contains(body, "打开网页：https://example.com/weather") || !strings.Contains(body, "MCP：docs.lookup（12 ms）") || !strings.Contains(body, "Read：a.cpp b.cpp") {
		t.Fatalf("expected shared command and web search rows, got %#v", ops[0])
	}
	if strings.Contains(body, `bash -lc`) {
		t.Fatalf("expected command row to still strip shell wrapper, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressTruncatesLongCommandSummary(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Commands: []string{
				`/bin/bash -lc "python scripts/really_long_task.py --workspace /tmp/demo --mode dry-run --verbose"`,
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "执行：") {
		t.Fatalf("expected activity prefix, got %#v", ops[0])
	}
	if !strings.Contains(body, "...") {
		t.Fatalf("expected truncated summary, got %#v", ops[0])
	}
	if strings.Contains(body, "--workspace /tmp/demo --mode dry-run --verbose") {
		t.Fatalf("expected long command tail to be truncated, got %#v", ops[0])
	}
}
