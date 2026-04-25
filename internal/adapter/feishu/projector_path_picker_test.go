package feishu

import (
	"strings"
	"testing"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectPathPickerStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := eventcontract.Event{
		Kind:              eventcontract.KindPathPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		PathPickerView: &control.FeishuPathPickerView{
			PickerID:     "picker-1",
			Mode:         control.PathPickerModeFile,
			Title:        "选择文件",
			RootPath:     "/root",
			CurrentPath:  "/root",
			SelectedPath: "/root/a.txt",
			ConfirmLabel: "发送",
			CancelLabel:  "取消",
			CanConfirm:   true,
			Entries: []control.FeishuPathPickerEntry{
				{Name: "subdir", Label: "subdir", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
				{Name: "a.txt", Label: "a.txt", Kind: control.PathPickerEntryFile, ActionKind: control.PathPickerEntryActionSelect, Selected: true},
			},
		},
	}
	ops := projector.ProjectEvent("chat-1", event)
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one card op, got %#v", ops)
	}
	actions := cardActionsFromElements(ops[0].CardElements)
	if len(actions) == 0 {
		t.Fatalf("expected stamped picker actions, got %#v", ops[0].CardElements)
	}
	for _, action := range actions {
		value := cardValueMap(action)
		if value[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
			t.Fatalf("expected daemon lifecycle on picker action, got %#v", value)
		}
	}
}

func TestProjectPathPickerUsesUpdateCardWhenMessageIDPresent(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindPathPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		PathPickerView: &control.FeishuPathPickerView{
			PickerID:     "picker-1",
			MessageID:    "om-card-1",
			Mode:         control.PathPickerModeDirectory,
			Title:        "选择目录",
			RootPath:     "/root",
			CurrentPath:  "/root",
			SelectedPath: "/root",
			ConfirmLabel: "确认",
			CancelLabel:  "返回",
			CanConfirm:   true,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one card op, got %#v", ops)
	}
	if ops[0].Kind != OperationUpdateCard || ops[0].MessageID != "om-card-1" || ops[0].ReplyToMessageID != "" {
		t.Fatalf("expected update-card op for existing path picker message, got %#v", ops[0])
	}
	if !ops[0].CardUpdateMulti {
		t.Fatalf("expected path picker update to remain multi-update capable, got %#v", ops[0])
	}
}

func TestPathPickerTerminalElementsHideSelectorsAndButtons(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:    "picker-1",
		MessageID:   "om-card-1",
		Mode:        control.PathPickerModeFile,
		Title:       "发送文件",
		Terminal:    true,
		StatusTitle: "已开始发送，可继续其他操作",
		StatusSections: []control.FeishuCardTextSection{
			{Label: "文件", Lines: []string{"report.txt"}},
			{Label: "大小", Lines: []string{"101.0 MB"}},
		},
	}, "life-terminal")
	if len(elements) != 5 {
		t.Fatalf("expected title plus two labeled plain-text sections, got %#v", elements)
	}
	if containsButtonLabel(elements, "确认") || containsButtonLabel(elements, "取消") {
		t.Fatalf("expected terminal picker to omit buttons, got %#v", elements)
	}
	for _, element := range elements {
		if cardStringValue(element["tag"]) == "select_static" {
			t.Fatalf("expected terminal picker to omit selectors, got %#v", elements)
		}
	}
	if !containsMarkdownExact(elements, "**已开始发送，可继续其他操作**") {
		t.Fatalf("expected terminal status title, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**文件**") || !containsCardTextExact(elements, "report.txt") {
		t.Fatalf("expected terminal picker file section, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**大小**") || !containsCardTextExact(elements, "101.0 MB") {
		t.Fatalf("expected terminal picker size section, got %#v", elements)
	}
}

func TestPathPickerTerminalSectionsKeepDynamicValuesOutOfMarkdown(t *testing.T) {
	dynamic := "report `*.md`"
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:    "picker-1",
		Mode:        control.PathPickerModeFile,
		Title:       "发送文件",
		Terminal:    true,
		StatusTitle: "已开始发送，可继续其他操作",
		StatusSections: []control.FeishuCardTextSection{
			{Label: "文件", Lines: []string{dynamic}},
			{Label: "大小", Lines: []string{"101.0 MB"}},
		},
	}, "life-dynamic")
	if !containsCardTextExact(elements, dynamic) {
		t.Fatalf("expected dynamic file name in plain_text block, got %#v", elements)
	}
	for _, element := range elements {
		if markdown := markdownContent(element); markdown != "" && markdown == dynamic {
			t.Fatalf("expected dynamic value to stay out of markdown, got %#v", elements)
		}
	}
}

