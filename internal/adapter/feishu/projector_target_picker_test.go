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
	var sawWorkspace, sawSession, sawCancel, sawConfirm bool
	for _, action := range actions {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerSelectWorkspace:
			sawWorkspace = true
		case cardActionKindTargetPickerSelectSession:
			sawSession = true
		case cardActionKindTargetPickerCancel:
			sawCancel = true
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawWorkspace || !sawSession || !sawCancel || !sawConfirm {
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

func TestTargetPickerElementsRenderModeSwitchAndSourceSelect(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:          "picker-1",
		Title:             "选择工作区与会话",
		SelectedMode:      control.FeishuTargetPickerModeAddWorkspace,
		SelectedSource:    control.FeishuTargetPickerSourceGitURL,
		ShowModeSwitch:    true,
		ShowSourceSelect:  true,
		SourcePlaceholder: "选择工作区来源",
		ConfirmLabel:      "填写 Git URL",
		CanConfirm:        false,
		ModeOptions: []control.FeishuTargetPickerModeOption{
			{Value: control.FeishuTargetPickerModeExistingWorkspace, Label: "已有工作区"},
			{Value: control.FeishuTargetPickerModeAddWorkspace, Label: "添加工作区", Selected: true},
		},
		SourceOptions: []control.FeishuTargetPickerSourceOption{
			{Value: control.FeishuTargetPickerSourceLocalDirectory, Label: "本地目录", MetaText: "选择已经存在的目录", Available: true},
			{Value: control.FeishuTargetPickerSourceGitURL, Label: "Git URL", MetaText: "需要本机已安装 git 后才能使用", Available: false, UnavailableReason: "当前机器未检测到 git"},
		},
		AddModeSummary:        "完成后会进入新会话待命",
		SourceUnavailableHint: "当前机器未检测到 `git`",
	}, "life-3")

	var sawSourceSelect bool
	var sawModeButton bool
	for _, action := range cardActionsFromElements(elements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerSelectMode:
			sawModeButton = true
		case cardActionKindTargetPickerSelectSource:
			sawSourceSelect = true
		}
	}
	if !sawModeButton || !sawSourceSelect {
		t.Fatalf("expected mode button callbacks and source select callback, got %#v", elements)
	}
}

func TestTargetPickerElementsRenderLocalDirectoryOpenPathAction(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:         "picker-1",
		Title:            "选择工作区与会话",
		SelectedMode:     control.FeishuTargetPickerModeAddWorkspace,
		SelectedSource:   control.FeishuTargetPickerSourceLocalDirectory,
		ShowModeSwitch:   true,
		ShowSourceSelect: true,
		ConfirmLabel:     "接入并继续",
		CanConfirm:       false,
		ModeOptions: []control.FeishuTargetPickerModeOption{
			{Value: control.FeishuTargetPickerModeExistingWorkspace, Label: "已有工作区"},
			{Value: control.FeishuTargetPickerModeAddWorkspace, Label: "添加工作区", Selected: true},
		},
		SourceOptions: []control.FeishuTargetPickerSourceOption{
			{Value: control.FeishuTargetPickerSourceLocalDirectory, Label: "本地目录", Available: true},
			{Value: control.FeishuTargetPickerSourceGitURL, Label: "Git URL", Available: true},
		},
	}, "life-4")

	var sawOpenPath bool
	for _, action := range cardActionsFromElements(elements) {
		if cardValueMap(action)[cardActionPayloadKeyKind] != cardActionKindTargetPickerOpenPathPicker {
			continue
		}
		if cardValueMap(action)[cardActionPayloadKeyTargetValue] != control.FeishuTargetPickerPathFieldLocalDirectory {
			t.Fatalf("unexpected local-directory open-path payload: %#v", cardValueMap(action))
		}
		sawOpenPath = true
	}
	if !sawOpenPath {
		t.Fatalf("expected local-directory branch to render open-path action, got %#v", elements)
	}
}

func TestTargetPickerElementsRenderGitFormWithOpenPathAndSubmit(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:         "picker-1",
		Title:            "选择工作区与会话",
		SelectedMode:     control.FeishuTargetPickerModeAddWorkspace,
		SelectedSource:   control.FeishuTargetPickerSourceGitURL,
		ShowModeSwitch:   true,
		ShowSourceSelect: true,
		ConfirmLabel:     "克隆并继续",
		CanConfirm:       true,
		GitParentDir:     "/data/dl",
		GitRepoURL:       "https://github.com/kxn/codex-remote-feishu.git",
		GitDirectoryName: "crf",
		GitFinalPath:     "/data/dl/crf",
		ModeOptions: []control.FeishuTargetPickerModeOption{
			{Value: control.FeishuTargetPickerModeExistingWorkspace, Label: "已有工作区"},
			{Value: control.FeishuTargetPickerModeAddWorkspace, Label: "添加工作区", Selected: true},
		},
		SourceOptions: []control.FeishuTargetPickerSourceOption{
			{Value: control.FeishuTargetPickerSourceLocalDirectory, Label: "本地目录", Available: true},
			{Value: control.FeishuTargetPickerSourceGitURL, Label: "Git URL", Available: true},
		},
	}, "life-5")

	var form map[string]any
	for _, element := range elements {
		if cardStringValue(element["tag"]) == "form" {
			form = element
			break
		}
	}
	if form == nil {
		t.Fatalf("expected git branch to render form, got %#v", elements)
	}
	formElements, _ := form["elements"].([]map[string]any)
	if len(formElements) < 3 {
		t.Fatalf("expected git form inputs and actions, got %#v", form)
	}
	if formElements[0]["name"] != control.FeishuTargetPickerGitRepoURLFieldName || formElements[1]["name"] != control.FeishuTargetPickerGitDirectoryNameFieldName {
		t.Fatalf("unexpected git form input names: %#v", formElements)
	}

	var sawOpenPath bool
	var sawCancel bool
	var sawConfirm bool
	for _, action := range cardActionsFromElements(formElements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerOpenPathPicker:
			if cardValueMap(action)[cardActionPayloadKeyTargetValue] == control.FeishuTargetPickerPathFieldGitParentDir {
				sawOpenPath = true
			}
		case cardActionKindTargetPickerCancel:
			sawCancel = true
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawOpenPath || !sawCancel || !sawConfirm {
		t.Fatalf("expected git form to render open-path and confirm actions, got %#v", elements)
	}
}
