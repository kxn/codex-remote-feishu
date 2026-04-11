package feishu

import (
	"fmt"
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
			Kind:         control.SelectionPromptAttachInstance,
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list",
			Options: []control.SelectionOption{
				{Index: 1, OptionID: "inst-2", Label: "web", MetaText: "2分前 · 当前焦点可跟随", ButtonLabel: "切换"},
				{Index: 2, OptionID: "inst-3", Label: "ops", MetaText: "1小时前 · 当前被其他飞书会话接管", Disabled: true},
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
	if len(ops[0].CardElements) != 8 {
		t.Fatalf("expected grouped instance selection elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**当前实例**" {
		t.Fatalf("unexpected first element: %#v", ops[0].CardElements[0])
	}
	if ops[0].CardElements[1]["content"] != "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list" {
		t.Fatalf("unexpected context summary: %#v", ops[0].CardElements[1])
	}
	if ops[0].CardElements[2]["content"] != "**可接管**" {
		t.Fatalf("unexpected available header: %#v", ops[0].CardElements[2])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[3])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[3])
	}
	if cardButtonLabel(t, actionRow[0]) != "切换 · web" || actionRow[0]["width"] != "fill" {
		t.Fatalf("unexpected button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "attach_instance" || value["instance_id"] != "inst-2" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].CardElements[4]["content"] != "2分前 · 当前焦点可跟随" {
		t.Fatalf("unexpected available meta: %#v", ops[0].CardElements[4])
	}
	if ops[0].CardElements[5]["content"] != "**其他状态**" {
		t.Fatalf("unexpected unavailable header: %#v", ops[0].CardElements[5])
	}
	blockedRow := cardElementButtons(t, ops[0].CardElements[6])
	if len(blockedRow) != 1 {
		t.Fatalf("expected one unavailable action button, got %#v", ops[0].CardElements[6])
	}
	if cardButtonLabel(t, blockedRow[0]) != "不可接管 · ops" || blockedRow[0]["disabled"] != true {
		t.Fatalf("unexpected unavailable button: %#v", blockedRow[0])
	}
	if ops[0].CardElements[7]["content"] != "1小时前 · 当前被其他飞书会话接管" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected selection prompt to use structured V2 send path, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	if containsRenderedTag(renderedElements, "action") {
		t.Fatalf("expected rendered V2 selection prompt to avoid legacy action rows, got %#v", renderedElements)
	}
	renderedValue := renderedButtonCallbackValue(t, renderedElements[3])
	if renderedValue["kind"] != "attach_instance" || renderedValue["instance_id"] != "inst-2" {
		t.Fatalf("unexpected rendered V2 selection action payload: %#v", renderedValue)
	}
}

func TestProjectWorkspaceSelectionPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:         control.SelectionPromptAttachWorkspace,
			ContextTitle: "当前工作区",
			ContextText:  "droid · 5分前\n同工作区内继续工作请直接 /use 或 /new",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "/data/dl/web",
					Label:       "web",
					MetaText:    "2分前 · 有 VS Code 活动",
					ButtonLabel: "切换",
				},
				{
					Index:    2,
					OptionID: "/data/dl/ops",
					Label:    "ops",
					MetaText: "1小时前 · 当前被其他飞书会话接管",
					Disabled: true,
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
	if len(ops[0].CardElements) != 8 {
		t.Fatalf("expected grouped workspace selection elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**当前工作区**" {
		t.Fatalf("unexpected first element: %#v", ops[0].CardElements[0])
	}
	if ops[0].CardElements[1]["content"] != "droid · 5分前\n同工作区内继续工作请直接 /use 或 /new" {
		t.Fatalf("unexpected context summary: %#v", ops[0].CardElements[1])
	}
	if ops[0].CardElements[2]["content"] != "**可接管**" {
		t.Fatalf("unexpected available header: %#v", ops[0].CardElements[2])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[3])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[3])
	}
	if cardButtonLabel(t, actionRow[0]) != "切换 · web" || actionRow[0]["width"] != "fill" {
		t.Fatalf("unexpected button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "attach_workspace" || value["workspace_key"] != "/data/dl/web" {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].CardElements[4]["content"] != "2分前 · 有 VS Code 活动" {
		t.Fatalf("unexpected available meta: %#v", ops[0].CardElements[4])
	}
	if ops[0].CardElements[5]["content"] != "**其他状态**" {
		t.Fatalf("unexpected unavailable header: %#v", ops[0].CardElements[5])
	}
	blockedRow := cardElementButtons(t, ops[0].CardElements[6])
	if len(blockedRow) != 1 {
		t.Fatalf("expected unavailable action button, got %#v", ops[0].CardElements[6])
	}
	if cardButtonLabel(t, blockedRow[0]) != "不可接管 · ops" || blockedRow[0]["disabled"] != true {
		t.Fatalf("unexpected unavailable button: %#v", blockedRow[0])
	}
	if ops[0].CardElements[7]["content"] != "1小时前 · 当前被其他飞书会话接管" {
		t.Fatalf("unexpected unavailable meta: %#v", ops[0].CardElements[7])
	}
}

func TestProjectSessionSelectionPromptUsesButtonFirstLayout(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:  control.SelectionPromptUseThread,
			Title: "最近会话",
			Options: []control.SelectionOption{
				{
					Index:               1,
					OptionID:            "thread-1",
					Label:               "修复登录流程",
					ButtonLabel:         "修复登录流程",
					Subtitle:            "/data/dl/droid\n可接管",
					AllowCrossWorkspace: true,
				},
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
		t.Fatalf("expected group header + action + markdown elements, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "**可接管**" {
		t.Fatalf("unexpected group header: %#v", ops[0].CardElements[0])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[1])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	if actionRow[0]["width"] != "fill" {
		t.Fatalf("expected thread button to fill width, got %#v", actionRow[0])
	}
	if cardButtonLabel(t, actionRow[0]) != "接管 · 修复登录流程" {
		t.Fatalf("unexpected button text: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "use_thread" || value["thread_id"] != "thread-1" || value["allow_cross_workspace"] != true {
		t.Fatalf("unexpected action payload: %#v", value)
	}
	if ops[0].CardElements[2]["content"] != "<text_tag color='neutral'>/data/dl/droid</text_tag>\n可接管" {
		t.Fatalf("unexpected option detail element: %#v", ops[0].CardElements[2])
	}
}

func TestProjectWorkspaceSelectionPromptPreservesShowWorkspaceThreadsAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:  control.SelectionPromptAttachWorkspace,
			Title: "工作区列表",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "/data/dl/picdetect",
					Label:       "picdetect",
					ButtonLabel: "恢复",
					MetaText:    "3分前 · 后台可恢复",
					ActionKind:  "show_workspace_threads",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[1])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[1])
	}
	if cardButtonLabel(t, actionRow[0]) != "恢复 · picdetect" {
		t.Fatalf("unexpected button text: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "show_workspace_threads" || value["workspace_key"] != "/data/dl/picdetect" {
		t.Fatalf("expected workspace selection to preserve special action, got %#v", value)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedValue := renderedButtonCallbackValue(t, renderedElements[1])
	if renderedValue["kind"] != "show_workspace_threads" || renderedValue["workspace_key"] != "/data/dl/picdetect" {
		t.Fatalf("expected rendered V2 workspace selection to preserve special action, got %#v", renderedValue)
	}
}

func TestProjectWorkspaceSelectionPromptRendersExpandAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:  control.SelectionPromptAttachWorkspace,
			Title: "工作区列表",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "/data/dl/web",
					Label:       "web",
					ButtonLabel: "切换",
					MetaText:    "2分前 · 有 VS Code 活动",
				},
				{
					Index:       2,
					Label:       "全部工作区",
					ButtonLabel: "全部工作区",
					MetaText:    "还有 3 个工作区未显示",
					ActionKind:  "show_all_workspaces",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 6 {
		t.Fatalf("expected workspace card to include more section, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[3]["content"] != "**更多**" {
		t.Fatalf("expected more header, got %#v", ops[0].CardElements[3])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[4])
	if len(actionRow) != 1 {
		t.Fatalf("expected one expand action button, got %#v", ops[0].CardElements[4])
	}
	if cardButtonLabel(t, actionRow[0]) != "查看全部 · 全部工作区" {
		t.Fatalf("unexpected expand button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "show_all_workspaces" {
		t.Fatalf("unexpected expand payload: %#v", value)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedValue := renderedButtonCallbackValue(t, renderedElements[4])
	if renderedValue["kind"] != "show_all_workspaces" {
		t.Fatalf("unexpected rendered expand payload: %#v", renderedValue)
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
				{Index: 1, OptionID: "thread-1", Label: "修复登录流程", ButtonLabel: "修复登录流程"},
			},
		},
	})
	actionRow := cardElementButtons(t, ops[0].CardElements[1])
	value := cardButtonPayload(t, actionRow[0])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected selection prompt action to carry daemon lifecycle id, got %#v", value)
	}
}

