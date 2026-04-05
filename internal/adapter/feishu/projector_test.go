package feishu

import (
	"strings"
	"testing"

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

func TestProjectTypingAndThumbsDownReactions(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventPendingInput,
		PendingInput: &control.PendingInputState{
			SourceMessageID: "msg-1",
			TypingOn:        true,
			TypingOff:       true,
			ThumbsDown:      true,
		},
	})
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations, got %#v", ops)
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
				EffectiveModel:                 "gpt-5.4",
				EffectiveReasoningEffort:       "medium",
				EffectiveModelSource:           "surface_override",
				EffectiveReasoningEffortSource: "thread",
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
		"飞书临时覆盖：模型 `gpt-5.4`",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0])
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
