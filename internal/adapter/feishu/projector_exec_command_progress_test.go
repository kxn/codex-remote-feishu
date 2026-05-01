package feishu

import (
	"strconv"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func progressWithTimeline(progress control.ExecCommandProgress) *control.ExecCommandProgress {
	if len(progress.Timeline) == 0 {
		progress.Timeline = control.BuildExecCommandProgressTimeline(progress)
	}
	return &progress
}

func TestProjectExecCommandProgressCreatesDirectCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:    "thread-1",
			TurnID:      "turn-1",
			ItemID:      "cmd-1",
			DetourLabel: "临时会话 · 分支",
			Commands: []string{
				`/bin/bash -lc "npm test"`,
				`bash -lc 'go test ./...'`,
			},
		}),
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
	header := renderedV2CardHeader(t, op)
	if got := headerTextContent(header, "subtitle"); got != "**临时会话 · 分支**" {
		t.Fatalf("expected detour subtitle on progress card, got %#v", header)
	}
	expectedFirst := "**执行**：" + markdownCodeSpan("npm test")
	expectedSecond := "**执行**：" + markdownCodeSpan("go test ./...")
	if !strings.Contains(op.CardBody, expectedFirst) || !strings.Contains(op.CardBody, expectedSecond) {
		t.Fatalf("expected activity-prefixed command list body, got %#v", op)
	}
	if strings.Contains(op.CardBody, "bash -lc") {
		t.Fatalf("expected command list body to strip shell wrapper, got %#v", op)
	}
	payload := renderOperationCard(op, op.effectiveCardEnvelope())
	assertRenderedCardPayloadBasicInvariants(t, payload)
	body, _ := payload["body"].(map[string]any)
	elements, ok := cardPayloadElementsSlice(body["elements"])
	if !ok || len(elements) != 2 {
		t.Fatalf("expected one markdown element per command row, got %#v", payload)
	}
	if markdownContent(elements[0]) != expectedFirst || markdownContent(elements[1]) != expectedSecond {
		t.Fatalf("unexpected rendered command rows: %#v", elements)
	}
	if plainTextContent(elements[0]) != "" || plainTextContent(elements[1]) != "" {
		t.Fatalf("expected shared progress command rows to stop using plain_text blocks, got %#v", elements)
	}
	if strings.Contains(op.CardBody, "状态") || strings.Contains(op.CardBody, "目录") {
		t.Fatalf("expected command list body only, got %#v", op)
	}
}

func TestProjectExecCommandProgressUpdatesExistingCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "cmd-1",
			MessageID: "om-progress-1",
			Command:   "npm test",
			Status:    "completed",
			Final:     true,
		}),
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

