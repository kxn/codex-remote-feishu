package feishu

import (
	"strings"
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

func TestProjectTargetPickerUsesUpdateCardWhenMessageIDPresent(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventFeishuTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		FeishuTargetPickerView: &control.FeishuTargetPickerView{
			PickerID:    "picker-1",
			MessageID:   "om-card-1",
			Title:       "选择工作区与会话",
			Stage:       control.FeishuTargetPickerStageProcessing,
			StatusTitle: "正在切换会话",
			StatusText:  "正在恢复目标会话。",
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one card op, got %#v", ops)
	}
	if ops[0].Kind != OperationUpdateCard || ops[0].MessageID != "om-card-1" || ops[0].ReplyToMessageID != "" {
		t.Fatalf("expected update-card op for existing target picker message, got %#v", ops[0])
	}
	if !ops[0].CardUpdateMulti {
		t.Fatalf("expected target picker update to remain multi-update capable, got %#v", ops[0])
	}
}

func TestTargetPickerProcessingStageRendersCancelOnlyForGitImport(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:              "picker-1",
		Stage:                 control.FeishuTargetPickerStageProcessing,
		StatusTitle:           "正在导入 Git 工作区",
		StatusText:            "执行中。普通输入已暂停，请等待完成或取消。",
		CanCancelProcessing:   true,
		ProcessingCancelLabel: "取消导入",
	}, "life-processing")
	var sawCancel bool
	for _, action := range cardActionsFromElements(elements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerCancel:
			sawCancel = true
		case cardActionKindTargetPickerConfirm:
			t.Fatalf("did not expect processing card to keep confirm action, got %#v", elements)
		}
	}
	if !sawCancel {
		t.Fatalf("expected processing git-import card to render cancel action, got %#v", elements)
	}
	if !containsMarkdownWithPrefix(elements, "**正在导入 Git 工作区**") {
		t.Fatalf("expected processing git-import card to render status markdown, got %#v", elements)
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

func TestTargetPickerTerminalStageSealsCardWithoutInteractiveControls(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:               "picker-1",
		Stage:                  control.FeishuTargetPickerStageSucceeded,
		StatusTitle:            "已切换会话",
		StatusText:             "当前工作目标已经切换完成。",
		SelectedWorkspaceLabel: "web",
		SelectedSessionLabel:   "整理样式",
	}, "life-terminal")
	if len(cardActionsFromElements(elements)) != 0 {
		t.Fatalf("expected terminal target picker card to remove interactive controls, got %#v", elements)
	}
	for _, element := range elements {
		tag := cardStringValue(element["tag"])
		if tag == "select_static" || tag == "button" || tag == "form" || tag == "button_group" {
			t.Fatalf("expected terminal target picker card to keep markdown-only body, got %#v", elements)
		}
	}
	if !containsMarkdownWithPrefix(elements, "**已切换会话**") {
		t.Fatalf("expected terminal target picker card to render final status text, got %#v", elements)
	}
}

func TestTargetPickerTerminalSectionsKeepDynamicValuesOutOfMarkdown(t *testing.T) {
	dynamic := "https://example.com/repo`name`.git"
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:    "picker-1",
		Stage:       control.FeishuTargetPickerStageProcessing,
		StatusTitle: "正在导入 Git 工作区",
		StatusSections: []control.FeishuCardTextSection{
			{Label: "对象", Lines: []string{dynamic, "-> /tmp/demo"}},
			{Label: "阶段", Lines: []string{"- [>] 克隆仓库"}},
		},
		StatusFooter: "执行中。普通输入已暂停，请等待完成或取消。",
	}, "life-sections")
	var sawDynamicPlainText bool
	for _, element := range elements {
		if strings.Contains(plainTextContent(element), dynamic) {
			sawDynamicPlainText = true
			break
		}
	}
	if !sawDynamicPlainText {
		t.Fatalf("expected dynamic repo url in plain_text section, got %#v", elements)
	}
	for _, element := range elements {
		if markdown := markdownContent(element); markdown != "" && markdown == dynamic {
			t.Fatalf("expected dynamic repo url to stay out of markdown, got %#v", elements)
		}
	}
	if !containsCardTextExact(elements, "执行中。普通输入已暂停，请等待完成或取消。") {
		t.Fatalf("expected footer to render as plain_text, got %#v", elements)
	}
}