func TestPathPickerElementsUseEnterAndSelectPayloadKinds(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeFile,
		Title:        "选择文件",
		RootPath:     "/root",
		CurrentPath:  "/root",
		ConfirmLabel: "确认",
		CancelLabel:  "取消",
		Entries: []control.FeishuPathPickerEntry{
			{Name: "subdir", Label: "subdir", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: "a.txt", Label: "a.txt", Kind: control.PathPickerEntryFile, ActionKind: control.PathPickerEntryActionSelect},
		},
	}, "life-2")
	actions := cardActionsFromElements(elements)
	if len(actions) < 4 {
		t.Fatalf("expected picker navigation and entry actions, got %#v", actions)
	}
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected file picker footer actions to be separated by divider, got %#v", elements)
	}
	selectCount := 0
	for _, element := range elements {
		if element["tag"] == "select_static" {
			selectCount++
		}
	}
	if selectCount != 2 {
		t.Fatalf("expected compact file picker to render two selects, got %#v", elements)
	}
	if containsButtonLabel(elements, "进入 · subdir") || containsButtonLabel(elements, "选择 · a.txt") {
		t.Fatalf("expected compact file picker to avoid per-entry buttons, got %#v", elements)
	}
	if containsButtonLabel(elements, "上一级") {
		t.Fatalf("expected compact file picker to use .. directory option instead of up button, got %#v", elements)
	}
	foundEnter := false
	foundSelect := false
	for _, action := range actions {
		value := cardValueMap(action)
		switch value[cardActionPayloadKeyKind] {
		case cardActionKindPathPickerEnter:
			foundEnter = true
		case cardActionKindPathPickerSelect:
			foundSelect = true
		}
	}
	if !foundEnter || !foundSelect {
		t.Fatalf("expected enter/select payload kinds, got %#v", actions)
	}
}

func TestDirectoryModePathPickerUsesCompactDirectorySelect(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeDirectory,
		Title:        "选择目录",
		RootPath:     "/root",
		CurrentPath:  "/root",
		SelectedPath: "/root",
		ConfirmLabel: "确认",
		CancelLabel:  "取消",
		Entries: []control.FeishuPathPickerEntry{
			{Name: "subdir", Label: "subdir", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: ".hidden", Label: ".hidden", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: "note.txt", Label: "note.txt", Kind: control.PathPickerEntryFile, Disabled: true, DisabledReason: "当前只可选择目录"},
		},
	}, "life-dir")

	actions := cardActionsFromElements(elements)
	if len(actions) < 3 {
		t.Fatalf("expected directory picker actions, got %#v", actions)
	}
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected directory picker footer actions to be separated by divider, got %#v", elements)
	}
	selectCount := 0
	for _, element := range elements {
		if element["tag"] == "select_static" {
			selectCount++
		}
	}
	if selectCount != 1 {
		t.Fatalf("expected directory picker to render one compact select, got %#v", elements)
	}
	if containsButtonLabel(elements, "进入 · subdir") || containsButtonLabel(elements, "进入 · .hidden") {
		t.Fatalf("expected directory picker to avoid per-entry buttons, got %#v", elements)
	}
	if containsButtonLabel(elements, "上一级") {
		t.Fatalf("expected directory picker to use .. directory option instead of up button, got %#v", elements)
	}
	if containsMarkdownExact(elements, "**目录内容**") || containsMarkdownWithPrefix(elements, "1. ") {
		t.Fatalf("expected directory picker to avoid verbose entry listing, got %#v", elements)
	}
	options := selectStaticOptionValues(t, elements, cardPathPickerDirectorySelectFieldName)
	if got, want := options, []string{".", "subdir", ".hidden"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory options: got %v want %v", got, want)
	}
	if got, want := selectStaticOptionLabels(t, elements, cardPathPickerDirectorySelectFieldName), []string{"root（当前目录）", "subdir/", ".hidden/"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory option labels: got %v want %v", got, want)
	}
	if initial := selectStaticInitialOption(t, elements, cardPathPickerDirectorySelectFieldName); initial != "." {
		t.Fatalf("expected current-directory initial option, got %q", initial)
	}
	if placeholder := selectStaticPlaceholder(t, elements, cardPathPickerDirectorySelectFieldName); placeholder != ".. 返回上一级，或选择子目录" {
		t.Fatalf("unexpected directory placeholder: %q", placeholder)
	}
	foundEnter := false
	for _, action := range actions {
		value := cardValueMap(action)
		if value[cardActionPayloadKeyKind] == cardActionKindPathPickerEnter {
			foundEnter = true
		}
	}
	if !foundEnter {
		t.Fatalf("expected compact directory picker to use enter payloads, got %#v", actions)
	}
}

