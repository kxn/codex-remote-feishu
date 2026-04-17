package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectExecCommandProgressCreatesDirectCard(t *testing.T) {
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
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "" || !op.CardUpdateMulti {
		t.Fatalf("expected initial exec progress direct card, got %#v", op)
	}
	if op.CardTitle != "工作中" {
		t.Fatalf("expected generic processing title, got %#v", op)
	}
	if !strings.Contains(op.CardBody, "执行：`npm test`") || !strings.Contains(op.CardBody, "执行：`go test ./...`") {
		t.Fatalf("expected activity-prefixed command list body, got %#v", op)
	}
	if strings.Contains(op.CardBody, "bash -lc") {
		t.Fatalf("expected command list body to strip shell wrapper, got %#v", op)
	}
	payload := renderOperationCard(op, op.ordinaryCardEnvelope())
	body, _ := payload["body"].(map[string]any)
	elements, ok := cardPayloadElementsSlice(body["elements"])
	if !ok || len(elements) != 2 {
		t.Fatalf("expected one markdown element per command row, got %#v", payload)
	}
	if elements[0]["content"] != "执行：`npm test`" || elements[1]["content"] != "执行：`go test ./...`" {
		t.Fatalf("unexpected rendered command rows: %#v", elements)
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
	if op.CardThemeKey != cardThemeProgress {
		t.Fatalf("expected exec progress to use progress theme, got %#v", op)
	}
}

func TestProjectExecCommandProgressRendersTransientReasoningStatusAtBottom(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: "npm test"},
			},
			TransientStatus: &control.ExecCommandProgressTransientStatus{
				Kind: "reasoning",
				Text: "思考中",
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	entry := strings.Index(body, "执行：")
	status := strings.Index(body, "• 思考中")
	if entry == -1 || status == -1 || status <= entry {
		t.Fatalf("expected transient reasoning status at bottom, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressDoesNotRetractEmptyTransientCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "reasoning-1",
			MessageID: "om-progress-1",
		},
	})
	if len(ops) != 0 {
		t.Fatalf("expected empty transient clear to leave the old card in place, got %#v", ops)
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
				{ItemID: "compact-1", Kind: "context_compaction", Summary: "上下文已整理。"},
			},
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "completed",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read", Kind: "read", Items: []string{"a.cpp", "b.cpp"}},
				},
			}},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "• 已探索") || !strings.Contains(body, "  └ 读取 a.cpp、b.cpp") || !strings.Contains(body, "执行：`go test ./...`") || !strings.Contains(body, "搜索：上海天气") || !strings.Contains(body, "打开网页：https://example.com/weather") || !strings.Contains(body, "MCP：docs.lookup（12 ms）") || !strings.Contains(body, "整理：上下文已整理。") {
		t.Fatalf("expected shared command and web search rows, got %#v", ops[0])
	}
	if strings.Contains(body, `bash -lc`) {
		t.Fatalf("expected command row to still strip shell wrapper, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressInterleavesExplorationRowsAndEntriesByVisibleSeq(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-3",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read-1", Kind: "read", Items: []string{"foo.txt"}, LastSeq: 1},
					{RowID: "read-2", Kind: "read", Items: []string{"bar.txt"}, LastSeq: 3},
				},
			}},
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-2", Kind: "command_execution", Label: "执行", Summary: "npm test", LastSeq: 2},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Count(body, "• 探索中") != 2 {
		t.Fatalf("expected split exploration headers around entry barrier, got %#v", ops[0])
	}
	readFoo := strings.Index(body, "读取 foo.txt")
	entry := strings.Index(body, "执行：")
	readBar := strings.Index(body, "读取 bar.txt")
	if readFoo == -1 || entry == -1 || readBar == -1 || !(readFoo < entry && entry < readBar) {
		t.Fatalf("expected exploration rows and entries to follow visible seq order, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersEachLineAsSeparateMarkdownElement(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-2",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: `bash -lc "rg -n 'x' | sed -n '1,2p'"`, LastSeq: 1},
				{ItemID: "cmd-2", Kind: "command_execution", Label: "执行", Summary: `bash -lc "rg --files -g '*.css' -g '*.scss'"`, LastSeq: 2},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	payload := renderOperationCard(ops[0], ops[0].ordinaryCardEnvelope())
	body, _ := payload["body"].(map[string]any)
	elements, ok := cardPayloadElementsSlice(body["elements"])
	if !ok || len(elements) != 2 {
		t.Fatalf("expected one markdown element per progress line, got %#v", payload)
	}
	if elements[0]["content"] != "执行：`rg -n 'x' | sed -n '1,2p'`" {
		t.Fatalf("unexpected first progress line: %#v", elements[0])
	}
	second, _ := elements[1]["content"].(string)
	if !strings.HasPrefix(second, "执行：`rg --files -g '*.css' -g '") || !strings.HasSuffix(second, "...`") {
		t.Fatalf("expected truncated command to stay isolated in its own markdown element, got %#v", elements[1])
	}
	if strings.Contains(second, "<text_tag") {
		t.Fatalf("expected progress lines to avoid raw text_tag markup, got %#v", elements[1])
	}
}

func TestProjectExecCommandProgressRendersExplorationBlockStatuses(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "exploration",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read", Kind: "read", Items: []string{"docs/README.md", "internal/core/control/types.go"}},
					{RowID: "list::internal/core", Kind: "list", Summary: "internal/core"},
					{RowID: "search::compact::internal/", Kind: "search", Summary: "compact", Secondary: "internal/"},
				},
			}},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "• 探索中") || !strings.Contains(body, "  └ 读取 README.md、types.go") || !strings.Contains(body, "    列目录 internal/core") || !strings.Contains(body, "    搜索 compact（范围：internal/）") {
		t.Fatalf("expected exploration block rendering, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersExploredHeaderForFailedExploration(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "exploration",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "failed",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read::1", Kind: "read", Items: []string{"/dev/null"}},
				},
			}},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "• 已探索") || strings.Contains(body, "Exploration failed") || !strings.Contains(body, "读取 null") {
		t.Fatalf("expected upstream-style explored rendering for failed block, got %#v", ops[0])
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
