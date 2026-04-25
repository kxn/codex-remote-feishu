package feishu

import (
	"fmt"
	"strings"
	"testing"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectTargetPickerStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := eventcontract.Event{
		Kind:              eventcontract.KindTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		TargetPickerView: &control.FeishuTargetPickerView{
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
	ops := projector.ProjectEvent("chat-1", event)
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
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		TargetPickerView: &control.FeishuTargetPickerView{
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
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected processing git-import card to separate footer actions with divider, got %#v", elements)
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
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected target picker editing card to render footer divider, got %#v", elements)
	}
}

func TestTargetPickerElementsRenderLockedWorkspaceAsReadOnlyContext(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:                 "picker-1",
		WorkspaceSelectionLocked: true,
		SelectedWorkspaceKey:     "/data/dl/web",
		SelectedWorkspaceLabel:   "web",
		SelectedWorkspaceMeta:    "当前绑定工作区",
		SessionPlaceholder:       "选择会话",
		ConfirmLabel:             "切换",
		SessionOptions: []control.FeishuTargetPickerSessionOption{
			{Value: "thread:thread-2", Kind: control.FeishuTargetPickerSessionThread, Label: "整理样式", MetaText: "刚刚"},
		},
	}, "life-locked")

	selectCount := 0
	for _, element := range elements {
		if cardStringValue(element["tag"]) == "select_static" {
			selectCount++
		}
	}
	if selectCount != 1 {
		t.Fatalf("expected locked target picker to render session select only, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**当前工作区**") {
		t.Fatalf("expected locked picker to render read-only workspace summary, got %#v", elements)
	}
	var sawWorkspaceSummary bool
	for _, element := range elements {
		text := plainTextContent(element)
		if strings.Contains(text, "web") && strings.Contains(text, "当前绑定工作区") {
			sawWorkspaceSummary = true
			break
		}
	}
	if !sawWorkspaceSummary {
		t.Fatalf("expected locked picker to render read-only workspace summary text, got %#v", elements)
	}
	if containsMarkdownExact(elements, "**工作区**") {
		t.Fatalf("did not expect editable workspace header for locked picker, got %#v", elements)
	}
	actions := cardActionsFromElements(elements)
	var sawWorkspace, sawSession bool
	for _, action := range actions {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerSelectWorkspace:
			sawWorkspace = true
		case cardActionKindTargetPickerSelectSession:
			sawSession = true
		}
	}
	if sawWorkspace || !sawSession {
		t.Fatalf("expected locked picker to keep only session selection callback, got %#v", actions)
	}
}