func TestProjectUseAllSelectionPromptGroupsByWorkspace(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout:       "workspace_grouped_useall",
			Kind:         control.SelectionPromptUseThread,
			Title:        "全部会话",
			ContextTitle: "当前工作区",
			ContextText:  "droid · 5分前\n同工作区内切换请直接用 /use",
			ContextKey:   "/data/dl/droid",
			Options: []control.SelectionOption{
				{
					Index:       3,
					OptionID:    "thread-2",
					Label:       "别的会话",
					ButtonLabel: "别的会话",
					GroupKey:    "/data/dl/web",
					GroupLabel:  "web",
					AgeText:     "2分前",
					MetaText:    "2分14秒前",
				},
				{
					Index:       1,
					OptionID:    "thread-1",
					Label:       "当前会话",
					ButtonLabel: "当前会话",
					GroupKey:    "/data/dl/droid",
					GroupLabel:  "droid",
					MetaText:    "已接管",
					IsCurrent:   true,
				},
				{
					Index:       4,
					OptionID:    "thread-3",
					Label:       "另一个会话",
					ButtonLabel: "另一个会话",
					GroupKey:    "/data/dl/web",
					GroupLabel:  "web",
					AgeText:     "2分前",
					MetaText:    "38分前 · VS Code 占用中",
				},
				{
					Index:       5,
					OptionID:    "thread-4",
					Label:       "不可接管会话",
					ButtonLabel: "不可接管会话",
					GroupKey:    "/data/dl/ops",
					GroupLabel:  "ops",
					AgeText:     "1小时前",
					MetaText:    "当前被其他飞书会话接管，暂不可接管",
					Disabled:    true,
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	wantHeaders := []string{"**当前会话**", "**当前工作区**", "**web · 2分前**", "**ops · 1小时前**"}
	for _, header := range wantHeaders {
		found := false
		for _, element := range ops[0].CardElements {
			if element["content"] == header {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected grouped header %q, got %#v", header, ops[0].CardElements)
		}
	}
	var buttonLabels []string
	for _, element := range ops[0].CardElements {
		if element["tag"] != "button" && element["tag"] != "column_set" && element["tag"] != "action" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			buttonLabels = append(buttonLabels, cardButtonLabel(t, button))
		}
	}
	if strings.Join(buttonLabels, " | ") != "当前 · 当前会话 | 查看当前工作区全部会话 | 接管 · 别的会话 | 接管 · 另一个会话" {
		t.Fatalf("unexpected grouped button labels: %#v", buttonLabels)
	}
	var rendered []string
	for _, element := range ops[0].CardElements {
		if content, _ := element["content"].(string); content != "" {
			rendered = append(rendered, content)
		}
	}
	for _, fragment := range []string{
		"droid · 5分前\n同工作区内切换请直接用 /use",
		"1. 2分14秒前",
		"2. 38分前 · VS Code 占用中",
		"当前被其他飞书会话接管，暂不可接管",
	} {
		if !containsString(rendered, fragment) {
			t.Fatalf("expected rendered grouped content to include %q, got %#v", fragment, rendered)
		}
	}
}

func TestProjectUseAllSelectionPromptLimitsWorkspaceToFiveAndAddsExpandButtons(t *testing.T) {
	projector := NewProjector()
	options := []control.SelectionOption{
		{
			Index:       1,
			OptionID:    "thread-current",
			Label:       "当前会话",
			ButtonLabel: "当前会话",
			GroupKey:    "/data/dl/droid",
			GroupLabel:  "droid",
			MetaText:    "已接管",
			IsCurrent:   true,
		},
	}
	for i := 1; i <= 6; i++ {
		options = append(options, control.SelectionOption{
			Index:       i + 1,
			OptionID:    fmt.Sprintf("thread-%d", i),
			Label:       fmt.Sprintf("web-%d", i),
			ButtonLabel: fmt.Sprintf("web-%d", i),
			GroupKey:    "/data/dl/web",
			GroupLabel:  "web",
			AgeText:     "2分前",
			MetaText:    fmt.Sprintf("%d分前", i),
		})
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout:       "workspace_grouped_useall",
			Kind:         control.SelectionPromptUseThread,
			Title:        "全部会话",
			ContextTitle: "当前工作区",
			ContextText:  "droid · 5分前\n同工作区内切换请直接用 /use",
			ContextKey:   "/data/dl/droid",
			Options:      options,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	var buttonLabels []string
	for _, element := range ops[0].CardElements {
		if element["tag"] != "button" && element["tag"] != "column_set" && element["tag"] != "action" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			buttonLabels = append(buttonLabels, cardButtonLabel(t, button))
		}
	}
	if strings.Join(buttonLabels, " | ") != "当前 · 当前会话 | 查看当前工作区全部会话 | 接管 · web-1 | 接管 · web-2 | 接管 · web-3 | 接管 · web-4 | 接管 · web-5 | 查看web全部会话" {
		t.Fatalf("unexpected grouped/limited button labels: %#v", buttonLabels)
	}
}

func TestProjectUseAllSelectionPromptRendersWorkspaceGroupExpandAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout: "workspace_grouped_useall",
			Kind:   control.SelectionPromptUseThread,
			Title:  "全部会话",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "thread-1",
					Label:       "web-1",
					ButtonLabel: "web-1",
					GroupKey:    "/data/dl/web",
					GroupLabel:  "web",
					AgeText:     "2分前",
					MetaText:    "2分前",
				},
				{
					Index:       2,
					Label:       "全部工作区",
					ButtonLabel: "全部工作区",
					MetaText:    "还有 3 个工作区未显示",
					ActionKind:  "show_all_thread_workspaces",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardElements[3]["content"] != "**更多**" {
		t.Fatalf("expected more header, got %#v", ops[0].CardElements)
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[4])
	if len(actionRow) != 1 {
		t.Fatalf("expected one expand action button, got %#v", ops[0].CardElements[4])
	}
	if cardButtonLabel(t, actionRow[0]) != "查看全部 · 全部工作区" {
		t.Fatalf("unexpected expand button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "show_all_thread_workspaces" {
		t.Fatalf("unexpected expand payload: %#v", value)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedValue := renderedButtonCallbackValue(t, renderedElements[4])
	if renderedValue["kind"] != "show_all_thread_workspaces" {
		t.Fatalf("unexpected rendered expand payload: %#v", renderedValue)
	}
}

func TestProjectUseAllSelectionPromptRendersWorkspaceGroupReturnAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout: "workspace_grouped_useall",
			Kind:   control.SelectionPromptUseThread,
			Title:  "全部会话",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "thread-1",
					Label:       "web-1",
					ButtonLabel: "web-1",
					GroupKey:    "/data/dl/web",
					GroupLabel:  "web",
					AgeText:     "2分前",
					MetaText:    "2分前",
				},
				{
					Index:       2,
					Label:       "最近工作区",
					ButtonLabel: "最近工作区",
					MetaText:    "回到最近 5 个工作区",
					ActionKind:  "show_recent_thread_workspaces",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[4])
	if len(actionRow) != 1 {
		t.Fatalf("expected one return action button, got %#v", ops[0].CardElements[4])
	}
	if cardButtonLabel(t, actionRow[0]) != "返回 · 最近工作区" {
		t.Fatalf("unexpected return button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "show_recent_thread_workspaces" {
		t.Fatalf("unexpected return payload: %#v", value)
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedValue := renderedButtonCallbackValue(t, renderedElements[4])
	if renderedValue["kind"] != "show_recent_thread_workspaces" {
		t.Fatalf("unexpected rendered return payload: %#v", renderedValue)
	}
}

func TestProjectVSCodeRecentSelectionPromptShowsInstanceSummaryAndMore(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout:       "vscode_instance_threads",
			Kind:         control.SelectionPromptUseThread,
			Title:        "最近会话",
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "thread-current",
					Label:       "修复登录流程",
					ButtonLabel: "修复登录流程",
					MetaText:    "当前跟随中 · 20秒前",
					IsCurrent:   true,
				},
				{
					Index:       2,
					OptionID:    "thread-focus",
					Label:       "整理日志",
					ButtonLabel: "整理日志",
					MetaText:    "VS Code 当前焦点 · 1分前",
				},
				{
					Index:       3,
					ButtonLabel: "当前实例全部会话",
					MetaText:    "展开当前实例内的全部会话",
					ActionKind:  "show_scoped_threads",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	wantHeaders := []string{"**当前实例**", "**当前会话**", "**可接管**", "**更多**"}
	for _, header := range wantHeaders {
		found := false
		for _, element := range ops[0].CardElements {
			if element["content"] == header {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected header %q, got %#v", header, ops[0].CardElements)
		}
	}
	var buttonLabels []string
	for _, element := range ops[0].CardElements {
		if element["tag"] != "button" && element["tag"] != "column_set" && element["tag"] != "action" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			buttonLabels = append(buttonLabels, cardButtonLabel(t, button))
		}
	}
	if strings.Join(buttonLabels, " | ") != "当前 · 修复登录流程 | 接管 · 整理日志 | 查看全部 · 当前实例全部会话" {
		t.Fatalf("unexpected vscode recent button labels: %#v", buttonLabels)
	}
}

func TestProjectVSCodeAllSelectionPromptUsesNumberedMetaRows(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventSelectionPrompt,
		SelectionPrompt: &control.SelectionPrompt{
			Layout:       "vscode_instance_threads",
			Kind:         control.SelectionPromptUseThread,
			Title:        "当前实例全部会话",
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "thread-current",
					Label:       "修复登录流程",
					ButtonLabel: "修复登录流程",
					MetaText:    "当前跟随中 · 20秒前",
					IsCurrent:   true,
				},
				{
					Index:       2,
					OptionID:    "thread-focus",
					Label:       "整理日志",
					ButtonLabel: "整理日志",
					MetaText:    "VS Code 当前焦点 · 1分前",
				},
				{
					Index:       3,
					OptionID:    "thread-old",
					Label:       "历史会话",
					ButtonLabel: "历史会话",
					MetaText:    "已被其他飞书会话接管",
					Disabled:    true,
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	var rendered []string
	for _, element := range ops[0].CardElements {
		if content, _ := element["content"].(string); content != "" {
			rendered = append(rendered, content)
		}
	}
	for _, fragment := range []string{"**当前实例**", "**当前会话**", "**全部会话**", "1. VS Code 当前焦点 · 1分前", "2. 已被其他飞书会话接管"} {
		if !containsString(rendered, fragment) {
			t.Fatalf("expected rendered vscode all content to include %q, got %#v", fragment, rendered)
		}
	}
	var buttonLabels []string
	for _, element := range ops[0].CardElements {
		if element["tag"] != "button" && element["tag"] != "column_set" && element["tag"] != "action" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			buttonLabels = append(buttonLabels, cardButtonLabel(t, button))
		}
	}
	if strings.Join(buttonLabels, " | ") != "当前 · 修复登录流程 | 接管 · 整理日志 | 不可接管 · 历史会话" {
		t.Fatalf("unexpected vscode all button labels: %#v", buttonLabels)
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

func TestProjectCommandHelpCatalogPreservesAmpersandsInCommandTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:   "命令帮助",
			Summary: "常用联调命令。",
			Sections: []control.CommandCatalogSection{{
				Title: "联调",
				Entries: []control.CommandCatalogEntry{{
					Commands:    []string{"go test ./internal/app/daemon ./internal/core/orchestrator"},
					Description: "先跑 daemon/orchestrator。",
					Examples:    []string{"cd web && npm test -- --run src/lib/api.test.ts"},
				}},
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	content, _ := ops[0].CardElements[1]["content"].(string)
	if strings.Contains(content, "&amp;&amp;") {
		t.Fatalf("expected command tag to preserve &&, got %q", content)
	}
	if !containsAll(content,
		"<text_tag color='neutral'>go test ./internal/app/daemon ./internal/core/orchestrator</text_tag>",
		"<text_tag color='neutral'>cd web && npm test -- --run src/lib/api.test.ts</text_tag>",
	) {
		t.Fatalf("unexpected command catalog markdown: %q", content)
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
	actionRow := cardElementButtons(t, ops[0].CardElements[2])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[2])
	}
	if cardButtonLabel(t, actionRow[0]) != "查看实例" {
		t.Fatalf("unexpected button label: %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
	if value["kind"] != "run_command" || value["command_text"] != "/list" {
		t.Fatalf("unexpected run command payload: %#v", value)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected button-only command catalog to use structured V2 send path, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	if containsRenderedTag(renderedElements, "action") {
		t.Fatalf("expected rendered V2 command catalog to avoid legacy action rows, got %#v", renderedElements)
	}
	renderedValue := renderedButtonCallbackValue(t, renderedElements[3])
	if renderedValue["kind"] != "run_command" || renderedValue["command_text"] != "/list" {
		t.Fatalf("unexpected rendered V2 command button payload: %#v", renderedValue)
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
	firstRow := cardElementButtons(t, ops[0].CardElements[1])
	secondRow := cardElementButtons(t, ops[0].CardElements[2])
	if len(firstRow) != 1 || len(secondRow) != 1 {
		t.Fatalf("expected one button per stacked row, got %#v / %#v", firstRow, secondRow)
	}
	if cardButtonLabel(t, firstRow[0]) != "low" {
		t.Fatalf("unexpected first stacked label: %#v", firstRow[0])
	}
	if cardButtonLabel(t, secondRow[0]) != "high（当前）" {
		t.Fatalf("unexpected second stacked label: %#v", secondRow[0])
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected compact button catalog to use structured V2 send path, got %#v", ops[0])
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	if renderedElements[2]["tag"] != "button" || renderedElements[3]["tag"] != "button" {
		t.Fatalf("expected stacked compact buttons to render as direct V2 buttons, got %#v", renderedElements)
	}
}

func TestProjectInteractiveCommandCatalogRendersBreadcrumbsAndCommandForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:        "模型",
			Summary:      "直接在卡片里输入模型名。",
			Interactive:  true,
			DisplayStyle: control.CommandCatalogDisplayCompactButtons,
			Breadcrumbs:  []control.CommandCatalogBreadcrumb{{Label: "菜单首页"}, {Label: "发送设置"}, {Label: "模型"}},
			Sections: []control.CommandCatalogSection{{
				Title: "手动输入",
				Entries: []control.CommandCatalogEntry{{
					Form: &control.CommandCatalogForm{
						CommandID:   control.FeishuCommandModel,
						CommandText: "/model",
						SubmitLabel: "应用",
						Field: control.CommandCatalogFormField{
							Name:        "command_args",
							Kind:        control.CommandCatalogFormFieldText,
							Label:       "输入模型名，或输入“模型名 推理强度”。",
							Placeholder: "gpt-5.4 high",
						},
					},
				}},
			}},
			RelatedButtons: []control.CommandCatalogButton{{
				Label:       "返回发送设置",
				CommandText: "/menu send_settings",
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected breadcrumb + section + form + related action, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[0]["content"] != "菜单首页 / 发送设置 / 模型" {
		t.Fatalf("unexpected breadcrumb element: %#v", ops[0].CardElements[0])
	}
	formContainer := ops[0].CardElements[2]
	if formContainer["tag"] != "form" {
		t.Fatalf("expected form container, got %#v", formContainer)
	}
	formElements, _ := formContainer["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected input + submit button, got %#v", formContainer)
	}
	input, _ := formElements[0]["name"].(string)
	if input != "command_args" {
		t.Fatalf("unexpected form field name: %#v", formElements[0])
	}
	if formElements[1]["action_type"] != nil || formElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected V2 form submit button, got %#v", formElements[1])
	}
	value := cardButtonPayload(t, formElements[1])
	if value["kind"] != "submit_command_form" || value["command"] != "/model" || value["field_name"] != "command_args" {
		t.Fatalf("unexpected submit payload: %#v", value)
	}
	relatedRow := cardElementButtons(t, ops[0].CardElements[3])
	relatedValue := cardButtonPayload(t, relatedRow[0])
	if relatedValue["kind"] != "run_command" || relatedValue["command_text"] != "/menu send_settings" {
		t.Fatalf("unexpected related button payload: %#v", relatedValue)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected command catalog with form to use V2 in #120, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	if renderedElements[3]["tag"] != "form" {
		t.Fatalf("expected rendered V2 form element, got %#v", renderedElements)
	}
	renderedFormElements, _ := renderedElements[3]["elements"].([]map[string]any)
	if len(renderedFormElements) != 2 {
		t.Fatalf("expected rendered V2 form to keep input and submit button, got %#v", renderedElements[3])
	}
	if renderedFormElements[1]["action_type"] != nil || renderedFormElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected command form submit button to use V2 form_action_type, got %#v", renderedFormElements[1])
	}
	renderedSubmitValue := renderedButtonCallbackValue(t, renderedFormElements[1])
	if renderedSubmitValue["kind"] != "submit_command_form" || renderedSubmitValue["command"] != "/model" {
		t.Fatalf("unexpected rendered command form payload: %#v", renderedSubmitValue)
	}
	renderedRelatedValue := renderedButtonCallbackValue(t, renderedElements[4])
	if renderedRelatedValue["kind"] != "run_command" || renderedRelatedValue["command_text"] != "/menu send_settings" {
		t.Fatalf("unexpected rendered related button payload: %#v", renderedRelatedValue)
	}
}

func TestProjectCommandFormStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventCommandCatalog,
		DaemonLifecycleID: "life-1",
		CommandCatalog: &control.CommandCatalog{
			Interactive: true,
			Sections: []control.CommandCatalogSection{{
				Entries: []control.CommandCatalogEntry{{
					Form: &control.CommandCatalogForm{
						CommandID:   control.FeishuCommandReasoning,
						CommandText: "/reasoning",
						SubmitLabel: "应用",
						Field: control.CommandCatalogFormField{
							Name: "command_args",
							Kind: control.CommandCatalogFormFieldText,
						},
					},
				}},
			}},
		},
	})
	formContainer := ops[0].CardElements[0]
	formElements, _ := formContainer["elements"].([]map[string]any)
	value := cardButtonPayload(t, formElements[1])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected form action to carry daemon lifecycle id, got %#v", value)
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
	actionRow := cardElementButtons(t, ops[0].CardElements[0])
	value := cardButtonPayload(t, actionRow[0])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected command catalog action to carry daemon lifecycle id, got %#v", value)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected command catalog with form to use structured V2 send path, got %#v", ops[0])
	}
}

func TestProjectInteractiveCommandCatalogRelatedButtonsUseV2WhenNoForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventCommandCatalog,
		CommandCatalog: &control.CommandCatalog{
			Title:        "发送设置",
			Summary:      "请选择操作。",
			Interactive:  true,
			DisplayStyle: control.CommandCatalogDisplayDefault,
			RelatedButtons: []control.CommandCatalogButton{{
				Label:       "返回菜单",
				CommandText: "/menu",
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected related-buttons catalog without form to use V2, got %#v", ops[0])
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	if len(renderedElements) != 2 {
		t.Fatalf("expected summary markdown plus related button, got %#v", renderedElements)
	}
	renderedValue := renderedButtonCallbackValue(t, renderedElements[1])
	if renderedValue["kind"] != "run_command" || renderedValue["command_text"] != "/menu" {
		t.Fatalf("unexpected related button callback payload: %#v", renderedValue)
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
	actionRow := cardElementButtons(t, ops[0].CardElements[0])
	if len(actionRow) != 4 {
		t.Fatalf("expected 4 request option buttons, got %#v", ops[0].CardElements[0])
	}
	acceptValue := cardButtonPayload(t, actionRow[0])
	sessionValue := cardButtonPayload(t, actionRow[1])
	declineValue := cardButtonPayload(t, actionRow[2])
	feedbackValue := cardButtonPayload(t, actionRow[3])
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
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected request prompt to use structured V2 send path, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedButtons := renderedColumnButtons(t, renderedElements[1])
	if len(renderedButtons) != 4 {
		t.Fatalf("expected rendered V2 request prompt to keep 4 buttons, got %#v", renderedElements[1])
	}
	renderedAcceptValue := renderedButtonCallbackValue(t, renderedButtons[0])
	renderedSessionValue := renderedButtonCallbackValue(t, renderedButtons[1])
	renderedDeclineValue := renderedButtonCallbackValue(t, renderedButtons[2])
	renderedFeedbackValue := renderedButtonCallbackValue(t, renderedButtons[3])
	if renderedAcceptValue["request_option_id"] != "accept" || renderedSessionValue["request_option_id"] != "acceptForSession" {
		t.Fatalf("unexpected rendered request accept payloads: %#v / %#v", renderedAcceptValue, renderedSessionValue)
	}
	if renderedDeclineValue["request_option_id"] != "decline" || renderedFeedbackValue["request_option_id"] != "captureFeedback" {
		t.Fatalf("unexpected rendered request decline/feedback payloads: %#v / %#v", renderedDeclineValue, renderedFeedbackValue)
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
	actionRow := cardElementButtons(t, ops[0].CardElements[0])
	value := cardButtonPayload(t, actionRow[0])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected request prompt action to carry daemon lifecycle id, got %#v", value)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected request prompt to use structured V2 send path, got %#v", ops[0])
	}
}

func TestProjectRequestUserInputPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventRequestPrompt,
		RequestPrompt: &control.RequestPrompt{
			RequestID:   "req-ui-1",
			RequestType: "request_user_input",
			Title:       "需要补充输入",
			Body:        "本地 Codex 正在等待你补充参数或说明。",
			ThreadTitle: "droid · 修复登录流程",
			Questions: []control.RequestPromptQuestion{
				{
					ID:             "model",
					Header:         "模型",
					Question:       "请选择模型",
					DirectResponse: true,
					Options: []control.RequestPromptQuestionOption{
						{Label: "gpt-5.4", Description: "推荐"},
						{Label: "gpt-5.3"},
					},
				},
				{
					ID:          "notes",
					Header:      "备注",
					Question:    "补充说明",
					AllowOther:  true,
					Secret:      true,
					Placeholder: "请填写补充说明",
				},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) < 4 {
		t.Fatalf("expected question elements and form, got %#v", ops[0].CardElements)
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[1])
	if len(actionRow) != 2 {
		t.Fatalf("expected direct response buttons for first question, got %#v", ops[0].CardElements[1])
	}
	value := cardButtonPayload(t, actionRow[0])
	requestAnswers, _ := value["request_answers"].(map[string]any)
	modelAnswers, _ := requestAnswers["model"].([]any)
	if value["kind"] != "request_respond" || len(modelAnswers) != 1 || modelAnswers[0] != "gpt-5.4" {
		t.Fatalf("unexpected request option payload: %#v", value)
	}
	form, _ := ops[0].CardElements[3]["elements"].([]map[string]any)
	if len(form) != 3 {
		t.Fatalf("expected two inputs and one submit button, got %#v", ops[0].CardElements[2])
	}
	submitValue := cardButtonPayload(t, form[2])
	if submitValue["kind"] != "submit_request_form" || submitValue["request_id"] != "req-ui-1" {
		t.Fatalf("unexpected request form payload: %#v", submitValue)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected request_user_input prompt to use structured V2 send path, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	renderedButtons := renderedColumnButtons(t, renderedElements[2])
	if len(renderedButtons) != 2 {
		t.Fatalf("expected rendered direct-response button row, got %#v", renderedElements[2])
	}
	renderedValue := renderedButtonCallbackValue(t, renderedButtons[0])
	renderedAnswers, _ := renderedValue["request_answers"].(map[string]any)
	renderedModelAnswers, _ := renderedAnswers["model"].([]any)
	if renderedValue["kind"] != "request_respond" || len(renderedModelAnswers) != 1 || renderedModelAnswers[0] != "gpt-5.4" {
		t.Fatalf("unexpected rendered direct-response payload: %#v", renderedValue)
	}
	renderedForm := renderedElements[4]
	if renderedForm["tag"] != "form" {
		t.Fatalf("expected rendered V2 request form, got %#v", renderedForm)
	}
	renderedFormElements, _ := renderedForm["elements"].([]map[string]any)
	if len(renderedFormElements) != 3 {
		t.Fatalf("expected rendered request form to keep two inputs and submit button, got %#v", renderedForm)
	}
	if renderedFormElements[2]["action_type"] != nil || renderedFormElements[2]["form_action_type"] != "submit" {
		t.Fatalf("expected rendered request form submit button to use V2 form_action_type, got %#v", renderedFormElements[2])
	}
	renderedSubmitValue := renderedButtonCallbackValue(t, renderedFormElements[2])
	if renderedSubmitValue["kind"] != "submit_request_form" || renderedSubmitValue["request_id"] != "req-ui-1" {
		t.Fatalf("unexpected rendered request form payload: %#v", renderedSubmitValue)
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
	actionRow := cardElementButtons(t, ops[0].CardElements[3])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button for confirm row, got %#v", ops[0].CardElements[3])
	}
	if cardButtonLabel(t, actionRow[0]) != "强踢并占用" {
		t.Fatalf("expected custom button label, got %#v", actionRow[0])
	}
	value := cardButtonPayload(t, actionRow[0])
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
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected notice to use structured V2 send path, got %#v", ops[0])
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
		"<text_tag color='neutral'>/model <模型></text_tag>",
		"<text_tag color='neutral'>/model clear</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticePreservesAngleBracketsInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "核心证据很简单：`section -> entry -> button`，占位符：`/model <模型> <推理强度>`，比较：`a < b > c`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&gt;") || strings.Contains(ops[0].CardBody, "&lt;") {
		t.Fatalf("expected inline code to preserve angle brackets, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>section -> entry -> button</text_tag>",
		"<text_tag color='neutral'>/model <模型> <推理强度></text_tag>",
		"<text_tag color='neutral'>a < b > c</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticePreservesQuotesInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "请求层把 `\"/api/admin/*\"` 和 `\"/api/setup/*\"` 统一转成本地路径。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&#34;") || strings.Contains(ops[0].CardBody, "&#39;") {
		t.Fatalf("expected inline code to preserve quotes, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		`<text_tag color='neutral'>"/api/admin/*"</text_tag>`,
		`<text_tag color='neutral'>"/api/setup/*"</text_tag>`,
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticePreservesAmpersandsInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "请先跑 `go test ./internal/app/daemon ./internal/core/orchestrator`，再执行 `cd web && npm test -- --run src/lib/api.test.ts`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&amp;&amp;") {
		t.Fatalf("expected inline code to preserve &&, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>go test ./internal/app/daemon ./internal/core/orchestrator</text_tag>",
		"<text_tag color='neutral'>cd web && npm test -- --run src/lib/api.test.ts</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticeKeepsLiteralEntitiesEscapedInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "如果你就是要展示实体字面量，请写成 `&lt;text_tag&gt;`，不要把它当成真实标签。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "<text_tag color='neutral'>&amp;lt;text_tag&amp;gt;</text_tag>") {
		t.Fatalf("expected literal entity form to stay escaped, got %#v", ops[0].CardBody)
	}
}

func TestProjectFinalAssistantBlockPreservesAngleBracketsInInlineCode(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-inline-angle",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "请运行 `/model <模型> <推理强度>`，再检查 `a < b > c`。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&gt;") || strings.Contains(ops[0].CardBody, "&lt;") {
		t.Fatalf("expected final inline code to preserve angle brackets, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>/model <模型> <推理强度></text_tag>",
		"<text_tag color='neutral'>a < b > c</text_tag>",
	) {
		t.Fatalf("unexpected final inline-tag body: %#v", ops[0].CardBody)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func cardElementButtons(t *testing.T, element map[string]any) []map[string]any {
	t.Helper()
	switch element["tag"] {
	case "button":
		return []map[string]any{element}
	case "column_set":
		columns, _ := element["columns"].([]map[string]any)
		buttons := make([]map[string]any, 0, len(columns))
		for _, column := range columns {
			elements, _ := column["elements"].([]map[string]any)
			if len(elements) == 0 {
				continue
			}
			buttons = append(buttons, elements[0])
		}
		if len(buttons) == 0 {
			t.Fatalf("expected buttons inside column_set, got %#v", element)
		}
		return buttons
	default:
		t.Fatalf("expected button or column_set, got %#v", element)
		return nil
	}
}

func assertNoLegacyCardModelMarkers(t *testing.T, values []map[string]any) {
	t.Helper()
	for _, value := range values {
		assertNoLegacyCardModelMarkersAny(t, value)
	}
}

func assertNoLegacyCardModelMarkersAny(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		if tag, _ := typed["tag"].(string); tag == "action" {
			t.Fatalf("unexpected legacy action container in production card model: %#v", typed)
		}
		if actionType, ok := typed["action_type"]; ok && actionType != nil {
			t.Fatalf("unexpected legacy action_type in production card model: %#v", typed)
		}
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	case []map[string]any:
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	case []any:
		for _, child := range typed {
			assertNoLegacyCardModelMarkersAny(t, child)
		}
	}
}

func cardButtonLabel(t *testing.T, button map[string]any) string {
	t.Helper()
	textValue, _ := button["text"].(map[string]any)
	label, _ := textValue["content"].(string)
	if label == "" {
		t.Fatalf("expected button label, got %#v", button)
	}
	return label
}

func cardButtonPayload(t *testing.T, button map[string]any) map[string]any {
	t.Helper()
	if value, _ := button["value"].(map[string]any); len(value) != 0 {
		return value
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected callback payload on button, got %#v", button)
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	if len(value) == 0 {
		t.Fatalf("expected callback value payload, got %#v", button)
	}
	return value
}

func renderedV2BodyElements(t *testing.T, operation Operation) []map[string]any {
	t.Helper()
	if operation.cardEnvelope != cardEnvelopeV2 || operation.card == nil {
		t.Fatalf("expected structured V2 operation, got %#v", operation)
	}
	payload := renderOperationCard(operation, operation.ordinaryCardEnvelope())
	if payload["schema"] != "2.0" {
		t.Fatalf("expected V2 schema, got %#v", payload)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	return elements
}

func containsRenderedTag(elements []map[string]any, tag string) bool {
	for _, element := range elements {
		if element["tag"] == tag {
			return true
		}
	}
	return false
}

func renderedButtonCallbackValue(t *testing.T, button map[string]any) map[string]any {
	t.Helper()
	if button["tag"] != "button" {
		t.Fatalf("expected rendered V2 button, got %#v", button)
	}
	if button["value"] != nil {
		t.Fatalf("expected rendered V2 button to move callback payload into behaviors, got %#v", button)
	}
	behaviors, _ := button["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected one callback behavior, got %#v", button)
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	return value
}

func renderedColumnButtons(t *testing.T, element map[string]any) []map[string]any {
	t.Helper()
	if element["tag"] != "column_set" {
		t.Fatalf("expected rendered V2 column_set, got %#v", element)
	}
	columns, _ := element["columns"].([]map[string]any)
	buttons := make([]map[string]any, 0, len(columns))
	for _, column := range columns {
		elements, _ := column["elements"].([]map[string]any)
		if len(elements) != 1 || elements[0]["tag"] != "button" {
			t.Fatalf("expected one button per V2 column, got %#v", column)
		}
		buttons = append(buttons, elements[0])
	}
	return buttons
}

func TestProjectImageOutputAsImageMessage(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventImageOutput,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ImageOutput: &control.ImageOutput{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			ItemID:    "img-1",
			SavedPath: "/tmp/generated.png",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendImage {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].ReplyToMessageID != "om-source-1" {
		t.Fatalf("expected image output to reply to source message, got %#v", ops[0])
	}
	if ops[0].ImagePath != "/tmp/generated.png" || ops[0].ImageBase64 != "" {
		t.Fatalf("unexpected image output operation payload: %#v", ops[0])
	}
}
