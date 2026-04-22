package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectSelectionPromptAsCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Kind:         control.SelectionPromptAttachInstance,
		ContextTitle: "当前实例",
		ContextText:  "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list",
		Options: []control.SelectionOption{
			{Index: 1, OptionID: "inst-2", Label: "web", MetaText: "2分前 · 当前焦点可跟随", ButtonLabel: "切换"},
			{Index: 2, OptionID: "inst-3", Label: "ops", MetaText: "1小时前 · 当前被其他飞书会话接管", Disabled: true},
		},
	}))
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
	if plainTextContent(ops[0].CardElements[1]) != "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list" {
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
	if plainTextContent(ops[0].CardElements[4]) != "2分前 · 当前焦点可跟随" {
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
	if plainTextContent(ops[0].CardElements[7]) != "1小时前 · 当前被其他飞书会话接管" {
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
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Kind:         control.SelectionPromptAttachWorkspace,
		ContextTitle: "当前工作区",
		ContextText:  "droid · 5分前\n同工作区内继续工作可 /use，或直接发送文本（也可 /new）",
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
	}))
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
	if plainTextContent(ops[0].CardElements[1]) != "droid · 5分前\n同工作区内继续工作可 /use，或直接发送文本（也可 /new）" {
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
	if plainTextContent(ops[0].CardElements[4]) != "2分前 · 有 VS Code 活动" {
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
	if plainTextContent(ops[0].CardElements[7]) != "1小时前 · 当前被其他飞书会话接管" {
		t.Fatalf("unexpected unavailable meta: %#v", ops[0].CardElements[7])
	}
}

func TestProjectSessionSelectionPromptUsesButtonFirstLayout(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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
	if plainTextContent(ops[0].CardElements[2]) != "/data/dl/droid\n可接管" {
		t.Fatalf("unexpected option detail element: %#v", ops[0].CardElements[2])
	}
}

func TestProjectWorkspaceSelectionPromptPreservesShowWorkspaceThreadsAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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
	event := selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Kind: control.SelectionPromptUseThread,
		Options: []control.SelectionOption{
			{Index: 1, OptionID: "thread-1", Label: "修复登录流程", ButtonLabel: "修复登录流程"},
		},
	})
	event.DaemonLifecycleID = "life-1"
	ops := projector.Project("chat-1", event)
	actionRow := cardElementButtons(t, ops[0].CardElements[1])
	value := cardButtonPayload(t, actionRow[0])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected selection prompt action to carry daemon lifecycle id, got %#v", value)
	}
}