func TestDirectoryModePathPickerPrependsParentOptionWhenCanGoUp(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeDirectory,
		Title:        "选择目录",
		RootPath:     "/root",
		CurrentPath:  "/root/subdir",
		SelectedPath: "/root/subdir",
		ConfirmLabel: "确认",
		CancelLabel:  "取消",
		CanGoUp:      true,
		Entries: []control.FeishuPathPickerEntry{
			{Name: "alpha", Label: "alpha", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: ".hidden", Label: ".hidden", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
		},
	}, "life-dir-up")

	options := selectStaticOptionValues(t, elements, cardPathPickerDirectorySelectFieldName)
	if got, want := options, []string{".", "..", "alpha", ".hidden"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory options with parent: got %v want %v", got, want)
	}
	if got, want := selectStaticOptionLabels(t, elements, cardPathPickerDirectorySelectFieldName), []string{"subdir（当前目录）", "..", "alpha/", ".hidden/"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory option labels with parent: got %v want %v", got, want)
	}
	if placeholder := selectStaticPlaceholder(t, elements, cardPathPickerDirectorySelectFieldName); placeholder != ".. 返回上一级，或选择子目录" {
		t.Fatalf("unexpected directory placeholder: %q", placeholder)
	}
	if initial := selectStaticInitialOption(t, elements, cardPathPickerDirectorySelectFieldName); initial != "." {
		t.Fatalf("expected current-directory initial option, got %q", initial)
	}
}

