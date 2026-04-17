package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectPathPickerStampsDaemonLifecycleID(t *testing.T) {
	projector := NewProjector()
	event := control.UIEvent{
		Kind:              control.UIEventFeishuPathPicker,
		SurfaceSessionID:  "surface-1",
		DaemonLifecycleID: "life-1",
		FeishuPathPickerView: &control.FeishuPathPickerView{
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
	ops := projector.Project("chat-1", event)
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
	if got, want := options, []string{"..", "alpha", ".hidden"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected directory options: got %v want %v", got, want)
	}
	if placeholder := selectStaticPlaceholder(t, elements, cardPathPickerDirectorySelectFieldName); placeholder != ".. 返回上一级，或选择子目录" {
		t.Fatalf("unexpected directory placeholder: %q", placeholder)
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
	if got, want := options, []string{"alpha"}; !equalPathPickerTestStrings(got, want) {
		t.Fatalf("unexpected root directory options: got %v want %v", got, want)
	}
}

func selectStaticOptionValues(t *testing.T, elements []map[string]any, fieldName string) []string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	return cardOptionValues(selectElement["options"])
}

func selectStaticPlaceholder(t *testing.T, elements []map[string]any, fieldName string) string {
	t.Helper()
	selectElement := findSelectStaticElement(t, elements, fieldName)
	placeholder, _ := selectElement["placeholder"].(map[string]any)
	return cardStringValue(placeholder["content"])
}

func findSelectStaticElement(t *testing.T, elements []map[string]any, fieldName string) map[string]any {
	t.Helper()
	for _, element := range elements {
		if cardStringValue(element["tag"]) != "select_static" || cardStringValue(element["name"]) != fieldName {
			continue
		}
		return element
	}
	t.Fatalf("select_static %q not found in %#v", fieldName, elements)
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
