package feishu

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestProjectSelectionPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:     control.SelectionPromptAttachInstance,
			PromptID: "prompt-1",
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "inst-1", Label: "droid", Subtitle: "/data/dl/droid", IsCurrent: true},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "在线实例" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected interactive selection card body to be empty markdown root, got %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected markdown + action button elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "1. droid - 工作目录 `/data/dl/droid` [当前]" {
		t.Fatalf("unexpected first element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["prompt_id"] != "prompt-1" || value["option_id"] != "inst-1" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
}

func TestProjectSessionSelectionPromptIncludesHint(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:     control.SelectionPromptUseThread,
			PromptID: "prompt-2",
			Title:    "最近会话",
			Hint:     "发送 `/useall` 查看全部会话。",
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "thread-1", Label: "droid · 修复登录流程", Subtitle: "/data/dl/droid"},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "最近会话" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("expected markdown + action + hint elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "1. droid · 修复登录流程\n`/data/dl/droid`" {
		t.Fatalf("unexpected option element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["prompt_id"] != "prompt-2" || value["option_id"] != "thread-1" || value["kind"] != "prompt_select" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].CardElements[2]["content"] != "发送 `/useall` 查看全部会话。" {
		t.Fatalf("unexpected hint element: %#v", ops[0].CardElements[2])
	}
}

func TestProjectRequestPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventRequestPrompt,
		RequestPrompt: &control.RequestPrompt{
			RequestID:   "req-1",
			RequestType: "approval",
			Title:       "需要确认",
			Body:        "本地 Codex 想执行：\n\n```text\ngit push\n```",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			Options: []control.RequestPromptOption{
				{OptionID: "accept", Label: "允许执行", Style: "primary"},
				{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
				{OptionID: "decline", Label: "拒绝", Style: "default"},
				{OptionID: "captureFeedback", Label: "告诉 Codex 怎么改", Style: "default"},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "需要确认" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if !containsAll(ops[0].CardBody, "当前会话：droid · 修复登录流程", "git push") {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected action row and hint, got %#v", ops[0].CardElements)
	}
	actionRow, _ := ops[0].CardElements[0]["actions"].([]map[string]any)
	if len(actionRow) != 4 {
		t.Fatalf("expected 4 request option buttons, got %#v", ops[0].CardElements[0])
	}
	acceptValue, _ := actionRow[0]["value"].(map[string]any)
	sessionValue, _ := actionRow[1]["value"].(map[string]any)
	declineValue, _ := actionRow[2]["value"].(map[string]any)
	feedbackValue, _ := actionRow[3]["value"].(map[string]any)
	if acceptValue["kind"] != "request_respond" || acceptValue["request_id"] != "req-1" || acceptValue["request_option_id"] != "accept" {
		t.Fatalf("unexpected accept payload: %#v", acceptValue)
	}
	if sessionValue["request_option_id"] != "acceptForSession" {
		t.Fatalf("unexpected accept-for-session payload: %#v", sessionValue)
	}
	if declineValue["request_option_id"] != "decline" {
		t.Fatalf("unexpected decline payload: %#v", declineValue)
	}
	if feedbackValue["request_option_id"] != "captureFeedback" {
		t.Fatalf("unexpected feedback payload: %#v", feedbackValue)
	}
}

func TestProjectNewInstanceSelectionPromptUsesRecoverAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:     control.SelectionPromptNewInstance,
			PromptID: "prompt-3",
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "headless-1", Label: "droid · 修复登录流程", Subtitle: "/data/dl/droid"},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "选择要恢复的会话" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	textValue, _ := actionRow[0]["text"].(map[string]any)
	if textValue["content"] != "恢复" {
		t.Fatalf("expected recover button label, got %#v", actionRow[0])
	}
}

func TestProjectQueueTypingAndThumbsDownReactions(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventPendingInput,
		PendingInput: &control.PendingInputState{
			SourceMessageID: "msg-1",
			QueueOn:         true,
			QueueOff:        true,
			TypingOn:        true,
			TypingOff:       true,
			ThumbsDown:      true,
		},
	})
	if len(ops) != 5 {
		t.Fatalf("expected 5 operations, got %#v", ops)
	}
	if ops[0].EmojiType != emojiQueuePending || ops[1].EmojiType != emojiQueuePending || ops[4].EmojiType != emojiDiscarded {
		t.Fatalf("unexpected queue/discard reaction projection: %#v", ops)
	}
}

func TestProjectNoticeAsSystemCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "attached",
			Text: "已接管 droid。当前输入目标：droid · 修复登录流程",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "系统提示" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardThemeKey != "system" {
		t.Fatalf("unexpected card theme: %#v", ops[0])
	}
}

func TestProjectNoticeUsesCustomTitleAndTheme(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code:     "debug_error",
			Title:    "链路错误 · wrapper.observe_codex_stdout",
			Text:     "调试信息",
			ThemeKey: "relay-error",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "链路错误 · wrapper.observe_codex_stdout" || ops[0].CardThemeKey != "relay-error" {
		t.Fatalf("expected custom notice projection, got %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockAsThreadCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventBlockCommitted,
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
	if ops[0].CardTitle != "最终回复 · droid · 修复登录流程" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardThemeKey != "thread-1" {
		t.Fatalf("unexpected theme key: %#v", ops[0])
	}
	if ops[0].CardBody != "已收到：\n\n```text\nREADME.md\nsrc\n```" {
		t.Fatalf("unexpected card body: %#v", ops[0])
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
		"如果现在从飞书发送一条消息：",
		"模型：`gpt-5.4`（飞书临时覆盖）",
		"推理强度：`medium`（会话配置）",
		"执行权限：`confirm`（飞书临时覆盖）",
		"飞书临时覆盖：模型 `gpt-5.4`，权限 `confirm`",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0])
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
	if !strings.Contains(ops[0].CardBody, "执行权限：`full`（飞书默认）") {
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
		"模型：`gpt-5.4`（飞书默认）",
		"推理强度：`xhigh`（飞书默认）",
		"执行权限：`full`（飞书默认）",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
	}
}

func TestProjectSnapshotIncludesHeadlessAttachmentAndPendingLaunch(t *testing.T) {
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
		"已接管：droid (Headless)",
		"实例 PID：`4321`",
		"Headless 创建中：",
		"进程 PID：`5678`",
		"droid (Headless) - 工作目录 `/data/dl/droid` · PID `4321`",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
	}
}

func TestProjectThreadSelectionChangeIncludesShortThreadID(t *testing.T) {
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
	if ops[0].CardBody != "当前输入目标已切换到：dl · 新会话\n\n会话 ID：7fd1…b782\n\n最近信息：\n最近一条信息" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
}

func containsAll(body string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(body, part) {
			return false
		}
	}
	return true
}