func TestPathPickerFileModePaginatesOversizedLanesAndKeepsFooter(t *testing.T) {
	entries := make([]control.FeishuPathPickerEntry, 0, 260)
	for i := 0; i < 120; i++ {
		name := "dir-" + leftPad3(i)
		entries = append(entries, control.FeishuPathPickerEntry{
			Name:       name,
			Label:      name + "-" + strings.Repeat("d", 80),
			Kind:       control.PathPickerEntryDirectory,
			ActionKind: control.PathPickerEntryActionEnter,
		})
	}
	for i := 0; i < 140; i++ {
		name := "file-" + leftPad3(i) + ".txt"
		entries = append(entries, control.FeishuPathPickerEntry{
			Name:       name,
			Label:      name + "-" + strings.Repeat("f", 96),
			Kind:       control.PathPickerEntryFile,
			ActionKind: control.PathPickerEntryActionSelect,
			Selected:   i == 96,
		})
	}

	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:        "picker-1",
		Mode:            control.PathPickerModeFile,
		Title:           "选择文件",
		RootPath:        "/root",
		CurrentPath:     "/root",
		SelectedPath:    "/root/file-096.txt",
		DirectoryCursor: 88,
		FileCursor:      96,
		ConfirmLabel:    "发送",
		CancelLabel:     "取消",
		CanConfirm:      true,
		Entries:         entries,
	}, "life-large")

	size, err := cardtransport.InteractiveMessageCardSize("选择文件", "", cardThemeInfo, elements, true)
	if err != nil {
		t.Fatalf("measure path picker card: %v", err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected paginated path picker to fit transport budget, got %d bytes", size)
	}

	var sawDirectoryPage, sawFilePage, sawConfirm bool
	for _, action := range cardActionsFromElements(elements) {
		value := cardValueMap(action)
		switch value[cardActionPayloadKeyKind] {
		case cardActionKindPathPickerPage:
			switch value[cardActionPayloadKeyFieldName] {
			case cardPathPickerDirectorySelectFieldName:
				sawDirectoryPage = true
			case cardPathPickerFileSelectFieldName:
				sawFilePage = true
			}
		case cardActionKindPathPickerConfirm:
			sawConfirm = true
		}
	}
	if !sawDirectoryPage || !sawFilePage {
		t.Fatalf("expected large file picker to render page callbacks for both selects, got %#v", elements)
	}
	if !sawConfirm || !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected large file picker to keep footer actions visible, got %#v", elements)
	}
	if !containsCardTextExact(elements, pathPickerPaginationHint) {
		t.Fatalf("expected large file picker to render pagination hint, got %#v", elements)
	}
}

func TestPathPickerDirectoryModePaginatesOversizedLaneAndKeepsFixedOptions(t *testing.T) {
	entries := make([]control.FeishuPathPickerEntry, 0, 180)
	for i := 0; i < 180; i++ {
		name := "dir-" + leftPad3(i)
		entries = append(entries, control.FeishuPathPickerEntry{
			Name:       name,
			Label:      name + "-" + strings.Repeat("d", 92),
			Kind:       control.PathPickerEntryDirectory,
			ActionKind: control.PathPickerEntryActionEnter,
		})
	}

	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:        "picker-1",
		Mode:            control.PathPickerModeDirectory,
		Title:           "选择目录",
		RootPath:        "/root",
		CurrentPath:     "/root/nested",
		SelectedPath:    "/root/nested",
		DirectoryCursor: 121,
		ConfirmLabel:    "确认",
		CancelLabel:     "取消",
		CanGoUp:         true,
		CanConfirm:      true,
		Entries:         entries,
	}, "life-dir-large")

	size, err := cardtransport.InteractiveMessageCardSize("选择目录", "", cardThemeInfo, elements, true)
	if err != nil {
		t.Fatalf("measure directory path picker card: %v", err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected directory path picker to fit transport budget, got %d bytes", size)
	}

	options := selectStaticOptionValues(t, elements, cardPathPickerDirectorySelectFieldName)
	if len(options) < 2 || options[0] != "." || options[1] != ".." {
		t.Fatalf("expected paginated directory picker to keep fixed current/parent options, got %v", options)
	}
	if !containsCardTextExact(elements, pathPickerPaginationHint) {
		t.Fatalf("expected directory path picker to render pagination hint, got %#v", elements)
	}
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected directory path picker to keep footer visible, got %#v", elements)
	}
}

