package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectTargetPickerStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := control.UIEvent{
		Kind:              control.UIEventFeishuTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		FeishuTargetPickerView: &control.FeishuTargetPickerView{
			PickerID:             "picker-1",
			Title:                "选择工作区与会话",
			WorkspacePlaceholder: "选择工作区",
			SessionPlaceholder:   "选择会话",
			SelectedWorkspaceKey: "/data/dl/web",
			SelectedSessionValue: "thread:thread-2",
			ConfirmLabel:         "使用会话",
			CanConfirm:           true,
			WorkspaceOptions: []control.FeishuTargetPickerWorkspaceOption{
				{Value: "/data/dl/web", Label: "web", MetaText: "刚刚"},
			},
			SessionOptions: []control.FeishuTargetPickerSessionOption{
				{Value: "thread:thread-2", Kind: control.FeishuTargetPickerSessionThread, Label: "web · 整理样式", MetaText: "刚刚"},
				{Value: "new_thread", Kind: control.FeishuTargetPickerSessionNewThread, Label: "新建会话", MetaText: "在这个工作区里开始一个新的会话"},
			},
		},
	}
	ops := projector.Project("chat-1", event)
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one card op, got %#v", ops)
	}
	actions := cardActionsFromElements(ops[0].CardElements)
	if len(actions) == 0 {
		t.Fatalf("expected stamped target picker actions, got %#v", ops[0].CardElements)
	}
	for _, action := range actions {
		value := cardValueMap(action)
		if value[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
			t.Fatalf("expected daemon lifecycle on target picker action, got %#v", value)
		}
	}
}

func TestTargetPickerElementsUseSelectCallbacksAndConfirm(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:             "picker-1",
		Title:                "选择工作区与会话",
		WorkspacePlaceholder: "选择工作区",
		SessionPlaceholder:   "选择会话",
		SelectedWorkspaceKey: "/data/dl/web",
		SelectedSessionValue: "thread:thread-2",
		ConfirmLabel:         "使用会话",
		CanConfirm:           true,
		WorkspaceOptions: []control.FeishuTargetPickerWorkspaceOption{
			{Value: "/data/dl/web", Label: "web", MetaText: "刚刚"},
		},
		SessionOptions: []control.FeishuTargetPickerSessionOption{
			{Value: "thread:thread-2", Kind: control.FeishuTargetPickerSessionThread, Label: "web · 整理样式", MetaText: "刚刚"},
			{Value: "new_thread", Kind: control.FeishuTargetPickerSessionNewThread, Label: "新建会话", MetaText: "在这个工作区里开始一个新的会话"},
		},
	}, "life-2")
	selectCount := 0
	for _, element := range elements {
		if element["tag"] == "select_static" {
			selectCount++
		}
	}
	if selectCount != 2 {
		t.Fatalf("expected target picker to render two selects, got %#v", elements)
	}
	actions := cardActionsFromElements(elements)
	var sawWorkspace, sawSession, sawConfirm bool
	for _, action := range actions {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerSelectWorkspace:
			sawWorkspace = true
		case cardActionKindTargetPickerSelectSession:
			sawSession = true
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawWorkspace || !sawSession || !sawConfirm {
		t.Fatalf("expected target picker payload kinds, got %#v", actions)
	}
}

func TestTargetPickerElementsKeepSessionPlaceholderWhenSelectionIsEmpty(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:             "picker-1",
		Title:                "选择工作区与会话",
		WorkspacePlaceholder: "选择工作区",
		SessionPlaceholder:   "选择会话",
		SelectedWorkspaceKey: "/data/dl/web",
		ConfirmLabel:         "使用会话",
		CanConfirm:           false,
		WorkspaceOptions: []control.FeishuTargetPickerWorkspaceOption{
			{Value: "/data/dl/web", Label: "web", MetaText: "刚刚"},
		},
		SessionOptions: []control.FeishuTargetPickerSessionOption{
			{Value: "thread:thread-2", Kind: control.FeishuTargetPickerSessionThread, Label: "web · 整理样式", MetaText: "刚刚"},
			{Value: "new_thread", Kind: control.FeishuTargetPickerSessionNewThread, Label: "新建会话", MetaText: "在这个工作区里开始一个新的会话"},
		},
	}, "life-2")

	var sessionSelect map[string]any
	for _, element := range elements {
		if cardStringValue(element["tag"]) == "select_static" && element["name"] == cardTargetPickerSessionFieldName {
			sessionSelect = element
		}
	}
	if sessionSelect == nil {
		t.Fatalf("expected session select element, got %#v", elements)
	}
	if _, ok := sessionSelect["initial_option"]; ok {
		t.Fatalf("expected empty session selection to use placeholder, got %#v", sessionSelect)
	}
	var sawDisabledConfirm bool
	for _, action := range cardActionsFromElements(elements) {
		if cardValueMap(action)[cardActionPayloadKeyKind] == cardActionKindTargetPickerConfirm && action["disabled"] == true {
			sawDisabledConfirm = true
		}
	}
	if !sawDisabledConfirm {
		t.Fatalf("expected confirm button to stay disabled, got %#v", elements)
	}
}