func TestProjectExecCommandProgressRendersReasoningSummaryInsideTimeline(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: "npm test", LastSeq: 1},
				{ItemID: "reasoning-1", Kind: "reasoning_summary", Summary: "Thinking.", LastSeq: 2},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	entry := strings.Index(body, "**执行**：")
	reasoning := strings.Index(body, "Thinking.")
	if entry == -1 || reasoning == -1 || reasoning <= entry {
		t.Fatalf("expected reasoning summary to render as a timeline line after command entry, got %#v", ops[0])
	}
	if strings.Contains(body, "**工作中** Thinking.") || strings.Contains(body, "**思考**") {
		t.Fatalf("expected raw reasoning text without synthetic label, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressUsesCanonicalTimelineOnly(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "compact-1",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read-legacy", Kind: "read", Items: []string{"legacy.txt"}, LastSeq: 1},
				},
			}},
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-legacy", Kind: "command_execution", Label: "执行", Summary: "legacy command", LastSeq: 2},
			},
			Commands: []string{`bash -lc "legacy command"`},
			Timeline: []control.ExecCommandProgressTimelineItem{
				{ID: "read-1", Kind: "read", Items: []string{"foo.txt"}, LastSeq: 1},
				{ID: "compact-1", Kind: "context_compaction", Summary: "上下文已压缩。", LastSeq: 2},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**读取**："+markdownCodeSpan("foo.txt")) || !strings.Contains(body, "**压缩**：上下文已压缩。") {
		t.Fatalf("expected canonical timeline items to render, got %#v", ops[0])
	}
	if strings.Contains(body, "legacy.txt") || strings.Contains(body, "legacy command") {
		t.Fatalf("expected projector to ignore legacy timeline carriers once canonical timeline exists, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressDoesNotRenderFallbackCommandsAlongsideExplorationBlocks(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "exploration",
			Commands: []string{
				`bash -lc "cat foo.txt"`,
				`bash -lc "cat bar.txt"`,
			},
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read-1", Kind: "read", Items: []string{"foo.txt"}, LastSeq: 1},
					{RowID: "read-2", Kind: "read", Items: []string{"bar.txt"}, LastSeq: 2},
				},
			}},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**读取**："+markdownCodeSpan("foo.txt")) || !strings.Contains(body, "**读取**："+markdownCodeSpan("bar.txt")) {
		t.Fatalf("expected exploration rows to stay visible, got %#v", ops[0])
	}
	if strings.Contains(body, "**执行**："+markdownCodeSpan("cat foo.txt")) || strings.Contains(body, "**执行**："+markdownCodeSpan("cat bar.txt")) {
		t.Fatalf("expected fallback command rows to stay hidden when real exploration blocks exist, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressKeepsRealEntriesOnSameTimelineAsExplorationRows(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "compact-1",
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
				{ItemID: "compact-1", Kind: "context_compaction", Label: "压缩", Summary: "上下文已压缩。", LastSeq: 2},
			},
			Commands: []string{
				`bash -lc "cat foo.txt"`,
				`bash -lc "cat bar.txt"`,
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	readFoo := strings.Index(body, "**读取**："+markdownCodeSpan("foo.txt"))
	compact := strings.Index(body, "**压缩**：上下文已压缩。")
	readBar := strings.Index(body, "**读取**："+markdownCodeSpan("bar.txt"))
	if readFoo == -1 || compact == -1 || readBar == -1 || !(readFoo < compact && compact < readBar) {
		t.Fatalf("expected real entries and exploration rows to share one seq timeline, got %#v", ops[0])
	}
	if strings.Contains(body, "**执行**："+markdownCodeSpan("cat foo.txt")) || strings.Contains(body, "**执行**："+markdownCodeSpan("cat bar.txt")) {
		t.Fatalf("expected command fallback rows to stay hidden when real timeline items already exist, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressDoesNotRetractEmptyTransientCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "reasoning-1",
			MessageID: "om-progress-1",
		}),
	})
	if len(ops) != 0 {
		t.Fatalf("expected empty transient clear to leave the old card in place, got %#v", ops)
	}
}

