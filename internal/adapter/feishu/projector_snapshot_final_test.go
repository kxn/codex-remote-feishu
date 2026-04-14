package feishu

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func intPtr(value int) *int {
	return &value
}

func TestProjectTurnFailedNoticeUsesErrorTheme(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code:  "turn_failed",
			Title: "链路错误 · codex.runtime_error",
			Text:  "摘要：stream disconnected before completion",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardThemeKey != cardThemeError {
		t.Fatalf("expected turn failure notice to project as error card, got %#v", ops[0])
	}
}

func TestProjectSnapshotShowsFollowWaitingAndAbandoning(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				DisplayName: "droid",
				RouteMode:   "follow_local",
				Abandoning:  true,
			},
			NextPrompt: control.PromptRouteSummary{
				RouteMode:                      "follow_local",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "high",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
				BaseModelSource:                "unknown",
				BaseReasoningEffortSource:      "unknown",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody, "正在断开，等待当前 turn 收尾", "跟随当前 VS Code（等待中）") {
		t.Fatalf("expected snapshot body to show follow waiting and abandoning, got %#v", ops[0].CardBody)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected snapshot to use structured V2 send path, got %#v", ops[0])
	}
}

func TestProjectSnapshotShowsNewThreadReadyTarget(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				DisplayName: "droid",
				RouteMode:   "new_thread_ready",
			},
			NextPrompt: control.PromptRouteSummary{
				RouteMode:                      "new_thread_ready",
				CWD:                            "/data/dl/droid",
				CreateThread:                   true,
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "xhigh",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody, "新建会话（等待首条消息）", "**当前目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>") {
		t.Fatalf("expected snapshot body to show new-thread-ready target, got %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "**目标：**") {
		t.Fatalf("snapshot body should not repeat target section, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotShowsGateAndRetainedOfflineAttachment(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:          "inst-1",
				DisplayName:         "droid",
				SelectedThreadID:    "thread-1",
				SelectedThreadTitle: "droid · 修复登录流程",
				RouteMode:           "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 修复登录流程",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "high",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
			Gate: control.GateSummary{
				Kind:                "pending_request",
				PendingRequestCount: 2,
			},
			Dispatch: control.DispatchSummary{
				InstanceOnline: false,
				QueuedCount:    2,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**执行状态：** 实例离线，已保留接管关系；2 条排队消息会在恢复后继续",
		"**输入门禁：** 有 2 个待处理请求；普通文本和图片会先被拦住",
	) {
		t.Fatalf("expected snapshot body to show gate and retained offline attachment, got %#v", ops[0].CardBody)
	}
}

func TestProjectFinalAssistantBlockAsThreadCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:                 control.UIEventBlockCommitted,
		SourceMessageID:      "msg-1",
		SourceMessagePreview: "请帮我处理这个问题",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已收到：\n\n```text\nREADME.md\nsrc\n```",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复：请帮我处理这个问题" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].ReplyToMessageID != "msg-1" {
		t.Fatalf("unexpected reply target: %#v", ops[0])
	}
	if ops[0].CardThemeKey != cardThemeFinal {
		t.Fatalf("unexpected theme key: %#v", ops[0])
	}
	if ops[0].CardBody != "已收到：\n\n```text\nREADME.md\nsrc\n```" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected final block to use structured V2 send path, got %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockPreservesInlineMarkdown(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-inline",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已处理 `#47`，当前 verdict 是 `old`，可发送 `/use` 重试。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	want := "已处理 `#47`，当前 verdict 是 `old`，可发送 `/use` 重试。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected final markdown body: %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockKeepsMixedInlineAndFencedMarkdown(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-mixed",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已处理 `#47`。\n\n```text\n`old`\n/use\n```\n\n外面还有 `done`。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	want := "已处理 `#47`。\n\n```text\n`old`\n/use\n```\n\n外面还有 `done`。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected mixed final markdown body: %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockEmbedsFileChangeSummary(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-2",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已完成修改。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
		FileChangeSummary: &control.FileChangeSummary{
			ThreadID:     "thread-1",
			ThreadTitle:  "droid · 修复登录流程",
			FileCount:    3,
			AddedLines:   8,
			RemovedLines: 3,
			Files: []control.FileChangeSummaryEntry{
				{Path: "internal/core/orchestrator/service.go", AddedLines: 3, RemovedLines: 1},
				{Path: "internal/adapter/feishu/service.go", AddedLines: 2, RemovedLines: 1},
				{Path: "docs/old/guide.md", MovePath: "docs/new/guide.md", AddedLines: 3, RemovedLines: 1},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].ReplyToMessageID != "msg-2" {
		t.Fatalf("unexpected reply target: %#v", ops[0])
	}
	if ops[0].CardThemeKey != cardThemeFinal {
		t.Fatalf("unexpected theme key: %#v", ops[0])
	}
	if ops[0].CardBody != "已完成修改。" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected summary header plus three file rows, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**本次修改** 3 个文件  <font color='green'>+8</font> <font color='red'>-3</font>" {
		t.Fatalf("unexpected summary header: %#v", ops[0].CardElements[0])
	}
	if ops[0].CardElements[1]["content"] != "1. <text_tag color='neutral'>orchestrator/service.go</text_tag>  <font color='green'>+3</font> <font color='red'>-1</font>" {
		t.Fatalf("unexpected unique-suffix element: %#v", ops[0].CardElements[1])
	}
	if ops[0].CardElements[2]["content"] != "2. <text_tag color='neutral'>feishu/service.go</text_tag>  <font color='green'>+2</font> <font color='red'>-1</font>" {
		t.Fatalf("unexpected second unique-suffix element: %#v", ops[0].CardElements[2])
	}
	if ops[0].CardElements[3]["content"] != "3. <text_tag color='neutral'>old/guide.md</text_tag> → <text_tag color='neutral'>new/guide.md</text_tag>  <font color='green'>+3</font> <font color='red'>-1</font>" {
		t.Fatalf("unexpected rename summary element: %#v", ops[0].CardElements[3])
	}
}

func TestProjectFinalAssistantBlockAppendsElapsedAfterFileChangeSummary(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-2",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已完成修改。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
		FileChangeSummary: &control.FileChangeSummary{
			ThreadID:     "thread-1",
			ThreadTitle:  "droid · 修复登录流程",
			FileCount:    2,
			AddedLines:   5,
			RemovedLines: 1,
			Files: []control.FileChangeSummaryEntry{
				{Path: "internal/core/orchestrator/service.go", AddedLines: 3, RemovedLines: 1},
				{Path: "internal/adapter/feishu/service.go", AddedLines: 2},
			},
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed: 3400 * time.Millisecond,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected file summary rows plus elapsed footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[3]["content"] != "**本轮用时** 3秒" {
		t.Fatalf("unexpected elapsed footer: %#v", ops[0].CardElements[3])
	}
}

func TestProjectFinalAssistantBlockShowsElapsedWithoutFileSummary(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-2",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "已完成。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed: 2100 * time.Millisecond,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected standalone elapsed footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**本轮用时** 2秒" {
		t.Fatalf("unexpected standalone elapsed footer: %#v", ops[0].CardElements[0])
	}
}

func TestProjectFinalAssistantBlockShowsTurnUsageFooter(t *testing.T) {
	projector := NewProjector()
	contextWindow := 1000
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-usage",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed:            2100 * time.Millisecond,
			ContextInputTokens: intPtr(150),
			ModelContextWindow: &contextWindow,
			Usage: &control.FinalTurnUsage{
				InputTokens:           150,
				CachedInputTokens:     90,
				OutputTokens:          50,
				ReasoningOutputTokens: 20,
				TotalTokens:           200,
			},
			ThreadUsage: &control.FinalTurnUsage{
				InputTokens:           400,
				CachedInputTokens:     200,
				OutputTokens:          100,
				ReasoningOutputTokens: 40,
				TotalTokens:           500,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected standalone usage footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**本轮用时** 2秒  **本轮累计** 输入 150  缓存 90 (60.0%)  输出 50  推理 20  **线程累计** 输入 400  缓存 200 (50.0%)  输出 100  推理 40  **上下文剩余(估算)** 85.0%" {
		t.Fatalf("unexpected usage footer: %#v", ops[0].CardElements[0])
	}
}

func TestProjectFinalAssistantBlockCompactsThreadUsageFooter(t *testing.T) {
	projector := NewProjector()
	contextWindow := 1000000000
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-usage-compact",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed:            2100 * time.Millisecond,
			ContextInputTokens: intPtr(466989),
			ModelContextWindow: &contextWindow,
			Usage: &control.FinalTurnUsage{
				InputTokens:           466989,
				CachedInputTokens:     395648,
				OutputTokens:          1803,
				ReasoningOutputTokens: 761,
			},
			ThreadUsage: &control.FinalTurnUsage{
				InputTokens:           250741509,
				CachedInputTokens:     233287808,
				OutputTokens:          912728,
				ReasoningOutputTokens: 415546,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected standalone usage footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**本轮用时** 2秒  **本轮累计** 输入 466989  缓存 395648 (84.7%)  输出 1803  推理 761  **线程累计** 输入 250.7M  缓存 233.3M (93.0%)  输出 912.7K  推理 415.5K  **上下文剩余(估算)** 100.0%" {
		t.Fatalf("unexpected compact usage footer: %#v", ops[0].CardElements[0])
	}
}

func TestProjectFinalAssistantBlockShowsZeroInputWithoutCacheRatio(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-zero-usage",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed: 2100 * time.Millisecond,
			Usage: &control.FinalTurnUsage{
				InputTokens:           0,
				CachedInputTokens:     0,
				OutputTokens:          12,
				ReasoningOutputTokens: 3,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardElements[0]["content"] != "**本轮用时** 2秒  **本轮累计** 输入 0  缓存 0  输出 12  推理 3" {
		t.Fatalf("unexpected zero-input usage footer: %#v", ops[0].CardElements[0])
	}
}

func TestProjectFinalAssistantBlockAppendsCleanWorktreeSummary(t *testing.T) {
	projector := NewProjector()
	projector.readGitWorktree = func(cwd string) *gitWorktreeSummary {
		if cwd != "/data/dl/droid" {
			t.Fatalf("unexpected cwd: %q", cwd)
		}
		return &gitWorktreeSummary{}
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-3",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed:   2100 * time.Millisecond,
			ThreadCWD: "/data/dl/droid",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected elapsed footer plus worktree footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[1]["content"] != "**工作区** <text_tag color='neutral'>干净</text_tag>" {
		t.Fatalf("unexpected clean worktree footer: %#v", ops[0].CardElements[1])
	}
}

func TestProjectFinalAssistantBlockAppendsDirtyWorktreeSummary(t *testing.T) {
	projector := NewProjector()
	projector.readGitWorktree = func(string) *gitWorktreeSummary {
		return &gitWorktreeSummary{
			Dirty:          true,
			ModifiedCount:  3,
			UntrackedCount: 1,
			Files: []string{
				"internal/core/orchestrator/service.go",
				"internal/adapter/feishu/service.go",
				"README.md",
				"docs/general/remote-surface-state-machine.md",
			},
		}
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-4",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed:   2100 * time.Millisecond,
			ThreadCWD: "/data/dl/droid",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected elapsed footer plus worktree footer, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[1]["content"] != "**工作区** <text_tag color='neutral'>有改动</text_tag> <text_tag color='neutral'>3修改</text_tag> <text_tag color='neutral'>1未跟踪</text_tag> <text_tag color='neutral'>orchestrator/service.go</text_tag> <text_tag color='neutral'>feishu/service.go</text_tag> <text_tag color='neutral'>README.md</text_tag>" {
		t.Fatalf("unexpected dirty worktree footer: %#v", ops[0].CardElements[1])
	}
}

func TestProjectFinalAssistantBlockSkipsWorktreeSummaryOutsideGitRepo(t *testing.T) {
	projector := NewProjector()
	projector.readGitWorktree = func(string) *gitWorktreeSummary { return nil }
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-5",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已完成。",
			Final: true,
		},
		FinalTurnSummary: &control.FinalTurnSummary{
			Elapsed:   2100 * time.Millisecond,
			ThreadCWD: "/tmp/not-a-repo",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 1 {
		t.Fatalf("expected only elapsed footer outside git repo, got %#v", ops[0].CardElements)
	}
}

func TestParseGitStatusPaths(t *testing.T) {
	got := parseGitStatusPaths(strings.Join([]string{
		" M internal/core/orchestrator/service.go",
		"R  docs/old/guide.md -> docs/new/guide.md",
		"?? \"docs/my file.md\"",
		"?? internal/core/orchestrator/service.go",
	}, "\n"))
	want := []string{
		"internal/core/orchestrator/service.go",
		"docs/new/guide.md",
		"docs/my file.md",
	}
	if len(got) != len(want) {
		t.Fatalf("parseGitStatusPaths() len = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseGitStatusPaths()[%d] = %q, want %q (%#v)", i, got[i], want[i], got)
		}
	}
}

func TestParseGitWorktreeSummary(t *testing.T) {
	got := parseGitWorktreeSummary(strings.Join([]string{
		" M internal/core/orchestrator/service.go",
		"A  docs/new/guide.md",
		"R  docs/old/guide.md -> docs/new/renamed.md",
		"?? \"docs/my file.md\"",
		"?? internal/core/orchestrator/service.go",
	}, "\n"))
	if got == nil {
		t.Fatal("expected git worktree summary")
	}
	if !got.Dirty || got.ModifiedCount != 3 || got.UntrackedCount != 2 {
		t.Fatalf("unexpected summary counts: %#v", got)
	}
	wantFiles := []string{
		"internal/core/orchestrator/service.go",
		"docs/new/guide.md",
		"docs/new/renamed.md",
		"docs/my file.md",
	}
	if len(got.Files) != len(wantFiles) {
		t.Fatalf("summary files len = %d, want %d (%#v)", len(got.Files), len(wantFiles), got.Files)
	}
	for i := range wantFiles {
		if got.Files[i] != wantFiles[i] {
			t.Fatalf("summary files[%d] = %q, want %q (%#v)", i, got.Files[i], wantFiles[i], got.Files)
		}
	}
}

func TestFormatElapsedDurationUsesHumanReadableUnits(t *testing.T) {
	tests := []struct {
		name  string
		value time.Duration
		want  string
	}{
		{name: "sub second", value: 400 * time.Millisecond, want: "<1秒"},
		{name: "seconds only", value: 3400 * time.Millisecond, want: "3秒"},
		{name: "minutes and seconds", value: 65*time.Second + 400*time.Millisecond, want: "1分钟5秒"},
		{name: "hours minutes seconds", value: time.Hour + 2*time.Minute + 3*time.Second, want: "1小时2分钟3秒"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatElapsedDuration(tt.value); got != tt.want {
				t.Fatalf("formatElapsedDuration(%s) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestProjectFinalAssistantBlockTruncatesChineseTitlePreview(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:                 control.UIEventBlockCommitted,
		SourceMessageID:      "msg-3",
		SourceMessagePreview: "一二三四五六七八九十十一十二",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已处理。",
			Final: true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复：一二三四五六七八九十..." {
		t.Fatalf("unexpected chinese preview title: %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockTruncatesEnglishTitlePreview(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:                 control.UIEventBlockCommitted,
		SourceMessageID:      "msg-4",
		SourceMessagePreview: "please help me align the return format for this API response payload",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "已处理。",
			Final: true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "✅ 最后答复：please help me align the return format for this API..." {
		t.Fatalf("unexpected english preview title: %#v", ops[0])
	}
}

func TestProjectProcessAssistantBlockAsPlainText(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventBlockCommitted,
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "我先看一下目录结构。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       false,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendText {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].Text != "我先看一下目录结构。" {
		t.Fatalf("unexpected text body: %#v", ops[0])
	}
}

func TestProjectSnapshotIncludesEffectivePromptConfig(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode: "vscode",
			Attachment: control.AttachmentSummary{
				InstanceID:          "inst-1",
				ObjectType:          "vscode_instance",
				DisplayName:         "droid",
				SelectedThreadID:    "thread-1",
				SelectedThreadTitle: "droid · 修复登录流程",
				RouteMode:           "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 修复登录流程",
				CWD:                            "/data/dl/droid",
				BaseModel:                      "gpt-5.3-codex",
				BaseReasoningEffort:            "medium",
				BaseModelSource:                "thread",
				BaseReasoningEffortSource:      "thread",
				OverrideModel:                  "gpt-5.4",
				OverrideAccessMode:             "confirm",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "medium",
				EffectiveAccessMode:            "confirm",
				EffectiveModelSource:           "surface_override",
				EffectiveReasoningEffortSource: "thread",
				EffectiveAccessModeSource:      "surface_override",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前模式：** <text_tag color='neutral'>vscode</text_tag>",
		"**当前目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
		"**接管对象类型：** <text_tag color='neutral'>VS Code 实例</text_tag>",
		"**下条飞书消息：** 模型 <text_tag color='neutral'>gpt-5.4</text_tag>，推理 <text_tag color='neutral'>medium</text_tag>，权限 <text_tag color='neutral'>confirm</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0])
	}
	if strings.Contains(ops[0].CardBody, "已知会话：") ||
		strings.Contains(ops[0].CardBody, "在线实例：") ||
		strings.Contains(ops[0].CardBody, "飞书临时覆盖") ||
		strings.Contains(ops[0].CardBody, "底层真实配置") {
		t.Fatalf("status card should not include list sections, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotShowsNormalModeWhenDetached(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode: "normal",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前模式：** <text_tag color='neutral'>normal</text_tag>",
		"**接管对象类型：** 无",
		"**已接管：** 无",
	) {
		t.Fatalf("unexpected detached snapshot body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotShowsClaimedWorkspace(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode:  "normal",
			WorkspaceKey: "/data/dl/droid",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
		"**接管对象类型：** 无",
		"**已接管：** 无",
	) {
		t.Fatalf("unexpected snapshot body with workspace claim: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotShowsAttachedWorkspaceWithoutThread(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode:  "normal",
			WorkspaceKey: "/data/dl/droid",
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				ObjectType:  "workspace",
				DisplayName: "droid",
				RouteMode:   "unbound",
			},
			NextPrompt: control.PromptRouteSummary{
				CWD:                      "/data/dl/droid",
				EffectiveModel:           "gpt-5.4",
				EffectiveReasoningEffort: "medium",
				EffectiveAccessMode:      "confirm",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
		"**接管对象类型：** <text_tag color='neutral'>工作区</text_tag>",
		"**已接管：** droid",
		"**当前输入目标：** 未绑定会话",
		"**下条飞书消息：** 模型 <text_tag color='neutral'>gpt-5.4</text_tag>，推理 <text_tag color='neutral'>medium</text_tag>，权限 <text_tag color='neutral'>confirm</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body with attached workspace: %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "**工作目录：**") {
		t.Fatalf("snapshot body should hide duplicate prompt cwd, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysAutoContinueSummary(t *testing.T) {
	projector := NewProjector()
	dueAt := time.Date(2026, 4, 9, 12, 0, 30, 0, time.FixedZone("CST", 8*3600))
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			AutoContinue: control.AutoContinueSummary{
				Enabled:          true,
				PendingReason:    "retryable_failure",
				PendingDueAt:     dueAt,
				ConsecutiveCount: 2,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**autowhip：** 开启，连续 2 次，等待重试上游不稳定",
		"计划于 <text_tag color='neutral'>2026-04-09 12:00:30 CST</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysBinaryIdentityLine(t *testing.T) {
	projector := NewProjector()
	projector.SetSnapshotBinary("release/1.5 / v1.2.3 / abcdef1234")
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode: "normal",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "**当前二进制：** <text_tag color='neutral'>release/1.5 / v1.2.3 / abcdef1234</text_tag>") {
		t.Fatalf("expected snapshot binary line, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysCurrentDirectoryWithGitBranch(t *testing.T) {
	projector := NewProjector()
	cwd := testutil.WorkspacePath("data", "dl", "droid")
	projector.readGitWorktree = func(got string) *gitWorktreeSummary {
		if got != cwd {
			t.Fatalf("unexpected cwd: %q", cwd)
		}
		return &gitWorktreeSummary{Branch: "master"}
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			ProductMode:  "normal",
			WorkspaceKey: cwd,
			NextPrompt: control.PromptRouteSummary{
				CWD: cwd,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "**当前目录：** <text_tag color='neutral'>"+cwd+"</text_tag> · Git <text_tag color='neutral'>master</text_tag>") {
		t.Fatalf("expected current directory line, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysGitBranchAndCleanWorktree(t *testing.T) {
	projector := NewProjector()
	cwd := testutil.WorkspacePath("data", "dl", "droid")
	projector.readGitWorktree = func(got string) *gitWorktreeSummary {
		if got != cwd {
			t.Fatalf("unexpected cwd: %q", cwd)
		}
		return &gitWorktreeSummary{Branch: "feature/status-git"}
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			WorkspaceKey: cwd,
			NextPrompt: control.PromptRouteSummary{
				CWD: cwd,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前目录：** <text_tag color='neutral'>"+cwd+"</text_tag> · Git <text_tag color='neutral'>feature/status-git</text_tag>",
		"**Git 工作区：** <text_tag color='neutral'>干净</text_tag>",
	) {
		t.Fatalf("unexpected snapshot git body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysDirtyGitWorktreeSummary(t *testing.T) {
	projector := NewProjector()
	cwd := testutil.WorkspacePath("data", "dl", "droid")
	projector.readGitWorktree = func(got string) *gitWorktreeSummary {
		if got != cwd {
			t.Fatalf("unexpected cwd: %q", cwd)
		}
		return &gitWorktreeSummary{
			Branch:         "master",
			Dirty:          true,
			ModifiedCount:  3,
			UntrackedCount: 1,
		}
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			WorkspaceKey: testutil.WorkspacePath("data", "dl", "ignored"),
			NextPrompt: control.PromptRouteSummary{
				CWD: cwd,
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前目录：** <text_tag color='neutral'>"+cwd+"</text_tag> · Git <text_tag color='neutral'>master</text_tag>",
		"**Git 工作区：** <text_tag color='neutral'>有改动</text_tag> <text_tag color='neutral'>3修改</text_tag> <text_tag color='neutral'>1未跟踪</text_tag>",
	) {
		t.Fatalf("unexpected snapshot git body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotSkipsGitSummaryOutsideGitRepo(t *testing.T) {
	projector := NewProjector()
	projector.readGitWorktree = func(string) *gitWorktreeSummary { return nil }
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			WorkspaceKey: "/tmp/not-a-repo",
			NextPrompt: control.PromptRouteSummary{
				CWD: "/tmp/not-a-repo",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "**Git 分支：**") || strings.Contains(ops[0].CardBody, "**Git 工作区：**") {
		t.Fatalf("expected snapshot to skip git summary outside repo, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysFullAccessWithCompactToken(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:          "inst-1",
				DisplayName:         "droid",
				SelectedThreadID:    "thread-1",
				SelectedThreadTitle: "droid · 修复登录流程",
				RouteMode:           "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 修复登录流程",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "未知",
				EffectiveReasoningEffort:       "未知",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "权限 <text_tag color='neutral'>full</text_tag>") {
		t.Fatalf("expected compact full access token in snapshot body, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysSurfaceDefaultModel(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:          "inst-1",
				DisplayName:         "droid",
				SelectedThreadID:    "thread-1",
				SelectedThreadTitle: "droid · 修复登录流程",
				RouteMode:           "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 修复登录流程",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "xhigh",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
		"**下条飞书消息：** 模型 <text_tag color='neutral'>gpt-5.4</text_tag>，推理 <text_tag color='neutral'>xhigh</text_tag>，权限 <text_tag color='neutral'>full</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotDisplaysUnknownEffectivePromptValues(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:  "inst-1",
				DisplayName: "droid",
				RouteMode:   "unbound",
			},
			NextPrompt: control.PromptRouteSummary{},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "**下条飞书消息：** 模型 <text_tag color='neutral'>未知</text_tag>，推理 <text_tag color='neutral'>未知</text_tag>，权限 <text_tag color='neutral'>未知</text_tag>") {
		t.Fatalf("expected unknown effective prompt values, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotIncludesBackgroundRestoreAttachmentAndPendingLaunch(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:          "inst-headless-1",
				DisplayName:         "droid",
				Source:              "headless",
				Managed:             true,
				PID:                 4321,
				SelectedThreadID:    "thread-1",
				SelectedThreadTitle: "droid · 修复登录流程",
				RouteMode:           "pinned",
			},
			PendingHeadless: control.PendingHeadlessSummary{
				InstanceID:  "inst-headless-2",
				ThreadTitle: "droid · 新修复",
				ThreadCWD:   "/data/dl/droid",
				PID:         5678,
				ExpiresAt:   time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC),
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 修复登录流程",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "xhigh",
				EffectiveAccessMode:            "full_access",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
			Instances: []control.InstanceSummary{
				{InstanceID: "inst-headless-1", DisplayName: "droid", Source: "headless", Managed: true, PID: 4321, WorkspaceRoot: "/data/dl/droid", Online: true},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**已接管：** droid",
		"**实例 PID：** <text_tag color='neutral'>4321</text_tag>",
		"后台恢复中：",
		"**进程 PID：** <text_tag color='neutral'>5678</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "Headless") {
		t.Fatalf("snapshot body should not expose headless label, got %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "在线实例：") {
		t.Fatalf("status card should not include online instance list, got %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotTruncatesLongSelectedPreview(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSnapshot,
		Snapshot: &control.Snapshot{
			Attachment: control.AttachmentSummary{
				InstanceID:            "inst-1",
				DisplayName:           "droid",
				SelectedThreadID:      "thread-1",
				SelectedThreadTitle:   "droid · 这是一个特别长特别长特别长的当前输入目标标题",
				SelectedThreadPreview: "这是一条特别长特别长特别长特别长的最近消息内容，需要在 status 卡片里缩略显示",
				RouteMode:             "pinned",
			},
			NextPrompt: control.PromptRouteSummary{
				ThreadID:                       "thread-1",
				ThreadTitle:                    "droid · 这是一个特别长特别长特别长的当前输入目标标题",
				CWD:                            "/data/dl/droid",
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "medium",
				EffectiveAccessMode:            "confirm",
				EffectiveModelSource:           "surface_default",
				EffectiveReasoningEffortSource: "surface_default",
				EffectiveAccessModeSource:      "surface_default",
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前输入目标：** droid · 这是一个特别长特别长特别长的当前输入目标...",
		"**最近回复：** 这是一条特别长特别长特别长特别长的最近消息内容，...",
	) {
		t.Fatalf("expected snapshot body to compact long text, got %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "**目标：**") {
		t.Fatalf("snapshot body should not repeat target line, got %#v", ops[0].CardBody)
	}
}

func TestProjectThreadSelectionChangeDoesNotExposeThreadID(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventThreadSelectionChange,
		ThreadSelection: &control.ThreadSelectionChanged{
			ThreadID: "019d561a-7fd1-74b1-9049-00533ba2b782",
			Title:    "dl · 新会话",
			Preview:  "最近一条信息",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "系统提示" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardBody != "当前输入目标已切换到：dl · 新会话\n\n最近回复：\n最近一条信息" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected thread selection change to use structured V2 send path, got %#v", ops[0])
	}
}

func TestProjectThreadSelectionChangeForNewThreadReadyAvoidsSwitchedWording(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventThreadSelectionChange,
		ThreadSelection: &control.ThreadSelectionChanged{
			RouteMode: "new_thread_ready",
			Title:     "新建会话（等待首条消息）",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "已准备新建会话。\n\n当前还没有实际会话 ID；下一条文本会作为首条消息创建新会话。" {
		t.Fatalf("unexpected new-thread-ready card body: %#v", ops[0].CardBody)
	}
	if strings.Contains(ops[0].CardBody, "当前输入目标已切换到") {
		t.Fatalf("new-thread-ready card should not look like a real thread switch: %#v", ops[0].CardBody)
	}
}