func TestPathPickerOwnerSubpageDirectoryPaginatesOversizedLaneAndKeepsFooter(t *testing.T) {
	entries := make([]control.FeishuPathPickerEntry, 0, 180)
	for i := 0; i < 180; i++ {
		name := "dir-" + leftPad3(i)
		entries = append(entries, control.FeishuPathPickerEntry{
			Name:       name,
			Label:      name + "-" + strings.Repeat("d", 88),
			Kind:       control.PathPickerEntryDirectory,
			ActionKind: control.PathPickerEntryActionEnter,
		})
	}

	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:        "picker-1",
		Mode:            control.PathPickerModeDirectory,
		Title:           "选择目录",
		StageLabel:      "步骤 2/2",
		Question:        "选择工作目录",
		RootPath:        "/root",
		CurrentPath:     "/root",
		SelectedPath:    "/root",
		DirectoryCursor: 123,
		ConfirmLabel:    "使用这个目录",
		CancelLabel:     "返回",
		CanConfirm:      true,
		Entries:         entries,
	}, "life-owner-large")

	size, err := cardtransport.InteractiveMessageCardSize("选择目录", "", cardThemeInfo, elements, true)
	if err != nil {
		t.Fatalf("measure owner-subpage path picker card: %v", err)
	}
	if size > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("expected owner-subpage path picker to fit transport budget, got %d bytes", size)
	}
	if !containsCardTextExact(elements, pathPickerPaginationHint) {
		t.Fatalf("expected owner-subpage path picker to render pagination hint, got %#v", elements)
	}
	if !containsRenderedTag(elements, "hr") {
		t.Fatalf("expected owner-subpage path picker to keep footer visible, got %#v", elements)
	}
}

func TestOwnerSubpageDirectoryPathPickerUsesStepHeaderLayout(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeDirectory,
		Title:        "选择工作区与会话",
		StageLabel:   "目录/选择目录",
		Question:     "选择要接入的目录",
		RootPath:     "/workspace",
		CurrentPath:  "/workspace/projects",
		SelectedPath: "/workspace/projects",
		ConfirmLabel: "使用这个目录",
		CancelLabel:  "返回",
		CanConfirm:   true,
		Entries: []control.FeishuPathPickerEntry{
			{Name: "demo", Label: "demo", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
		},
	}, "life-owner-subpage")
	if !containsMarkdownExact(elements, formatNeutralTextTag("目录/选择目录")) {
		t.Fatalf("expected owner-subpage stage tag, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**选择要接入的目录**") {
		t.Fatalf("expected owner-subpage question, got %#v", elements)
	}
	if containsMarkdownExact(elements, "**当前选择**") {
		t.Fatalf("did not expect legacy current-selection summary, got %#v", elements)
	}
	if !containsMarkdownExact(elements, "**当前位置**") {
		t.Fatalf("expected current-path block in owner-subpage layout, got %#v", elements)
	}
}

func TestFileModePathPickerPrependsParentOptionWhenCanGoUp(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeFile,
		Title:        "选择文件",
		RootPath:     "/root",
		CurrentPath:  "/root/subdir",
		ConfirmLabel: "确认",
		CancelLabel:  "取消",
		CanGoUp:      true,
		Entries: []control.FeishuPathPickerEntry{
			{Name: "alpha", Label: "alpha", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: ".hidden", Label: ".hidden", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
			{Name: "a.txt", Label: "a.txt", Kind: control.PathPickerEntryFile, ActionKind: control.PathPickerEntryActionSelect},
		},
	}, "life-3")

	options := selectStaticOptionValues(t, elements, cardPathPickerDirectorySelectFieldName)
	if got, want := options, []string{".", "..", "alpha", ".hidden"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory options: got %v want %v", got, want)
	}
	if got, want := selectStaticOptionLabels(t, elements, cardPathPickerDirectorySelectFieldName), []string{"subdir（当前目录）", "..", "alpha/", ".hidden/"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected file-mode directory option labels: got %v want %v", got, want)
	}
	if placeholder := selectStaticPlaceholder(t, elements, cardPathPickerDirectorySelectFieldName); placeholder != ".. 返回上一级，或选择子目录" {
		t.Fatalf("unexpected directory placeholder: %q", placeholder)
	}
	if initial := selectStaticInitialOption(t, elements, cardPathPickerDirectorySelectFieldName); initial != "." {
		t.Fatalf("expected current-directory initial option, got %q", initial)
	}
}

