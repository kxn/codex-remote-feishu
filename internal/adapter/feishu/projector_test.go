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
			Kind: control.SelectionPromptAttachInstance,
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "inst-1", Label: "droid", Subtitle: "/data/dl/droid", IsCurrent: true},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "在线 VS Code 实例" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected interactive selection card body to be empty markdown root, got %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected markdown + action button elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "1. droid - 工作目录 <text_tag color='neutral'>/data/dl/droid</text_tag> [当前]" {
		t.Fatalf("unexpected first element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "attach_instance" || value["instance_id"] != "inst-1" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
}

func TestProjectWorkspaceSelectionPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind: control.SelectionPromptAttachWorkspace,
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "/data/dl/droid",
					Label:       "droid",
					Subtitle:    "/data/dl/droid\n检测到 VS Code 当前有活动会话",
					ButtonLabel: "切换",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "工作区列表" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected markdown + action button elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "1. droid - 工作区 <text_tag color='neutral'>/data/dl/droid</text_tag>\n检测到 VS Code 当前有活动会话" {
		t.Fatalf("unexpected first element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	textValue, _ := actionRow[0]["text"].(map[string]any)
	if textValue["content"] != "切换" {
		t.Fatalf("unexpected button label: %#v", actionRow[0])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "attach_workspace" || value["workspace_key"] != "/data/dl/droid" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
}

func TestProjectSessionSelectionPromptIncludesHint(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:  control.SelectionPromptUseThread,
			Title: "最近会话",
			Hint:  "发送 `/useall` 查看全部会话。",
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
	if ops[0].CardElements[0]["content"] != "1. droid · 修复登录流程\n<text_tag color='neutral'>/data/dl/droid</text_tag>" {
		t.Fatalf("unexpected option element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "use_thread" || value["thread_id"] != "thread-1" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].CardElements[2]["content"] != "发送 <text_tag color='neutral'>/useall</text_tag> 查看全部会话。" {
		t.Fatalf("unexpected hint element: %#v", ops[0].CardElements[2])
	}
}

func TestProjectSelectionPromptStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventSelectionPrompt,
		DaemonLifecycleID: "life-1",
		SelectionPrompt: &control.SelectionPrompt{
			Kind: control.SelectionPromptUseThread,
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "thread-1", Label: "droid · 修复登录流程"},
			},
		},
	})
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected selection prompt action to carry daemon lifecycle id, got %#v", value)
	}
}

func TestProjectCommandHelpCatalogAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:   "Slash 命令帮助",
			Summary: "当前支持的 slash command 如下。",
			Sections: []control.CommandCatalogSection{{
				Title: "帮助",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"/help", "menu"},
					Description: "查看帮助或再次打开命令菜单。",
					Examples:    []string{"/menu"},
				}},
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "Slash 命令帮助" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardBody != "当前支持的 slash command 如下。" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 2 {
		t.Fatalf("expected section header and entry markdown, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**帮助**" {
		t.Fatalf("unexpected section element: %#v", ops[0].CardElements[0])
	}
	content, _ := ops[0].CardElements[1]["content"].(string)
	if !containsAll(content, "<text_tag color='neutral'>/help</text_tag>", "<text_tag color='neutral'>menu</text_tag>", "查看帮助或再次打开命令菜单。", "<text_tag color='neutral'>/menu</text_tag>") {
		t.Fatalf("unexpected entry markdown: %#v", ops[0].CardElements[1])
	}
}

func TestProjectBuiltinCommandHelpCatalogPreservesPlaceholdersAndHidesKillInstance(t *testing.T) {
	projector := NewProjector()
	catalog := control.FeishuCommandHelpCatalog()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:           control.UIEventCommandCatalog,
		CommandCatalog: &catalog,
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	var rendered strings.Builder
	rendered.WriteString(ops[0].CardBody)
	for _, element := range ops[0].CardElements {
		content, _ := element["content"].(string)
		if content == "" {
			continue
		}
		rendered.WriteByte('\n')
		rendered.WriteString(content)
	}
	body := rendered.String()
	if strings.Contains(body, "/killinstance") {
		t.Fatalf("expected builtin help catalog to hide /killinstance, got %q", body)
	}
	if strings.Contains(body, "&lt;") || strings.Contains(body, "&gt;") {
		t.Fatalf("expected command placeholders to preserve angle brackets, got %q", body)
	}
	if !containsAll(body,
		"<text_tag color='neutral'>/model</text_tag>",
		"<text_tag color='neutral'>/reasoning</text_tag>",
		"<text_tag color='neutral'>/access</text_tag>",
		"<text_tag color='neutral'>/use</text_tag>",
		"<text_tag color='neutral'>/menu</text_tag>",
	) {
		t.Fatalf("unexpected builtin help catalog body: %q", body)
	}
}

func TestProjectInteractiveCommandCatalogAddsRunCommandButtons(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:       "命令菜单",
			Summary:     "固定动作可直接点击。",
			Interactive: true,
			Sections: []control.CommandCatalogSection{{
				Title: "实例与会话",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"/list"},
					Description: "列出当前在线实例。",
					Buttons: []control.CommandCatalogButton{
						{Label: "查看实例", CommandText: "/list"},
					},
				}},
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("expected section + entry + action row, got %#v", ops[0].CardElements)
	}
	actionRow, _ := ops[0].CardElements[2]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[2])
	}
	textValue, _ := actionRow[0]["text"].(map[string]any)
	if textValue["content"] != "查看实例" {
		t.Fatalf("unexpected button label: %#v", actionRow[0])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "run_command" || value["command_text"] != "/list" {
		t.Fatalf("unexpected run command payload: %#v", value)
	}
}