func TestProjectExecCommandProgressRendersSharedWebSearchEntries(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "web-1",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: `bash -lc "go test ./..."`},
				{ItemID: "web-1", Kind: "web_search", Label: "搜索", Summary: "上海天气"},
				{ItemID: "web-2", Kind: "web_search", Label: "打开网页", Summary: "https://example.com/weather"},
				{ItemID: "mcp-1", Kind: "mcp_tool_call", Label: "MCP", Summary: "docs.lookup（12 ms）"},
				{ItemID: "compact-1", Kind: "context_compaction", Summary: "上下文已压缩。"},
			},
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "completed",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read", Kind: "read", Items: []string{"a.cpp", "b.cpp"}},
				},
			}},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "探索中") || strings.Contains(body, "已探索") ||
		!strings.Contains(body, "**读取**："+markdownCodeSpan("a.cpp")+"、"+markdownCodeSpan("b.cpp")) ||
		!strings.Contains(body, "**执行**："+markdownCodeSpan("go test ./...")) ||
		!strings.Contains(body, "**搜索**："+markdownCodeSpan("上海天气")) ||
		!strings.Contains(body, "**打开网页**："+markdownCodeSpan("https://example.com/weather")) ||
		!strings.Contains(body, "**MCP**："+markdownCodeSpan("docs.lookup（12 ms）")) ||
		!strings.Contains(body, "**压缩**：上下文已压缩。") {
		t.Fatalf("expected shared command and web search rows, got %#v", ops[0])
	}
	if strings.Contains(body, `bash -lc`) {
		t.Fatalf("expected command row to still strip shell wrapper, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressKeepsWebSearchStatusPlainText(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "web-1",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "web-1", Kind: "web_search", Label: "搜索", Summary: "正在搜索网络"},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**搜索**：正在搜索网络") || strings.Contains(body, markdownCodeSpan("正在搜索网络")) {
		t.Fatalf("expected web search status to stay plain text, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersProcessPlanAndDelegatedTask(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "task-1",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "process_plan",
				Kind:    "process_plan",
				Status:  "completed",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "summary", Kind: "process_plan_summary", Summary: "Gathering evidence", LastSeq: 1},
					{RowID: "step-1", Kind: "process_plan_step", Summary: "Gather evidence", Secondary: "in_progress", LastSeq: 2},
					{RowID: "step-2", Kind: "process_plan_step", Summary: "Write summary", Secondary: "pending", LastSeq: 3},
				},
			}},
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "task-1", Kind: "delegated_task", Label: "Task", Summary: "Explore · Audit the repository", LastSeq: 4},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**计划**：Gathering evidence") ||
		!strings.Contains(body, "**计划进行中**：Gather evidence") ||
		!strings.Contains(body, "**计划待办**：Write summary") ||
		!strings.Contains(body, "**Task**：Explore · Audit the repository") {
		t.Fatalf("expected process plan and delegated task rendering, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersFileChangeSummaryInNormal(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "file-1",
			Verbosity:    "normal",
			MessageID:    "om-progress-1",
			CardStartSeq: 1,
			Entries: []control.ExecCommandProgressEntry{{
				ItemID:  "file-1::service.go",
				Kind:    "file_change",
				Label:   "修改",
				Summary: "service.go",
				FileChange: &control.ExecCommandProgressFileChange{
					Path:         "service.go",
					Kind:         "update",
					Diff:         "@@ -1 +1 @@\n-old\n+new",
					AddedLines:   1,
					RemovedLines: 1,
				},
				LastSeq: 1,
			}},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**修改**："+markdownCodeSpan("service.go")+"  "+formatFileChangeCountsMarkdown(1, 1)) {
		t.Fatalf("expected normal file_change summary row, got %#v", ops[0])
	}
	if strings.Contains(body, "```diff") || strings.Contains(body, "@@ -1 +1 @@") {
		t.Fatalf("expected normal verbosity not to inline diff block, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersFileChangeDiffInVerbose(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "file-1",
			Verbosity: "verbose",
			Entries: []control.ExecCommandProgressEntry{{
				ItemID:  "file-1::guide",
				Kind:    "file_change",
				Label:   "修改",
				Summary: "docs/guide.md -> docs/guide-v2.md",
				FileChange: &control.ExecCommandProgressFileChange{
					Path:         "docs/guide.md",
					MovePath:     "docs/guide-v2.md",
					Kind:         "update",
					Diff:         "@@ -1 +1 @@\n-old title\n+new title",
					AddedLines:   1,
					RemovedLines: 1,
				},
				LastSeq: 1,
			}},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**修改**："+markdownCodeSpan("guide.md")+" -> "+markdownCodeSpan("guide-v2.md")+"  "+formatFileChangeCountsMarkdown(1, 1)) {
		t.Fatalf("expected verbose file_change summary row, got %#v", ops[0])
	}
	if !strings.Contains(body, markdownFencedCodeBlock("diff", "@@ -1 +1 @@\n-old title\n+new title")) {
		t.Fatalf("expected verbose file_change to append fenced diff block, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressInterleavesExplorationRowsAndEntriesByVisibleSeq(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
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
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "探索中") || strings.Contains(body, "已探索") {
		t.Fatalf("expected exploration rows to render without headers, got %#v", ops[0])
	}
	readFoo := strings.Index(body, "**读取**："+markdownCodeSpan("foo.txt"))
	entry := strings.Index(body, "**执行**："+markdownCodeSpan("npm test"))
	readBar := strings.Index(body, "**读取**："+markdownCodeSpan("bar.txt"))
	if readFoo == -1 || entry == -1 || readBar == -1 || !(readFoo < entry && entry < readBar) {
		t.Fatalf("expected exploration rows and entries to follow visible seq order, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersEachLineAsSeparatePlainTextElement(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-2",
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: `bash -lc "rg -n 'x' | sed -n '1,2p'"`, LastSeq: 1},
				{ItemID: "cmd-2", Kind: "command_execution", Label: "执行", Summary: `bash -lc "rg --files -g '*.css' -g '*.scss'"`, LastSeq: 2},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	payload := renderOperationCard(ops[0], ops[0].effectiveCardEnvelope())
	assertRenderedCardPayloadBasicInvariants(t, payload)
	body, _ := payload["body"].(map[string]any)
	elements, ok := cardPayloadElementsSlice(body["elements"])
	if !ok || len(elements) != 2 {
		t.Fatalf("expected one markdown element per progress line, got %#v", payload)
	}
	if markdownContent(elements[0]) != "**执行**："+markdownCodeSpan("rg -n 'x' | sed -n '1,2p'") {
		t.Fatalf("unexpected first progress line: %#v", elements[0])
	}
	second := markdownContent(elements[1])
	wantSecond := "**执行**：" + markdownCodeSpan(truncateExecProgressSummary("rg --files -g '*.css' -g '*.scss'", 30))
	if second != wantSecond {
		t.Fatalf("expected truncated command to stay isolated in its own markdown element, got %#v", elements[1])
	}
	if plainTextContent(elements[0]) != "" || plainTextContent(elements[1]) != "" || strings.Contains(second, "<text_tag") {
		t.Fatalf("expected progress lines to use markdown elements without text_tag fallback, got %#v", elements[1])
	}
}

func TestProjectExecCommandProgressKeepsDynamicTextOutOfMarkdownElements(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "mcp-1",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "completed",
				Rows: []control.ExecCommandProgressBlockRow{
					{RowID: "read-1", Kind: "read", Items: []string{"docs/[draft].md"}, LastSeq: 1},
					{RowID: "search-1", Kind: "search", Summary: "`compact` [todo]", Secondary: "internal/[core]", LastSeq: 2},
				},
			}},
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: "echo \"[link](demo)\" `code`", LastSeq: 3},
				{ItemID: "mcp-1", Kind: "mcp_tool_call", Label: "MCP", Summary: "docs.lookup [spec](demo) `fast`", LastSeq: 4},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !containsAll(body,
		"**读取**："+markdownCodeSpan("[draft].md"),
		"**搜索**："+markdownCodeSpan("`compact` [todo]")+"（在 "+markdownCodeSpan("internal/[core]")+" 内）",
		"**执行**："+markdownCodeSpan("echo \"[link](demo)\" `code`"),
		"**MCP**："+markdownCodeSpan("docs.lookup [spec](demo) `fast`"),
	) {
		t.Fatalf("expected shared progress body to preserve raw dynamic text, got %#v", ops[0])
	}
	payload := renderOperationCard(ops[0], ops[0].effectiveCardEnvelope())
	assertRenderedCardPayloadBasicInvariants(t, payload)
	rendered := renderedV2BodyElements(t, ops[0])
	if !containsRenderedTag(rendered, "markdown") {
		t.Fatalf("expected shared progress renderer to use markdown elements for formatted rows, got %#v", rendered)
	}
	for _, element := range rendered {
		text := markdownContent(element)
		if text == "" {
			t.Fatalf("expected only markdown progress rows, got %#v", element)
		}
		if plainTextContent(element) != "" || strings.Contains(text, "<text_tag") || strings.Contains(text, "<font") {
			t.Fatalf("expected dynamic text rows to avoid plain_text fallback and unrelated font/text_tag markup, got %#v", element)
		}
	}
}