func TestProjectUseAllSelectionPromptGroupsByWorkspace(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	wantHeaders := []string{"**当前会话**", "**当前工作区**", "web · 2分前", "ops · 1小时前"}
	for _, header := range wantHeaders {
		if !containsCardTextExact(ops[0].CardElements, header) {
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
		if content := cardTextContent(element); content != "" {
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

func TestProjectUseAllSelectionViewGroupsByWorkspace(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread: &control.FeishuThreadSelectionView{
			Mode:        control.FeishuThreadSelectionNormalGlobalAll,
			RecentLimit: 5,
			CurrentWorkspace: &control.FeishuThreadSelectionWorkspaceContext{
				WorkspaceKey:   "/data/dl/droid",
				WorkspaceLabel: "droid",
				AgeText:        "5分前",
			},
			Entries: []control.FeishuThreadSelectionEntry{
				{
					ThreadID:       "thread-1",
					Summary:        "当前会话",
					WorkspaceKey:   "/data/dl/droid",
					WorkspaceLabel: "droid",
					Status:         "已接管",
					Current:        true,
				},
				{
					ThreadID:            "thread-2",
					Summary:             "别的会话",
					WorkspaceKey:        "/data/dl/web",
					WorkspaceLabel:      "web",
					AgeText:             "2分前",
					AllowCrossWorkspace: true,
				},
				{
					ThreadID:            "thread-3",
					Summary:             "另一个会话",
					WorkspaceKey:        "/data/dl/web",
					WorkspaceLabel:      "web",
					AgeText:             "2分前",
					Status:              "VS Code 占用中",
					AllowCrossWorkspace: true,
				},
				{
					ThreadID:            "thread-4",
					Summary:             "不可接管会话",
					WorkspaceKey:        "/data/dl/ops",
					WorkspaceLabel:      "ops",
					AgeText:             "1小时前",
					Status:              "当前被其他飞书会话接管，暂不可接管",
					Disabled:            true,
					AllowCrossWorkspace: true,
				},
			},
		},
	}
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:                control.UIEventFeishuSelectionView,
		FeishuSelectionView: &view,
		FeishuSelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:   control.FeishuUIDTOwnerSelection,
			PromptKind: control.SelectionPromptUseThread,
			Layout:     "workspace_grouped_useall",
			Title:      "全部会话",
			ViewMode:   string(control.FeishuThreadSelectionNormalGlobalAll),
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "全部会话" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if !containsRenderedTag(ops[0].CardElements, "markdown") {
		t.Fatalf("expected selection view to render structured card elements, got %#v", ops[0].CardElements)
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
	for _, want := range []string{
		"当前 · 当前会话",
		"查看当前工作区全部会话",
		"接管 · 别的会话",
		"接管 · 另一个会话",
	} {
		if !containsString(buttonLabels, want) {
			t.Fatalf("expected view-projected grouped button %q, got %#v", want, buttonLabels)
		}
	}
	var rendered []string
	for _, element := range ops[0].CardElements {
		if content := cardTextContent(element); content != "" {
			rendered = append(rendered, content)
		}
	}
	for _, fragment := range []string{
		"**当前工作区**",
		"droid · 5分前\n同工作区内切换请直接用 /use",
		"web · 2分前",
	} {
		if !containsString(rendered, fragment) {
			t.Fatalf("expected view-projected grouped content to include %q, got %#v", fragment, rendered)
		}
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	if !containsRenderedTag(renderedElements, "button") {
		t.Fatalf("expected rendered V2 view projection to keep interactive buttons, got %#v", renderedElements)
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
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Layout:       "workspace_grouped_useall",
		Kind:         control.SelectionPromptUseThread,
		Title:        "全部会话",
		ContextTitle: "当前工作区",
		ContextText:  "droid · 5分前\n同工作区内切换请直接用 /use",
		ContextKey:   "/data/dl/droid",
		Options:      options,
	}))
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
	if strings.Join(buttonLabels, " | ") != "当前 · 当前会话 | 查看当前工作区全部会话 | 接管 · web-1 | 接管 · web-2 | 展开 web" {
		t.Fatalf("unexpected grouped/limited button labels: %#v", buttonLabels)
	}
}

func TestProjectUseAllSelectionPromptRendersWorkspaceGroupExpandAction(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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

func TestProjectUseAllExpandedSelectionPromptUsesWorkspaceIndexAndFitsInlineCallback(t *testing.T) {
	projector := NewProjector()
	options := []control.SelectionOption{
		{
			Index:       1,
			OptionID:    "thread-current",
			Label:       "当前会话",
			ButtonLabel: "当前会话",
			GroupKey:    "/data/dl/current",
			GroupLabel:  "current",
			MetaText:    "已接管",
			IsCurrent:   true,
		},
	}
	index := 2
	for workspace := 1; workspace <= 40; workspace++ {
		workspaceKey := fmt.Sprintf("/data/dl/projects/team-alpha/service-%02d", workspace)
		workspaceLabel := fmt.Sprintf("service-%02d", workspace)
		for thread := 1; thread <= 3; thread++ {
			options = append(options, control.SelectionOption{
				Index:       index,
				OptionID:    fmt.Sprintf("thread-%02d-%d", workspace, thread),
				Label:       fmt.Sprintf("%s-thread-%d", workspaceLabel, thread),
				ButtonLabel: fmt.Sprintf("%s-thread-%d", workspaceLabel, thread),
				GroupKey:    workspaceKey,
				GroupLabel:  workspaceLabel,
				AgeText:     fmt.Sprintf("%d分前", workspace),
				MetaText:    fmt.Sprintf("%d分前", thread),
			})
			index++
		}
	}
	options = append(options,
		control.SelectionOption{
			Index:       index,
			OptionID:    "thread-ops-disabled",
			Label:       "ops-thread",
			ButtonLabel: "ops-thread",
			GroupKey:    "/data/dl/projects/team-alpha/ops",
			GroupLabel:  "ops",
			AgeText:     "2小时前",
			MetaText:    "当前被其他飞书会话接管，暂不可接管",
			Disabled:    true,
		},
		control.SelectionOption{
			Index:       index + 1,
			Label:       "最近工作区",
			ButtonLabel: "最近工作区",
			ActionKind:  "show_recent_thread_workspaces",
		},
	)
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Layout:       "workspace_grouped_useall",
		Kind:         control.SelectionPromptUseThread,
		Title:        "全部会话",
		ContextTitle: "当前工作区",
		ContextText:  "current · 1分前\n同工作区内切换请直接用 /use",
		ContextKey:   "/data/dl/current",
		Options:      options,
	}))
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
	for _, want := range []string{
		"当前 · 当前会话",
		"查看当前工作区全部会话",
		"查看全部 · service-01 (3)",
		"查看全部 · service-40 (3)",
		"不可恢复 · ops",
		"返回 · 最近工作区",
	} {
		if !containsString(buttonLabels, want) {
			t.Fatalf("expected workspace index button %q, got %#v", want, buttonLabels)
		}
	}
	for _, unexpected := range []string{
		"接管 · service-01-thread-1",
		"接管 · service-40-thread-3",
	} {
		if containsString(buttonLabels, unexpected) {
			t.Fatalf("did not expect per-thread button %q in expanded workspace index: %#v", unexpected, buttonLabels)
		}
	}
	response := callbackCardResponse(&ActionResult{ReplaceCurrentCard: &ops[0]})
	if response == nil {
		t.Fatalf("expected inline callback response for expanded workspace index")
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal inline callback response: %v", err)
	}
	if len(payload) >= 20000 {
		t.Fatalf("expected inline callback response under 20K, got %d bytes", len(payload))
	}
}

func TestProjectVSCodeRecentSelectionPromptShowsInstanceSummaryAndMore(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
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
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	var rendered []string
	for _, element := range ops[0].CardElements {
		if content := cardTextContent(element); content != "" {
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
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuCommandPageView{
		Title:           "Slash 命令帮助",
		SummarySections: summarySections("当前支持的 slash command 如下。"),
		Sections: []control.CommandCatalogSection{{
			Title: "帮助",
			Entries: []control.CommandCatalogEntry{{
				Commands:    []string{"/help", "menu"},
				Description: "查看帮助或再次打开命令菜单。",
				Examples:    []string{"/menu"},
			}},
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "Slash 命令帮助" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardBody != "" {
		t.Fatalf("unexpected card body: %#v", ops[0])
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("expected summary, section header, and entry text, got %#v", ops[0].CardElements)
	}
	if !containsCardTextExact(ops[0].CardElements, "当前支持的 slash command 如下。") {
		t.Fatalf("expected summary plain text block, got %#v", ops[0].CardElements)
	}
	if !containsMarkdownExact(ops[0].CardElements, "**帮助**") {
		t.Fatalf("unexpected section element: %#v", ops[0].CardElements)
	}
	want := "命令：/help / menu\n查看帮助或再次打开命令菜单。\n例如：/menu"
	if !containsCardTextExact(ops[0].CardElements, want) {
		t.Fatalf("unexpected entry text: %#v", ops[0].CardElements)
	}
}

func TestProjectCommandHelpCatalogPreservesAmpersandsInPlainText(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuCommandPageView{
		Title:           "命令帮助",
		SummarySections: summarySections("常用联调命令。"),
		Sections: []control.CommandCatalogSection{{
			Title: "联调",
			Entries: []control.CommandCatalogEntry{{
				Commands:    []string{"go test ./internal/app/daemon ./internal/core/orchestrator"},
				Description: "先跑 daemon/orchestrator。",
				Examples:    []string{"cd web && npm test -- --run src/lib/api.test.ts"},
			}},
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := []string{}
	for _, element := range ops[0].CardElements {
		if text := cardTextContent(element); text != "" {
			rendered = append(rendered, text)
		}
	}
	content := strings.Join(rendered, "\n")
	if strings.Contains(content, "&amp;&amp;") {
		t.Fatalf("expected plain-text command catalog to preserve &&, got %q", content)
	}
	if !containsAll(content,
		"命令：go test ./internal/app/daemon ./internal/core/orchestrator",
		"例如：cd web && npm test -- --run src/lib/api.test.ts",
	) {
		t.Fatalf("unexpected command catalog text: %q", content)
	}
}

func TestProjectBuiltinCommandHelpCatalogPreservesPlaceholdersAndHidesKillInstance(t *testing.T) {
	projector := NewProjector()
	catalog := control.FeishuCommandHelpPageView()
	ops := projector.Project("chat-1", commandCatalogEvent(catalog))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected builtin help summary to stay out of markdown body, got %#v", ops[0])
	}
	var rendered []string
	for _, element := range ops[0].CardElements {
		if content := cardTextContent(element); content != "" {
			rendered = append(rendered, content)
		}
	}
	body := strings.Join(rendered, "\n")
	if strings.Contains(body, "/killinstance") {
		t.Fatalf("expected builtin help catalog to hide /killinstance, got %q", body)
	}
	if strings.Contains(body, "&lt;") || strings.Contains(body, "&gt;") {
		t.Fatalf("expected command placeholders to preserve angle brackets, got %q", body)
	}
	if strings.Contains(body, "<text_tag") {
		t.Fatalf("expected builtin help catalog to avoid legacy markdown tags, got %q", body)
	}
	if !containsAll(body,
		"以下是当前主展示的 canonical slash command。历史 alias 仍可兼容，但不再作为新的主展示入口。",
		"命令：/model",
		"命令：/reasoning",
		"命令：/access",
		"命令：/use",
		"命令：/menu",
	) {
		t.Fatalf("unexpected builtin help catalog body: %q", body)
	}
}

func TestProjectInteractiveCommandCatalogAddsRunCommandButtons(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuCommandPageView{
		Title:           "命令菜单",
		SummarySections: summarySections("固定动作可直接点击。"),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayDefault,
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected summary + section + entry + action row, got %#v", ops[0].CardElements)
	}
	if !containsCardTextExact(ops[0].CardElements, "固定动作可直接点击。") {
		t.Fatalf("expected summary plain text block, got %#v", ops[0].CardElements)
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[3])
	if len(actionRow) != 1 {
		t.Fatalf("expected one action button, got %#v", ops[0].CardElements[3])
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
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuCommandPageView{
		Title:           "推理强度",
		SummarySections: summarySections("当前：`high`；飞书覆盖：`high`。"),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayCompactButtons,
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected summary + section + two stacked action rows, got %#v", ops[0].CardElements)
	}
	if !containsCardTextExact(ops[0].CardElements, "当前：`high`；飞书覆盖：`high`。") {
		t.Fatalf("expected summary plain text block, got %#v", ops[0].CardElements)
	}
	if ops[0].CardElements[1]["content"] != "**立即应用**" {
		t.Fatalf("unexpected section element: %#v", ops[0].CardElements[1])
	}
	rendered := []string{}
	for _, element := range ops[0].CardElements {
		if text := cardTextContent(element); text != "" {
			rendered = append(rendered, text)
		}
	}
	if strings.Contains(strings.Join(rendered, "\n"), "这段说明在紧凑布局里不应该出现。") {
		t.Fatalf("compact layout should not render entry text, got %#v", ops[0].CardElements)
	}
	firstRow := cardElementButtons(t, ops[0].CardElements[2])
	secondRow := cardElementButtons(t, ops[0].CardElements[3])
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

func TestCommandCatalogFromViewBuildsDetachedMenuHome(t *testing.T) {
	catalog, ok := control.FeishuCommandPageViewFromView(control.FeishuCommandView{
		Menu: &control.FeishuCommandMenuView{Stage: "detached"},
	}, "", "detached")
	if !ok {
		t.Fatalf("expected menu view to project into command page")
	}
	if catalog.Title != "命令菜单" || !catalog.Interactive {
		t.Fatalf("unexpected menu catalog: %#v", catalog)
	}
	if len(catalog.Sections) != 1 || catalog.Sections[0].Title != "全部分组" {
		t.Fatalf("unexpected menu sections: %#v", catalog.Sections)
	}
	if got := firstCommandTexts(catalog.Sections[0].Entries); len(got) != 0 {
		t.Fatalf("detached menu home should only expose group navigation entries, got %#v", got)
	}
}

func TestCommandCatalogFromViewCurrentWorkHonorsStageVisibility(t *testing.T) {
	normalCatalog, ok := control.FeishuCommandPageViewFromView(control.FeishuCommandView{
		Menu: &control.FeishuCommandMenuView{Stage: "normal_working", GroupID: "current_work"},
	}, "", "")
	if !ok {
		t.Fatalf("expected normal current_work menu to project")
	}
	gotNormal := firstCommandTexts(normalCatalog.Sections[0].Entries)
	wantNormal := []string{"/stop", "/compact", "/steerall", "/new", "/history", "/sendfile"}
	if fmt.Sprint(gotNormal) != fmt.Sprint(wantNormal) {
		t.Fatalf("normal current_work commands = %#v, want %#v", gotNormal, wantNormal)
	}

	vscodeCatalog, ok := control.FeishuCommandPageViewFromView(control.FeishuCommandView{
		Menu: &control.FeishuCommandMenuView{Stage: "vscode_working", GroupID: "current_work"},
	}, "", "")
	if !ok {
		t.Fatalf("expected vscode current_work menu to project")
	}
	gotVSCode := firstCommandTexts(vscodeCatalog.Sections[0].Entries)
	wantVSCode := []string{"/stop", "/compact", "/steerall", "/history", "/sendfile"}
	if fmt.Sprint(gotVSCode) != fmt.Sprint(wantVSCode) {
		t.Fatalf("vscode current_work commands = %#v, want %#v", gotVSCode, wantVSCode)
	}
}

func firstCommandTexts(entries []control.CommandCatalogEntry) []string {
	commands := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Commands) == 0 {
			continue
		}
		commands = append(commands, entry.Commands[0])
	}
	return commands
}

func TestProjectCommandFormStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := commandCatalogEvent(control.FeishuCommandPageView{
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
	})
	event.DaemonLifecycleID = "life-1"
	ops := projector.Project("chat-1", event)
	formContainer := ops[0].CardElements[0]
	formElements, _ := formContainer["elements"].([]map[string]any)
	value := cardButtonPayload(t, formElements[1])
	if value["daemon_lifecycle_id"] != "life-1" {
		t.Fatalf("expected form action to carry daemon lifecycle id, got %#v", value)
	}
}

func TestProjectInteractiveCommandCatalogStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := commandCatalogEvent(control.FeishuCommandPageView{
		Interactive: true,
		Sections: []control.CommandCatalogSection{{
			Entries: []control.CommandCatalogEntry{{
				Buttons: []control.CommandCatalogButton{{Label: "查看实例", CommandText: "/list"}},
			}},
		}},
	})
	event.DaemonLifecycleID = "life-1"
	ops := projector.Project("chat-1", event)
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
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuCommandPageView{
		Title:           "发送设置",
		SummarySections: summarySections("请选择操作。"),
		Interactive:     true,
		DisplayStyle:    control.CommandCatalogDisplayDefault,
		RelatedButtons: []control.CommandCatalogButton{{
			Label:       "返回菜单",
			CommandText: "/menu",
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected related-buttons catalog without form to use V2, got %#v", ops[0])
	}
	renderedElements := renderedV2BodyElements(t, ops[0])
	if len(renderedElements) != 3 {
		t.Fatalf("expected summary text plus divider and related button, got %#v", renderedElements)
	}
	if renderedElements[1]["tag"] != "hr" {
		t.Fatalf("expected divider before related button, got %#v", renderedElements)
	}
	renderedValue := renderedButtonCallbackValue(t, renderedElements[2])
	if renderedValue["kind"] != "run_command" || renderedValue["command_text"] != "/menu" {
		t.Fatalf("unexpected related button callback payload: %#v", renderedValue)
	}
}

func TestProjectRequestPromptAsCard(t *testing.T) {
	projector := NewProjector()
	event := requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-1",
		RequestType: "approval",
		Title:       "需要确认",
		ThreadID:    "thread-1",
		ThreadTitle: "droid · 修复登录流程",
		Sections: []control.FeishuCardTextSection{{
			Lines: []string{"本地 Codex 想执行：", "```text", "git push", "```"},
		}},
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许执行", Style: "primary"},
			{OptionID: "acceptForSession", Label: "本会话允许", Style: "default"},
			{OptionID: "decline", Label: "拒绝", Style: "default"},
			{OptionID: "captureFeedback", Label: "告诉 Codex 怎么改", Style: "default"},
		},
	})
	event.SourceMessageID = "om-source-1"
	ops := projector.Project("chat-1", event)
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].ReplyToMessageID != "" {
		t.Fatalf("expected request prompt to stay top-level, got %#v", ops[0])
	}
	if ops[0].CardTitle != "需要确认" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	if ops[0].CardThemeKey != cardThemeApproval {
		t.Fatalf("unexpected request prompt theme: %#v", ops[0])
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected request prompt card body to stay empty, got %#v", ops[0])
	}
	if len(ops[0].CardElements) != 4 {
		t.Fatalf("expected sections + action row + hint, got %#v", ops[0].CardElements)
	}
	if got := plainTextContent(ops[0].CardElements[0]); !containsAll(got, "当前会话：droid · 修复登录流程") {
		t.Fatalf("unexpected thread section: %#v", ops[0].CardElements[0])
	}
	if got := plainTextContent(ops[0].CardElements[1]); !containsAll(got, "本地 Codex 想执行：", "git push", "```text") {
		t.Fatalf("unexpected request section: %#v", ops[0].CardElements[1])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[2])
	if len(actionRow) != 4 {
		t.Fatalf("expected 4 request option buttons, got %#v", ops[0].CardElements[2])
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
	if got := plainTextContent(renderedElements[0]); !containsAll(got, "当前会话：droid · 修复登录流程") {
		t.Fatalf("unexpected rendered thread section: %#v", renderedElements[0])
	}
	if got := plainTextContent(renderedElements[1]); !containsAll(got, "本地 Codex 想执行：", "git push", "```text") {
		t.Fatalf("unexpected rendered request section: %#v", renderedElements[1])
	}
	renderedButtons := renderedColumnButtons(t, renderedElements[2])
	if len(renderedButtons) != 4 {
		t.Fatalf("expected rendered V2 request prompt to keep 4 buttons, got %#v", renderedElements[2])
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
	event := requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-1",
		RequestType: "approval",
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许执行", Style: "primary"},
		},
	})
	event.DaemonLifecycleID = "life-1"
	ops := projector.Project("chat-1", event)
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
	ops := projector.Project("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:       "req-ui-1",
		RequestType:     "request_user_input",
		RequestRevision: 3,
		Title:           "需要补充输入",
		ThreadTitle:     "droid · 修复登录流程",
		Sections: []control.FeishuCardTextSection{{
			Lines: []string{"本地 Codex 正在等待你补充参数或说明。"},
		}},
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected request_user_input card body to stay empty, got %#v", ops[0])
	}
	if len(ops[0].CardElements) != 9 {
		t.Fatalf("expected sections + progress + current-step elements, got %#v", ops[0].CardElements)
	}
	if got := plainTextContent(ops[0].CardElements[0]); !containsAll(got, "当前会话：droid · 修复登录流程") {
		t.Fatalf("expected thread section at top, got %#v", ops[0].CardElements[0])
	}
	if got := plainTextContent(ops[0].CardElements[1]); !containsAll(got, "本地 Codex 正在等待你补充参数或说明。") {
		t.Fatalf("expected request section after thread section, got %#v", ops[0].CardElements[1])
	}
	if got := markdownContent(ops[0].CardElements[2]); !strings.Contains(got, "回答进度") || !strings.Contains(got, "0/2") {
		t.Fatalf("expected progress markdown after sections, got %#v", ops[0].CardElements[2])
	}
	if got := markdownContent(ops[0].CardElements[3]); !strings.Contains(got, "问题 1/2") {
		t.Fatalf("expected first question heading after progress, got %#v", ops[0].CardElements[3])
	}
	if got := plainTextContent(ops[0].CardElements[4]); !containsAll(got, "标题：模型", "说明：", "请选择模型", "可选项：", "- gpt-5.4：推荐") {
		t.Fatalf("expected first question body after heading, got %#v", ops[0].CardElements[4])
	}
	actionRow := cardElementButtons(t, ops[0].CardElements[5])
	if len(actionRow) != 2 {
		t.Fatalf("expected direct response buttons for first question, got %#v", ops[0].CardElements[5])
	}
	value := cardButtonPayload(t, actionRow[0])
	requestAnswers, _ := value["request_answers"].(map[string]any)
	modelAnswers, _ := requestAnswers["model"].([]any)
	if value["kind"] != "request_respond" || len(modelAnswers) != 1 || modelAnswers[0] != "gpt-5.4" {
		t.Fatalf("unexpected request option payload: %#v", value)
	}
	if value["request_revision"] != 3 {
		t.Fatalf("expected request option to carry request revision, got %#v", value)
	}
	navRow := cardElementButtons(t, ops[0].CardElements[7])
	if len(navRow) != 2 {
		t.Fatalf("expected current-step navigation row, got %#v", ops[0].CardElements[7])
	}
	prevValue := cardButtonPayload(t, navRow[0])
	nextValue := cardButtonPayload(t, navRow[1])
	if prevValue["request_option_id"] != "step_previous" || nextValue["request_option_id"] != "step_next" {
		t.Fatalf("expected prev/next navigation payloads, got %#v / %#v", prevValue, nextValue)
	}
	submitValue := cardButtonPayload(t, ops[0].CardElements[8])
	if submitValue["kind"] != "request_respond" || submitValue["request_id"] != "req-ui-1" || submitValue["request_option_id"] != "submit" {
		t.Fatalf("unexpected request submit payload: %#v", submitValue)
	}
	if submitValue["request_revision"] != 3 {
		t.Fatalf("expected request submit action to carry request revision, got %#v", submitValue)
	}
	if ops[0].cardEnvelope != cardEnvelopeV2 || ops[0].card == nil {
		t.Fatalf("expected request_user_input prompt to use structured V2 send path, got %#v", ops[0])
	}
	assertNoLegacyCardModelMarkers(t, ops[0].CardElements)
	renderedElements := renderedV2BodyElements(t, ops[0])
	if got := plainTextContent(renderedElements[0]); !containsAll(got, "当前会话：droid · 修复登录流程") {
		t.Fatalf("expected rendered thread section at top, got %#v", renderedElements[0])
	}
	if got := plainTextContent(renderedElements[1]); !containsAll(got, "本地 Codex 正在等待你补充参数或说明。") {
		t.Fatalf("expected rendered request section after thread section, got %#v", renderedElements[1])
	}
	if got := markdownContent(renderedElements[2]); !strings.Contains(got, "回答进度") || !strings.Contains(got, "0/2") {
		t.Fatalf("expected rendered progress markdown, got %#v", renderedElements[2])
	}
	if got := markdownContent(renderedElements[3]); !strings.Contains(got, "问题 1/2") {
		t.Fatalf("expected rendered question heading after progress, got %#v", renderedElements[3])
	}
	if got := plainTextContent(renderedElements[4]); !containsAll(got, "标题：模型", "说明：", "请选择模型") {
		t.Fatalf("expected rendered question body after heading, got %#v", renderedElements[4])
	}
	renderedButtons := renderedColumnButtons(t, renderedElements[5])
	if len(renderedButtons) != 2 {
		t.Fatalf("expected rendered direct-response button row, got %#v", renderedElements[5])
	}
	renderedValue := renderedButtonCallbackValue(t, renderedButtons[0])
	renderedAnswers, _ := renderedValue["request_answers"].(map[string]any)
	renderedModelAnswers, _ := renderedAnswers["model"].([]any)
	if renderedValue["kind"] != "request_respond" || len(renderedModelAnswers) != 1 || renderedModelAnswers[0] != "gpt-5.4" {
		t.Fatalf("unexpected rendered direct-response payload: %#v", renderedValue)
	}
	if renderedValue["request_revision"] != 3 {
		t.Fatalf("expected rendered direct-response payload to carry request revision, got %#v", renderedValue)
	}
	renderedNav := renderedColumnButtons(t, renderedElements[7])
	if len(renderedNav) != 2 {
		t.Fatalf("expected rendered navigation row, got %#v", renderedElements[7])
	}
	renderedPrevValue := renderedButtonCallbackValue(t, renderedNav[0])
	renderedNextValue := renderedButtonCallbackValue(t, renderedNav[1])
	if renderedPrevValue["request_option_id"] != "step_previous" || renderedNextValue["request_option_id"] != "step_next" {
		t.Fatalf("unexpected rendered navigation payloads: %#v / %#v", renderedPrevValue, renderedNextValue)
	}
	renderedSubmitValue := renderedButtonCallbackValue(t, renderedElements[8])
	if renderedSubmitValue["kind"] != "request_respond" || renderedSubmitValue["request_id"] != "req-ui-1" || renderedSubmitValue["request_option_id"] != "submit" {
		t.Fatalf("unexpected rendered request submit payload: %#v", renderedSubmitValue)
	}
	if renderedSubmitValue["request_revision"] != 3 {
		t.Fatalf("expected rendered request submit payload to carry request revision, got %#v", renderedSubmitValue)
	}
}

func TestProjectRequestUserInputPromptRendersCurrentFormQuestionAsSingleStepForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:            "req-ui-form-1",
		RequestType:          "request_user_input",
		RequestRevision:      6,
		CurrentQuestionIndex: 1,
		Questions: []control.RequestPromptQuestion{
			{
				ID:             "model",
				Header:         "模型",
				Question:       "请选择模型",
				Answered:       true,
				DefaultValue:   "gpt-5.4",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "gpt-5.4"},
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
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := markdownContent(ops[0].CardElements[0]); !strings.Contains(got, "回答进度") || !strings.Contains(got, "1/2") || !strings.Contains(got, "当前第 2 题") {
		t.Fatalf("expected progress to expose current step, got %#v", ops[0].CardElements[0])
	}
	if got := markdownContent(ops[0].CardElements[1]); !strings.Contains(got, "问题 2/2") {
		t.Fatalf("expected second question heading, got %#v", ops[0].CardElements[1])
	}
	if got := plainTextContent(ops[0].CardElements[2]); !containsAll(got, "标题：备注", "状态：待回答", "该答案按私密输入处理") {
		t.Fatalf("expected current form question body, got %#v", ops[0].CardElements[2])
	}
	form := ops[0].CardElements[4]
	if form["tag"] != "form" {
		t.Fatalf("expected current-step form, got %#v", form)
	}
	formElements, _ := form["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected one input and one save button, got %#v", form)
	}
	if label := cardButtonLabel(t, formElements[1]); label != "保存本题" {
		t.Fatalf("expected step-save label, got %#v", formElements[1])
	}
	saveValue := cardButtonPayload(t, formElements[1])
	if saveValue["kind"] != "submit_request_form" || saveValue["request_option_id"] != "step_save" || saveValue["request_revision"] != 6 {
		t.Fatalf("unexpected step-save payload: %#v", saveValue)
	}
}

func TestProjectRequestUserInputPromptAddsSubmitActionWhenNoForm(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-ui-submit",
		RequestType: "request_user_input",
		Questions: []control.RequestPromptQuestion{
			{
				ID:             "model",
				Header:         "模型",
				Question:       "请选择模型",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "gpt-5.4"},
					{Label: "gpt-5.3"},
				},
			},
			{
				ID:             "effort",
				Header:         "推理强度",
				Question:       "请选择推理强度",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "high"},
					{Label: "medium"},
				},
			},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) < 7 {
		t.Fatalf("expected submit action row for no-form prompt, got %#v", ops[0].CardElements)
	}
	submitRow := cardElementButtons(t, ops[0].CardElements[6])
	if len(submitRow) != 1 {
		t.Fatalf("expected one submit action button, got %#v", ops[0].CardElements[6])
	}
	value := cardButtonPayload(t, submitRow[0])
	if value["kind"] != "request_respond" || value["request_option_id"] != "submit" {
		t.Fatalf("unexpected submit action payload: %#v", value)
	}
}