func TestFileModePathPickerOmitsParentOptionAtRoot(t *testing.T) {
	elements := pathPickerElements(control.FeishuPathPickerView{
		PickerID:     "picker-1",
		Mode:         control.PathPickerModeFile,
		Title:        "选择文件",
		RootPath:     "/root",
		CurrentPath:  "/root",
		ConfirmLabel: "确认",
		CancelLabel:  "取消",
		Entries: []control.FeishuPathPickerEntry{
			{Name: "alpha", Label: "alpha", Kind: control.PathPickerEntryDirectory, ActionKind: control.PathPickerEntryActionEnter},
		},
	}, "life-4")

	options := selectStaticOptionValues(t, elements, cardPathPickerDirectorySelectFieldName)
	if got, want := options, []string{".", "alpha"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected root directory options: got %v want %v", got, want)
	}
	if got, want := selectStaticOptionLabels(t, elements, cardPathPickerDirectorySelectFieldName), []string{"root（当前目录）", "alpha/"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected root directory option labels: got %v want %v", got, want)
	}
	if initial := selectStaticInitialOption(t, elements, cardPathPickerDirectorySelectFieldName); initial != "." {
		t.Fatalf("expected current-directory initial option, got %q", initial)
	}
}

func selectStaticOptionValues(t *testing.T, elements []map[string]any, fieldName string) []string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	return cardOptionValues(selectElement["options"])
}

func selectStaticOptionLabels(t *testing.T, elements []map[string]any, fieldName string) []string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	return cardOptionLabels(selectElement["options"])
}

func selectStaticPlaceholder(t *testing.T, elements []map[string]any, fieldName string) string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	placeholder, _ := selectElement["placeholder"].(map[string]any)
	return cardStringValue(placeholder["content"])
}

func selectStaticInitialOption(t *testing.T, elements []map[string]any, fieldName string) string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	return cardStringValue(selectElement["initial_option"])
}

func findSelectStaticElement(t *testing.T, elements []map[string]any, fieldName string) map[string]any {
	t.Helper()
	for _, element := range elements {
		switch cardStringValue(element["tag"]) {
		case "select_static":
			if cardStringValue(element["name"]) == fieldName {
				return element
			}
		case "column_set":
			columns, _ := element["columns"].([]map[string]any)
			for _, column := range columns {
				columnElements, _ := column["elements"].([]map[string]any)
				if selectElement := findSelectStaticElementOptional(columnElements, fieldName); selectElement != nil {
					return selectElement
				}
			}
		}
	}
	t.Fatalf("select_static %q not found in %#v", fieldName, elements)
	return nil
}

func findSelectStaticElementOptional(elements []map[string]any, fieldName string) map[string]any {
	for _, element := range elements {
		switch cardStringValue(element["tag"]) {
		case "select_static":
			if cardStringValue(element["name"]) == fieldName {
				return element
			}
		case "column_set":
			columns, _ := element["columns"].([]map[string]any)
			for _, column := range columns {
				columnElements, _ := column["elements"].([]map[string]any)
				if selectElement := findSelectStaticElementOptional(columnElements, fieldName); selectElement != nil {
					return selectElement
				}
			}
		}
	}
	return nil
}

func cardOptionValues(raw any) []string {
	switch typed := raw.(type) {
	case []map[string]any:
		values := make([]string, 0, len(typed))
		for _, option := range typed {
			if value := cardStringValue(option["value"]); value != "" {
				values = append(values, value)
			}
		}
		return values
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			option, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if value := cardStringValue(option["value"]); value != "" {
				values = append(values, value)
			}
		}
		return values
	default:
		return nil
	}
}

func cardOptionLabels(raw any) []string {
	switch typed := raw.(type) {
	case []map[string]any:
		labels := make([]string, 0, len(typed))
		for _, option := range typed {
			if text, ok := option["text"].(map[string]any); ok {
				labels = append(labels, cardStringValue(text["content"]))
			}
		}
		return labels
	case []any:
		labels := make([]string, 0, len(typed))
		for _, item := range typed {
			option, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := option["text"].(map[string]any); ok {
				labels = append(labels, cardStringValue(text["content"]))
			}
		}
		return labels
	default:
		return nil
	}
}

func equalPathPickerTestStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