func TestTargetPickerMessageDynamicTextUsesPlainText(t *testing.T) {
	dynamic := "目标目录已存在：/tmp/*/`demo`。"
	elements := targetPickerMessageElements([]control.FeishuTargetPickerMessage{{
		Level: control.FeishuTargetPickerMessageDanger,
		Text:  dynamic,
	}})
	if !containsCardTextExact(elements, dynamic) {
		t.Fatalf("expected dynamic message text in card output, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "<font color='red'>请先处理这个问题</font>") {
		t.Fatalf("expected danger label markdown, got %#v", elements)
	}
	for _, element := range elements {
		if markdown := markdownContent(element); markdown != "" && markdown == dynamic {
			t.Fatalf("expected dynamic message text to avoid markdown rendering, got %#v", elements)
		}
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

func TestTargetPickerEditingCardUsesStepHeaderInsteadOfSummary(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:               "picker-1",
		StageLabel:             "模式/目标",
		Question:               "切到哪个工作区 / 会话？",
		SelectedWorkspaceKey:   "/data/dl/web",
		SelectedWorkspaceLabel: "web",
		SelectedWorkspaceMeta:  "刚刚",
		ConfirmLabel:           "确认切换",
		WorkspaceOptions: []control.FeishuTargetPickerWorkspaceOption{
			{Value: "/data/dl/web", Label: "web", MetaText: "刚刚"},
		},
		SessionOptions: []control.FeishuTargetPickerSessionOption{
			{Value: "thread:thread-2", Kind: control.FeishuTargetPickerSessionThread, Label: "整理样式", MetaText: "刚刚"},
		},
	}, "life-step")
	if !containsMarkdownExact(elements, formatNeutralTextTag("模式/目标")) {
		t.Fatalf("expected step header stage tag, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**切到哪个工作区 / 会话？**") {
		t.Fatalf("expected step header question, got %#v", elements)
	}
	if containsMarkdownWithPrefix(elements, "**当前工作区**") {
		t.Fatalf("did not expect legacy summary block on editing card, got %#v", elements)
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
	if len(formElements) < 4 {
		t.Fatalf("expected git form inputs and actions, got %#v", form)
	}
	if formElements[0]["tag"] != "button" || formElements[0]["name"] != "target_picker_open_path" {
		t.Fatalf("expected git form to start with open-path button, got %#v", formElements)
	}
	if formElements[1]["name"] != control.FeishuTargetPickerGitRepoURLFieldName || formElements[2]["name"] != control.FeishuTargetPickerGitDirectoryNameFieldName {
		t.Fatalf("unexpected git form input names: %#v", formElements)
	}
	if len(formElements) != 4 {
		t.Fatalf("expected git form to keep two inputs plus two direct form buttons, got %#v", formElements)
	}
	if formElements[3]["tag"] != "button" || formElements[3]["name"] != "target_picker_confirm" {
		t.Fatalf("expected git form actions to stay flat inside form, got %#v", formElements)
	}
	var sawParentDirNearForm bool
	for i := 0; i < len(elements)-1; i++ {
		if cardStringValue(elements[i]["tag"]) != "markdown" || cardStringValue(elements[i+1]["tag"]) != "form" {
			continue
		}
		content := cardStringValue(elements[i]["content"])
		if !strings.Contains(content, "**落地父目录**") {
			continue
		}
		sawParentDirNearForm = true
		break
	}
	if !sawParentDirNearForm {
		t.Fatalf("expected open-path form to stay directly after parent-dir block, got %#v", elements)
	}

	var sawOpenPath bool
	var sawConfirm bool
	for _, action := range cardActionsFromElements(formElements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerOpenPathPicker:
			if cardValueMap(action)[cardActionPayloadKeyTargetValue] == control.FeishuTargetPickerPathFieldGitParentDir {
				sawOpenPath = true
			}
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	var sawCancel bool
	for _, action := range cardActionsFromElements(elements) {
		if cardValueMap(action)[cardActionPayloadKeyKind] == cardActionKindTargetPickerCancel {
			sawCancel = true
			break
		}
	}
	if !sawOpenPath || !sawCancel || !sawConfirm {
		t.Fatalf("expected git form to render open-path/confirm in form and cancel outside form, got %#v", elements)
	}
}

func TestProjectTargetPickerGitFormRendersFlatV2FormForInlineReplacement(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:              control.UIEventFeishuTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-5",
		FeishuTargetPickerView: &control.FeishuTargetPickerView{
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
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := renderedV2BodyElements(t, ops[0])
	var form map[string]any
	for _, element := range rendered {
		if cardStringValue(element["tag"]) == "form" {
			form = element
			break
		}
	}
	if form == nil {
		t.Fatalf("expected rendered git form, got %#v", rendered)
	}
	formElements, _ := form["elements"].([]map[string]any)
	if len(formElements) != 4 {
		t.Fatalf("expected rendered git form to keep flat form structure, got %#v", form)
	}
	if formElements[0]["tag"] != "button" || formElements[0]["name"] != "target_picker_open_path" {
		t.Fatalf("expected rendered git form to keep open-path button first, got %#v", formElements)
	}
	if formElements[3]["tag"] != "button" || formElements[3]["name"] != "target_picker_confirm" {
		t.Fatalf("expected rendered git form to keep confirm button last, got %#v", formElements)
	}
	for _, index := range []int{0, 3} {
		if formElements[index]["form_action_type"] != "submit" {
			t.Fatalf("expected rendered git form action %d to stay submit button, got %#v", index, formElements[index])
		}
	}
	for _, element := range formElements {
		if element["tag"] == "column_set" {
			t.Fatalf("did not expect column_set inside rendered git form, got %#v", formElements)
		}
	}
}