func TestProjectRequestUserInputPromptRendersConfirmSubmitActions(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:                          "req-ui-3",
		RequestType:                        "request_user_input",
		RequestRevision:                    4,
		SubmitWithUnansweredConfirmPending: true,
		SubmitWithUnansweredMissingLabels:  []string{"推理强度"},
		Questions: []control.RequestPromptQuestion{
			{
				ID:             "model",
				Header:         "模型",
				Question:       "请选择模型",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "gpt-5.4"},
					{Label: "gpt-5.3"},
				},
			},
			{
				ID:             "effort",
				Header:         "推理强度",
				Question:       "请选择推理强度",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "high"},
					{Label: "medium"},
				},
			},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("expected confirm markdown and confirm action row, got %#v", ops[0].CardElements)
	}
	if got := markdownContent(ops[0].CardElements[0]); !strings.Contains(got, "回答进度") || !strings.Contains(got, "0/2") {
		t.Fatalf("expected progress markdown for multi direct card, got %#v", ops[0].CardElements[0])
	}
	if confirmMarkdown := markdownContent(ops[0].CardElements[1]); !strings.Contains(confirmMarkdown, "仍有 1 个问题未回答") {
		t.Fatalf("expected confirm warning markdown, got %#v", ops[0].CardElements[1])
	}
	confirmRow := cardElementButtons(t, ops[0].CardElements[2])
	if len(confirmRow) != 2 {
		t.Fatalf("expected two confirm action buttons, got %#v", ops[0].CardElements[2])
	}
	cancelValue := cardButtonPayload(t, confirmRow[0])
	if cancelValue["kind"] != "request_respond" || cancelValue["request_option_id"] != "cancel_submit_with_unanswered" {
		t.Fatalf("unexpected cancel submit payload: %#v", cancelValue)
	}
	if cancelValue["request_revision"] != 4 {
		t.Fatalf("expected cancel action to carry request revision, got %#v", cancelValue)
	}
	confirmValue := cardButtonPayload(t, confirmRow[1])
	if confirmValue["kind"] != "request_respond" || confirmValue["request_option_id"] != "confirm_submit_with_unanswered" {
		t.Fatalf("unexpected confirm submit payload: %#v", confirmValue)
	}
	if confirmValue["request_revision"] != 4 {
		t.Fatalf("expected confirm action to carry request revision, got %#v", confirmValue)
	}
}