func TestProjectExecCommandProgressDropsOldLinesWhenOversized(t *testing.T) {
	projector := NewProjector()
	entries := make([]control.ExecCommandProgressEntry, 0, 480)
	for i := 1; i <= 480; i++ {
		entries = append(entries, control.ExecCommandProgressEntry{
			ItemID:  "cmd-" + strconv.Itoa(i),
			Kind:    "command_execution",
			Label:   "执行",
			Summary: "go test ./pkg/" + strconv.Itoa(i),
			LastSeq: i,
		})
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "cmd-480",
			MessageID:    "om-progress-1",
			CardStartSeq: 1,
			Entries:      entries,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected oversized shared progress to stay on one card, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationUpdateCard || op.MessageID != "om-progress-1" {
		t.Fatalf("expected oversized shared progress to keep patching current card, got %#v", op)
	}
	if op.ProgressCardStartSeq <= 1 {
		t.Fatalf("expected oversized shared progress to advance visible window start, got %#v", op)
	}
	payload := renderOperationCard(op, op.effectiveCardEnvelope())
	assertRenderedCardPayloadBasicInvariants(t, payload)
	size, err := feishuInteractiveMessageTransportSize(payload)
	if err != nil {
		t.Fatalf("measure shared progress transport payload: %v", err)
	}
	if size > feishuCardTransportLimitBytes {
		t.Fatalf("expected shared progress transport <= %d bytes, got %d", feishuCardTransportLimitBytes, size)
	}
	if strings.Contains(op.CardBody, oversizedCardMessage) {
		t.Fatalf("expected projector FIFO trimming to avoid gateway truncation marker, got %#v", op)
	}
	if !strings.Contains(op.CardBody, execProgressOmittedHistoryText) {
		t.Fatalf("expected oversized shared progress to explain omitted history, got %#v", op)
	}
	if strings.Contains(op.CardBody, "**执行**："+markdownCodeSpan("go test ./pkg/1")) || strings.Contains(op.CardBody, "**执行**："+markdownCodeSpan("go test ./pkg/2")) {
		t.Fatalf("expected oldest progress lines to fall out of active window, got %#v", op)
	}
	if !strings.Contains(op.CardBody, markdownCodeSpan("go test ./pkg/480")) {
		t.Fatalf("expected newest progress lines to stay visible, got %#v", op)
	}
}

func TestProjectExecCommandProgressUsesStoredWindowStartSeq(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "cmd-4",
			MessageID:    "om-progress-2",
			CardStartSeq: 3,
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: "go test ./one", LastSeq: 1},
				{ItemID: "cmd-2", Kind: "command_execution", Label: "执行", Summary: "go test ./two", LastSeq: 2},
				{ItemID: "cmd-3", Kind: "command_execution", Label: "执行", Summary: "go test ./three", LastSeq: 3},
				{ItemID: "cmd-4", Kind: "command_execution", Label: "执行", Summary: "go test ./four", LastSeq: 4},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected stored window to stay on one card, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "./one") || strings.Contains(body, "./two") {
		t.Fatalf("expected old lines before current stored window to stay out of active card, got %#v", ops[0])
	}
	if !strings.Contains(body, "./three") || !strings.Contains(body, "./four") {
		t.Fatalf("expected active stored window lines to stay visible, got %#v", ops[0])
	}
	if !strings.Contains(body, execProgressOmittedHistoryText) {
		t.Fatalf("expected stored window to show omitted-history marker, got %#v", ops[0])
	}
	if ops[0].ProgressCardStartSeq != 3 {
		t.Fatalf("expected stored window to preserve start seq, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressFallsBackWhenStoredWindowIsStale(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "cmd-2",
			MessageID:    "om-progress-3",
			CardStartSeq: 99,
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "cmd-1", Kind: "command_execution", Label: "执行", Summary: "go test ./one", LastSeq: 1},
				{ItemID: "cmd-2", Kind: "command_execution", Label: "执行", Summary: "go test ./two", LastSeq: 2},
			},
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected stale stored window to fall back to visible lines, got %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "./one") || !strings.Contains(ops[0].CardBody, "./two") {
		t.Fatalf("expected stale stored window to fall back to earliest visible lines, got %#v", ops[0])
	}
	if strings.Contains(ops[0].CardBody, execProgressOmittedHistoryText) {
		t.Fatalf("did not expect stale stored window fallback to invent omitted-history marker, got %#v", ops[0])
	}
	if ops[0].ProgressCardStartSeq != 1 {
		t.Fatalf("expected stale stored window fallback to reset start seq, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressKeepsLongReasoningSummaryUntilCardBudget(t *testing.T) {
	projector := NewProjector()
	summary := strings.Repeat("Thinking about command sequencing ", 10)
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "reasoning-1",
			MessageID:    "om-progress-1",
			CardStartSeq: 1,
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "reasoning-1", Kind: "reasoning_summary", Summary: summary, LastSeq: 1},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected long reasoning summary to stay on current card, got %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, strings.TrimSpace(summary)) || strings.Contains(ops[0].CardBody, "...") {
		t.Fatalf("expected reasoning summary to avoid dedicated short truncation, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressTruncatesSingleReasoningLineOnlyAtCardBudget(t *testing.T) {
	projector := NewProjector()
	summary := strings.Repeat("Thinking about command sequencing ", 5000)
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID:     "thread-1",
			TurnID:       "turn-1",
			ItemID:       "reasoning-1",
			MessageID:    "om-progress-1",
			CardStartSeq: 1,
			Entries: []control.ExecCommandProgressEntry{
				{ItemID: "reasoning-1", Kind: "reasoning_summary", Summary: summary, LastSeq: 1},
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected oversized single reasoning row to degrade to a sendable current card, got %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "Thinking about command sequencing") || !strings.Contains(ops[0].CardBody, "...") {
		t.Fatalf("expected card-budget fallback to truncate the single oversized row, got %#v", ops[0])
	}
	payload := renderOperationCard(ops[0], ops[0].effectiveCardEnvelope())
	size, err := feishuInteractiveMessageTransportSize(payload)
	if err != nil {
		t.Fatalf("measure shared progress transport payload: %v", err)
	}
	if size > feishuCardTransportLimitBytes {
		t.Fatalf("expected fallback shared progress transport <= %d bytes, got %d", feishuCardTransportLimitBytes, size)
	}
}

func TestProjectExecCommandProgressRendersExplorationBlockStatuses(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
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
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "探索中") || strings.Contains(body, "已探索") ||
		!strings.Contains(body, "**读取**："+markdownCodeSpan("README.md")+"、"+markdownCodeSpan("types.go")) ||
		!strings.Contains(body, "**列目录**："+markdownCodeSpan("internal/core")) ||
		!strings.Contains(body, "**搜索**："+markdownCodeSpan("compact")+"（在 "+markdownCodeSpan("internal/")+" 内）") {
		t.Fatalf("expected exploration block rendering, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressRendersExploredHeaderForFailedExploration(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
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
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "探索中") || strings.Contains(body, "已探索") || strings.Contains(body, "Exploration failed") || !strings.Contains(body, "**读取**："+markdownCodeSpan("null")) {
		t.Fatalf("expected upstream-style explored rendering for failed block, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressKeepsMergedReadFilenamesVisible(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "read-1",
			Blocks: []control.ExecCommandProgressBlock{{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "completed",
				Rows: []control.ExecCommandProgressBlockRow{{
					RowID: "read-1",
					Kind:  "read",
					Items: []string{
						"/tmp/alpha-really-long-file-name.md",
						"/tmp/beta-really-long-file-name.md",
						"/tmp/gamma-really-long-file-name.md",
					},
				}},
			}},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if strings.Contains(body, "...") || !strings.Contains(body, "alpha-really-long-file-name.md") || !strings.Contains(body, "beta-really-long-file-name.md") || !strings.Contains(body, "gamma-really-long-file-name.md") {
		t.Fatalf("expected merged read filenames to stay fully visible, got %#v", ops[0])
	}
}

func TestProjectExecCommandProgressTruncatesLongCommandSummary(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.KindExecCommandProgress,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ExecCommandProgress: progressWithTimeline(control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			Commands: []string{
				`/bin/bash -lc "python scripts/really_long_task.py --workspace /tmp/demo --mode dry-run --verbose"`,
			},
		}),
	})
	if len(ops) != 1 {
		t.Fatalf("expected one operation, got %#v", ops)
	}
	body := ops[0].CardBody
	if !strings.Contains(body, "**执行**：") {
		t.Fatalf("expected activity prefix, got %#v", ops[0])
	}
	if !strings.Contains(body, "...") {
		t.Fatalf("expected truncated summary, got %#v", ops[0])
	}
	if strings.Contains(body, "--workspace /tmp/demo --mode dry-run --verbose") {
		t.Fatalf("expected long command tail to be truncated, got %#v", ops[0])
	}
}