func TestTargetPickerElementsPaginateLargeDualSelectAndKeepFooter(t *testing.T) {
	workspaceOptions := make([]control.FeishuTargetPickerWorkspaceOption, 0, 120)
	for i := 0; i < 120; i++ {
		value := "workspace-" + leftPad3(i)
		workspaceOptions = append(workspaceOptions, control.FeishuTargetPickerWorkspaceOption{
			Value:    value,
			Label:    "工作区 " + leftPad3(i) + " " + strings.Repeat("w", 80),
			MetaText: "最近活动 " + strings.Repeat("m", 80),
		})
	}
	sessionOptions := make([]control.FeishuTargetPickerSessionOption, 0, 180)
	for i := 0; i < 180; i++ {
		value := "thread:thread-" + leftPad3(i)
		sessionOptions = append(sessionOptions, control.FeishuTargetPickerSessionOption{
			Value:    value,
			Kind:     control.FeishuTargetPickerSessionThread,
			Label:    "会话 " + leftPad3(i) + " " + strings.Repeat("s", 100),
			MetaText: "最近消息 " + strings.Repeat("t", 100),
		})
	}

	view := control.FeishuTargetPickerView{
		PickerID:             "picker-1",
		Title:                "选择工作区与会话",
		WorkspacePlaceholder: "选择工作区",
		SessionPlaceholder:   "选择会话",
		WorkspaceCursor:      48,
		SessionCursor:        132,
		SelectedWorkspaceKey: "workspace-048",
		SelectedSessionValue: "thread:thread-132",
		ConfirmLabel:         "切换",
		CanConfirm:           true,
		WorkspaceOptions:     workspaceOptions,
		SessionOptions:       sessionOptions,
	}

	elements := targetPickerElements(view, "life-large")
	size, err := cardtransport.InteractiveMessageCardSize(view.Title, "", targetPickerTheme(view), elements, true)
	if err != nil {
		t.Fatalf("measure target picker card: %v", err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected paginated target picker to fit transport budget, got %d bytes", size)
	}

	var sawWorkspacePage, sawSessionPage, sawConfirm bool
	for _, action := range cardActionsFromElements(elements) {
		value := cardValueMap(action)
		switch value[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerPage:
			switch value[cardActionPayloadKeyFieldName] {
			case cardTargetPickerWorkspaceFieldName:
				sawWorkspacePage = true
			case cardTargetPickerSessionFieldName:
				sawSessionPage = true
			}
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawWorkspacePage || !sawSessionPage {
		t.Fatalf("expected large target picker to render page callbacks for both selects, got %#v", elements)
	}
	if !sawConfirm || !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected large target picker to keep footer actions visible, got %#v", elements)
	}
	if !containsCardTextExact(elements, targetPickerPaginationHint) {
		t.Fatalf("expected large target picker to render pagination hint, got %#v", elements)
	}
}

func TestTargetPickerElementsPaginateLockedWorkspaceSessionAndKeepFooter(t *testing.T) {
	sessionOptions := make([]control.FeishuTargetPickerSessionOption, 0, 180)
	for i := 0; i < 180; i++ {
		value := "thread:thread-" + leftPad3(i)
		sessionOptions = append(sessionOptions, control.FeishuTargetPickerSessionOption{
			Value:    value,
			Kind:     control.FeishuTargetPickerSessionThread,
			Label:    "会话 " + leftPad3(i) + " " + strings.Repeat("s", 120),
			MetaText: "最近消息 " + strings.Repeat("t", 120),
		})
	}

	view := control.FeishuTargetPickerView{
		PickerID:                 "picker-1",
		Title:                    "选择工作区与会话",
		WorkspaceSelectionLocked: true,
		SelectedWorkspaceKey:     "/data/dl/web",
		SelectedWorkspaceLabel:   "web",
		SelectedWorkspaceMeta:    "当前绑定工作区",
		SessionPlaceholder:       "选择会话",
		SessionCursor:            121,
		SelectedSessionValue:     "thread:thread-121",
		ConfirmLabel:             "切换",
		CanConfirm:               true,
		SessionOptions:           sessionOptions,
	}

	elements := targetPickerElements(view, "life-locked-large")
	size, err := cardtransport.InteractiveMessageCardSize(view.Title, "", targetPickerTheme(view), elements, true)
	if err != nil {
		t.Fatalf("measure locked target picker card: %v", err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected locked target picker to fit transport budget, got %d bytes", size)
	}

	var sawSessionPage, sawWorkspacePage bool
	for _, action := range cardActionsFromElements(elements) {
		value := cardValueMap(action)
		if value[cardActionPayloadKeyKind] != cardActionKindTargetPickerPage {
			continue
		}
		switch value[cardActionPayloadKeyFieldName] {
		case cardTargetPickerSessionFieldName:
			sawSessionPage = true
		case cardTargetPickerWorkspaceFieldName:
			sawWorkspacePage = true
		}
	}
	if !sawSessionPage || sawWorkspacePage {
		t.Fatalf("expected locked target picker to paginate only the session lane, got %#v", elements)
	}
	if !containsCardTextExact(elements, targetPickerPaginationHint) {
		t.Fatalf("expected locked target picker to render pagination hint, got %#v", elements)
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

func leftPad3(value int) string {
	return fmt.Sprintf("%03d", value)
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

func TestTargetPickerElementsRenderSourceChoicesAsSingleStepPage(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:          "picker-1",
		Title:             "选择工作区与会话",
		Page:              control.FeishuTargetPickerPageSource,
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
	var sawConfirm bool
	for _, action := range cardActionsFromElements(elements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerSelectSource:
			sawSourceSelect = true
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawSourceSelect {
		t.Fatalf("expected source page to render source selection callbacks, got %#v", elements)
	}
	if sawConfirm {
		t.Fatalf("expected source page to advance by clicking a choice instead of rendering a footer confirm, got %#v", elements)
	}
}

func TestTargetPickerElementsRenderLocalDirectoryOpenPathAction(t *testing.T) {
	elements := targetPickerElements(control.FeishuTargetPickerView{
		PickerID:         "picker-1",
		Title:            "选择工作区与会话",
		Page:             control.FeishuTargetPickerPageLocalDirectory,
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
		Page:             control.FeishuTargetPickerPageGit,
		SelectedMode:     control.FeishuTargetPickerModeAddWorkspace,
		SelectedSource:   control.FeishuTargetPickerSourceGitURL,
		ShowModeSwitch:   true,
		ShowSourceSelect: true,
		CanGoBack:        true,
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
	if formElements[0]["tag"] != "column_set" {
		t.Fatalf("expected git form to start with parent-dir row, got %#v", formElements)
	}
	rowColumns, _ := formElements[0]["columns"].([]map[string]any)
	if len(rowColumns) != 2 {
		t.Fatalf("expected parent-dir row to render two columns, got %#v", formElements[0])
	}
	leftElements, _ := rowColumns[0]["elements"].([]map[string]any)
	if len(leftElements) != 1 || !strings.Contains(markdownContent(leftElements[0]), "**落地父目录**") {
		t.Fatalf("expected parent-dir row to keep directory markdown on the left, got %#v", formElements[0])
	}
	rightElements, _ := rowColumns[1]["elements"].([]map[string]any)
	if len(rightElements) != 1 || rightElements[0]["tag"] != "button" || rightElements[0]["name"] != "target_picker_open_path" {
		t.Fatalf("expected parent-dir row to keep open-path button on the right, got %#v", formElements[0])
	}
	if formElements[1]["name"] != control.FeishuTargetPickerGitRepoURLFieldName || formElements[2]["name"] != control.FeishuTargetPickerGitDirectoryNameFieldName {
		t.Fatalf("unexpected git form input names: %#v", formElements)
	}
	if len(formElements) != 5 {
		t.Fatalf("expected git form to keep parent-dir row, two inputs, divider and footer row, got %#v", formElements)
	}
	if formElements[3]["tag"] != "hr" {
		t.Fatalf("expected git form footer to be separated by divider, got %#v", formElements)
	}
	if formElements[4]["tag"] != "column_set" {
		t.Fatalf("expected git form footer actions to render as a horizontal button row, got %#v", formElements)
	}
	footerButtons := cardElementButtons(t, formElements[4])
	if len(footerButtons) != 3 {
		t.Fatalf("expected cancel/back/confirm in git form footer row, got %#v", formElements[4])
	}
	if footerButtons[0]["name"] != "target_picker_cancel" || footerButtons[1]["name"] != "target_picker_back" || footerButtons[2]["name"] != "target_picker_confirm" {
		t.Fatalf("expected git form footer to keep cancel/back/confirm order, got %#v", footerButtons)
	}
	if containsMarkdownWithPrefix(elements, "**最终路径**") {
		t.Fatalf("did not expect git card to render final-path preview, got %#v", elements)
	}

	var sawOpenPath bool
	var sawConfirm bool
	var sawBack bool
	for _, action := range cardActionsFromElements(formElements) {
		switch cardValueMap(action)[cardActionPayloadKeyKind] {
		case cardActionKindTargetPickerOpenPathPicker:
			if cardValueMap(action)[cardActionPayloadKeyTargetValue] == control.FeishuTargetPickerPathFieldGitParentDir {
				sawOpenPath = true
			}
		case cardActionKindTargetPickerBack:
			sawBack = true
		case cardActionKindTargetPickerConfirm:
			sawConfirm = true
		}
	}
	var sawCancel bool
	for _, action := range cardActionsFromElements(formElements) {
		if cardValueMap(action)[cardActionPayloadKeyKind] == cardActionKindTargetPickerCancel {
			sawCancel = true
			break
		}
	}
	if !sawOpenPath || !sawCancel || !sawBack || !sawConfirm {
		t.Fatalf("expected git form to render open-path/cancel/confirm inside the same form, got %#v", elements)
	}
}

func TestProjectTargetPickerGitFormRendersFlatV2FormForInlineReplacement(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindTargetPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-5",
		TargetPickerView: &control.FeishuTargetPickerView{
			PickerID:         "picker-1",
			Title:            "选择工作区与会话",
			Page:             control.FeishuTargetPickerPageGit,
			SelectedMode:     control.FeishuTargetPickerModeAddWorkspace,
			SelectedSource:   control.FeishuTargetPickerSourceGitURL,
			ShowModeSwitch:   true,
			ShowSourceSelect: true,
			CanGoBack:        true,
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
	if len(formElements) != 5 {
		t.Fatalf("expected rendered git form to keep parent-dir row, inputs and footer row, got %#v", form)
	}
	if formElements[0]["tag"] != "column_set" {
		t.Fatalf("expected rendered git form to start with parent-dir row, got %#v", formElements)
	}
	rowColumns, _ := formElements[0]["columns"].([]map[string]any)
	if len(rowColumns) != 2 {
		t.Fatalf("expected rendered parent-dir row to keep two columns, got %#v", formElements[0])
	}
	rowRightElements, _ := rowColumns[1]["elements"].([]map[string]any)
	if len(rowRightElements) != 1 || rowRightElements[0]["tag"] != "button" || rowRightElements[0]["name"] != "target_picker_open_path" {
		t.Fatalf("expected rendered open-path button on the right side of parent-dir row, got %#v", formElements[0])
	}
	if rowRightElements[0]["action_type"] != nil || rowRightElements[0]["form_action_type"] != "submit" {
		t.Fatalf("expected rendered open-path button to stay form submit action, got %#v", rowRightElements[0])
	}
	if formElements[3]["tag"] != "hr" {
		t.Fatalf("expected rendered git form to keep divider before footer buttons, got %#v", formElements)
	}
	if formElements[4]["tag"] != "column_set" {
		t.Fatalf("expected rendered git form footer to stay horizontal, got %#v", formElements)
	}
	renderedFooterButtons := renderedColumnButtons(t, formElements[4])
	if len(renderedFooterButtons) != 3 {
		t.Fatalf("expected rendered git footer row to keep three buttons, got %#v", formElements[4])
	}
	for _, button := range renderedFooterButtons {
		if button["action_type"] != nil || button["form_action_type"] != "submit" {
			t.Fatalf("expected rendered git footer button to stay form submit action, got %#v", button)
		}
	}
	if containsMarkdownWithPrefix(rendered, "**最终路径**") {
		t.Fatalf("did not expect rendered git form to include final-path preview, got %#v", rendered)
	}
}