func TestProjectCompactCommandCatalogStacksButtonsWithoutEntryMarkdown(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:        "推理强度",
			Summary:      "当前：`high`；飞书覆盖：`high`。",
			Interactive:  true,
			DisplayStyle: control.CommandCatalogDisplayCompactButtons,
			Sections: []control.CommandCatalogSection{{
				Title: "立即应用",
				Entries: []control.CommandCatalogEntry{{
					Title:       "点击即应用",
					Description: "这段说明在紧凑布局里不应该出现。",
					Buttons: []control.CommandCatalogButton{
						{Label: "low", CommandText: "/reasoning low"},
						{Label: "high（当前）", CommandText: "/reasoning high", Disabled: true, Style: "primary"},
					},
				}},
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("expected section + two stacked action rows, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**立即应用**" {
		t.Fatalf("unexpected section element: %#v", ops[0].CardElements[0])
	}
	if content, _ := ops[0].CardElements[1]["content"].(string); content != "" {
		t.Fatalf("compact layout should not render entry markdown, got %#v", ops[0].CardElements[1])
	}
	firstRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	secondRow, _ := ops[0].CardElements[2]["actions"].([]map[string]any)
	if len(firstRow) != 1 || len(secondRow) != 1 {
		t.Fatalf("expected one button per stacked row, got %#v / %#v", firstRow, secondRow)
	}
	firstText, _ := firstRow[0]["text"].(map[string]any)
	if firstText["content"] != "low" {
		t.Fatalf("unexpected first stacked label: %#v", firstRow[0])
	}
	secondText, _ := secondRow[0]["text"].(map[string]any)
	if secondText["content"] != "high（当前）" {
		t.Fatalf("unexpected second stacked label: %#v", secondRow[0])
	}
}

func TestProjectInteractiveCommandCatalogRendersBreadcrumbsAndCaptureButtons(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:        "模型",
			Summary:      "等待输入模型名。",
			Interactive:  true,
			DisplayStyle: control.CommandCatalogDisplayCompactButtons,
			Breadcrumbs:  []control.CommandCatalogBreadcrumb{{Label: "菜单首页"}, {Label: "发送设置"}, {Label: "模型"}},
			Sections: []control.CommandCatalogSection{{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Title:       "capture/apply fallback",
					Description: "下一条普通文本会先被捕获。",
					Buttons: []control.CommandCatalogButton{{
						Label:     "开始输入",
						Kind:      control.CommandCatalogButtonStartCommandCapture,
						CommandID: control.FeishuCommandModel,
					}},
				}},
			}},
			RelatedButtons: []control.CommandCatalogButton{{
				Label:     "取消",
				Kind:      control.CommandCatalogButtonCancelCommandCapture,
				CommandID: control.FeishuCommandModel,
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected breadcrumb + section + action + related action, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "菜单首页 / 发送设置 / 模型" {
		t.Fatalf("unexpected breadcrumb element: %#v", ops[0].CardElements[0])
	}
	actionRow, _ := ops[0].CardElements[2]["actions"].([]map[string]any)
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "start_command_capture" || value["command_id"] != control.FeishuCommandModel {
		t.Fatalf("unexpected start capture payload: %#v", value)
	}
	relatedRow, _ := ops[0].CardElements[3]["actions"].([]map[string]any)
	relatedValue, _ := relatedRow[0]["value"].(map[string]any)
	if relatedValue["kind"] != "cancel_command_capture" || relatedValue["command_id"] != control.FeishuCommandModel {
		t.Fatalf("unexpected cancel capture payload: %#v", relatedValue)
	}
}

func TestProjectInteractiveCommandCatalogStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventCommandCatalog,
		DaemonLifecycleID: "life-1",
		CommandCatalog: &control.CommandCatalog{
			Interactive: true,
			Sections: []control.CommandCatalogSection{{
				Entries: []control.CommandCatalogEntry{{
					Buttons: []control.CommandCatalogButton{{Label: "查看实例", CommandText: "/list"}},
				}},
			}},
		},
	})
	actionRow, _ := ops[0].CardElements[1]["actions"].([]map[string]any)
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected command catalog action to carry daemon lifecycle id, got %#v", value)
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
	if ops[0].CardThemeKey != cardThemeApproval {
		t.Fatalf("unexpected request prompt theme: %#v", ops[0])
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

func TestProjectRequestPromptStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventRequestPrompt,
		DaemonLifecycleID: "life-1",
		RequestPrompt: &control.RequestPrompt{
			RequestID:   "req-1",
			RequestType: "approval",
			Options: []control.RequestPromptOption{
				{OptionID: "accept", Label: "允许执行", Style: "primary"},
			},
		},
	})
	actionRow, _ := ops[0].CardElements[0]["actions"].([]map[string]any)
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected request prompt action to carry daemon lifecycle id, got %#v", value)
	}
}

