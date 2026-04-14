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
	if len(actions) < 5 {
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