func TestProjectRequestUserInputPromptShowsQuestionProgressAndAnswerStatus(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-ui-4",
		RequestType: "request_user_input",
		Questions: []control.RequestPromptQuestion{
			{
				ID:             "model",
				Header:         "模型",
				Question:       "请选择模型",
				Answered:       true,
				DefaultValue:   "gpt-5.4",
				DirectResponse: true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "gpt-5.4"},
					{Label: "gpt-5.3"},
				},
			},
			{
				ID:             "notes",
				Header:         "备注",
				Question:       "补充说明",
				Answered:       false,
				DirectResponse: false,
			},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := markdownContent(ops[0].CardElements[0]); !strings.Contains(got, "回答进度") || !strings.Contains(got, "1/2") {
		t.Fatalf("expected top progress markdown, got %#v", ops[0].CardElements[0])
	}
	if got := markdownContent(ops[0].CardElements[1]); !strings.Contains(got, "问题 1/2") {
		t.Fatalf("expected first question heading after progress, got %#v", ops[0].CardElements[1])
	}
	firstQuestion := plainTextContent(ops[0].CardElements[2])
	if !strings.Contains(firstQuestion, "状态：已回答") || !strings.Contains(firstQuestion, "当前答案：gpt-5.4") {
		t.Fatalf("expected first question to include answered status and current answer, got %q", firstQuestion)
	}
	firstRow := cardElementButtons(t, ops[0].CardElements[3])
	if len(firstRow) != 2 {
		t.Fatalf("expected first question direct options row, got %#v", ops[0].CardElements[3])
	}
	firstButton := firstRow[0]
	secondButton := firstRow[1]
	if firstButton["type"] != "primary" || secondButton["type"] != "default" {
		t.Fatalf("expected selected option highlighted and others downgraded, got %#v / %#v", firstButton, secondButton)
	}
}

func TestProjectKickThreadPromptUsesCustomButtonLabels(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", selectionPromptEvent(control.FeishuDirectSelectionPrompt{
		Kind: control.SelectionPromptKickThread,
		Options: []control.SelectionOption{
			{Index: 1, OptionID: "cancel", Label: "保留当前状态，不执行强踢。", ButtonLabel: "取消"},
			{Index: 2, OptionID: "thread-1", Label: "droid · 修复登录流程", Subtitle: "/data/dl/droid\n已被其他飞书会话占用，可强踢", ButtonLabel: "强踢并占用"},
		},
	}))
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

func TestProjectTurnOwnedNoticeStaysTopLevel(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventNotice,
		SourceMessageID: "om-source-1",
		Notice: &control.Notice{
			Code: "request_refresh",
			Text: "请在最新卡片上重试。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].ReplyToMessageID != "" {
		t.Fatalf("expected turn-owned notice to stay top-level, got %#v", ops[0])
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