func TestProjectKickThreadPromptUsesCustomButtonLabels(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind: control.SelectionPromptKickThread,
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "cancel", Label: "保留当前状态，不执行强踢。", ButtonLabel: "取消"},
				{Index: 2, OptionID: "thread-1", Label: "droid · 修复登录流程", Subtitle: "/data/dl/droid\n已被其他飞书会话占用，可强踢", ButtonLabel: "强踢并占用"},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "强踢当前会话？" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	actionRow, _ := ops[0].CardElements[3]["actions"].([]map[string]any)
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button for confirm row, got %#v", ops[0].CardElements[3])
	}
	textValue, _ := actionRow[0]["text"].(map[string]any)
	if textValue["content"] != "强踢并占用" {
		t.Fatalf("expected custom button label, got %#v", actionRow[0])
	}
	value, _ := actionRow[0]["value"].(map[string]any)
	if value["kind"] != "kick_thread_confirm" || value["thread_id"] != "thread-1" {
		t.Fatalf("unexpected kick confirm payload: %#v", value)
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

func TestProjectThumbsUpReaction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventPendingInput,
		PendingInput: &control.PendingInputState{
			SourceMessageID: "msg-1",
			QueueOff:        true,
			ThumbsUp:        true,
		},
	})
	if len(ops) != 2 {
		t.Fatalf("expected queue removal plus thumbs-up, got %#v", ops)
	}
	if ops[0].Kind != OperationRemoveReaction || ops[0].EmojiType != emojiQueuePending {
		t.Fatalf("expected first op to remove queue reaction, got %#v", ops)
	}
	if ops[1].Kind != OperationAddReaction || ops[1].EmojiType != emojiSteered {
		t.Fatalf("expected second op to add thumbs-up reaction, got %#v", ops)
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
	if ops[0].CardThemeKey != cardThemeSuccess {
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
	if ops[0].CardTitle != "链路错误 · wrapper.observe_codex_stdout" || ops[0].CardThemeKey != cardThemeError {
		t.Fatalf("expected custom notice projection, got %#v", ops[0])
	}
}

func TestProjectDebugErrorNoticeRendersInlineTagsAndPreservesFence(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "debug_error",
			Text: "位置：`gateway_apply`\n错误码：`send_card_failed`\n\n调试信息：\n```text\nraw `payload`\n```",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"位置：<text_tag color='neutral'>gateway_apply</text_tag>",
		"错误码：<text_tag color='neutral'>send_card_failed</text_tag>",
		"```text\nraw `payload`\n```",
	) {
		t.Fatalf("unexpected debug error body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticeRendersInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "用法：`/model` 查看当前配置；`/model <模型>`；`/model <模型> <推理强度>`；`/model clear`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"用法：<text_tag color='neutral'>/model</text_tag> 查看当前配置；",
		"<text_tag color='neutral'>/model &lt;模型&gt;</text_tag>",
		"<text_tag color='neutral'>/model clear</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticePreservesGreaterThanInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "核心证据很简单：`section -> entry -> button`，动作键：`run_command`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&gt;") {
		t.Fatalf("expected inline code to preserve >, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>section -> entry -> button</text_tag>",
		"<text_tag color='neutral'>run_command</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
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
	if !containsAll(ops[0].CardBody, "新建会话（等待首条消息）", "**目标：** 新建会话", "**工作目录：** <text_tag color='neutral'>/data/dl/droid</text_tag>") {
		t.Fatalf("expected snapshot body to show new-thread-ready target, got %#v", ops[0].CardBody)
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
		"**输入门禁：** 有 2 个待确认请求；普通文本和图片会先被拦住",
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
	if ops[0].CardTitle != "最后答复：请帮我处理这个问题" {
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
}

func TestProjectFinalAssistantBlockRendersInlineCodeAsTags(t *testing.T) {
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
	want := "已处理 <text_tag color='neutral'>#47</text_tag>，当前 verdict 是 <text_tag color='neutral'>old</text_tag>，可发送 <text_tag color='neutral'>/use</text_tag> 重试。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected inline-tag body: %#v", ops[0])
	}
}

func TestProjectFinalAssistantBlockKeepsFencedCodeWhileRenderingInlineTags(t *testing.T) {
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
	want := "已处理 <text_tag color='neutral'>#47</text_tag>。\n\n```text\n`old`\n/use\n```\n\n外面还有 <text_tag color='neutral'>done</text_tag>。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected mixed inline/fence body: %#v", ops[0])
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
	if ops[0].CardTitle != "最后答复" {
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
	if ops[0].CardElements[3]["content"] != "**本轮用时** 3.4s" {
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
	if ops[0].CardElements[0]["content"] != "**本轮用时** 2.1s" {
		t.Fatalf("unexpected standalone elapsed footer: %#v", ops[0].CardElements[0])
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
	if ops[0].CardTitle != "最后答复：一二三四五六七八九十..." {
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
	if ops[0].CardTitle != "最后答复：please help me align the return format for this API..." {
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
		"**接管对象类型：** <text_tag color='neutral'>VS Code 实例</text_tag>",
		"如果现在从飞书发送一条消息：",
		"**模型：** <text_tag color='neutral'>gpt-5.4</text_tag>（飞书临时覆盖）",
		"**推理强度：** <text_tag color='neutral'>medium</text_tag>（会话配置）",
		"**执行权限：** <text_tag color='neutral'>confirm</text_tag>（飞书临时覆盖）",
		"**飞书临时覆盖：** 模型 <text_tag color='neutral'>gpt-5.4</text_tag>，权限 <text_tag color='neutral'>confirm</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0])
	}
	if strings.Contains(ops[0].CardBody, "已知会话：") || strings.Contains(ops[0].CardBody, "在线实例：") {
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
		"**当前 workspace：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
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
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsAll(ops[0].CardBody,
		"**当前 workspace：** <text_tag color='neutral'>/data/dl/droid</text_tag>",
		"**接管对象类型：** <text_tag color='neutral'>工作区</text_tag>",
		"**已接管：** droid",
		"**当前输入目标：** 未绑定会话",
	) {
		t.Fatalf("unexpected snapshot body with attached workspace: %#v", ops[0].CardBody)
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
		"**自动继续：** 开启，连续 2 次，等待重试可恢复失败",
		"计划于 <text_tag color='neutral'>2026-04-09 12:00:30 CST</text_tag>",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
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
	if !strings.Contains(ops[0].CardBody, "**执行权限：** <text_tag color='neutral'>full</text_tag>（飞书默认）") {
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
		"**模型：** <text_tag color='neutral'>gpt-5.4</text_tag>（飞书默认）",
		"**推理强度：** <text_tag color='neutral'>xhigh</text_tag>（飞书默认）",
		"**执行权限：** <text_tag color='neutral'>full</text_tag>（飞书默认）",
	) {
		t.Fatalf("unexpected snapshot body: %#v", ops[0].CardBody)
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
