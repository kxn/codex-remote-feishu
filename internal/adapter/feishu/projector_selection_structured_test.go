package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectInstanceSelectionViewUsesStructuredButtons(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptAttachInstance,
		Instance: &control.FeishuInstanceSelectionView{
			Current: &control.FeishuInstanceSelectionCurrent{
				InstanceID:  "inst-current",
				Label:       "droid",
				ContextText: "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list",
			},
			Entries: []control.FeishuInstanceSelectionEntry{
				{
					InstanceID:  "inst-2",
					Label:       "web",
					ButtonLabel: "切换",
					MetaText:    "2分前 · 当前焦点可跟随",
				},
				{
					InstanceID: "inst-3",
					Label:      "ops",
					MetaText:   "30分前 · 当前被其他飞书会话接管",
					Disabled:   true,
				},
			},
		},
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindSelection,
		DaemonLifecycleID: "life-1",
		SelectionView:     &view,
		SelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:     control.FeishuUIDTOwnerSelection,
			PromptKind:   control.SelectionPromptAttachInstance,
			Title:        "在线 VS Code 实例",
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中\n焦点切换仍会自动跟随，换实例才用 /list",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "在线 VS Code 实例" {
		t.Fatalf("unexpected card title: %#v", ops[0])
	}
	rendered := renderedV2BodyElements(t, ops[0])
	if containsRenderedTag(rendered, "select_static") {
		t.Fatalf("expected instance view to stay button-based, got %#v", rendered)
	}
	var buttonLabels []string
	for _, element := range rendered {
		if element["tag"] != "button" && element["tag"] != "column_set" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			buttonLabels = append(buttonLabels, cardButtonLabel(t, button))
			value := renderedButtonCallbackValue(t, button)
			if value[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
				t.Fatalf("expected stamped daemon lifecycle on instance button, got %#v", value)
			}
		}
	}
	if !containsString(buttonLabels, "切换 · web") || !containsString(buttonLabels, "不可接管 · ops") {
		t.Fatalf("unexpected structured instance button labels: %#v", buttonLabels)
	}
}

func TestProjectVSCodeThreadSelectionViewUsesDropdown(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread: &control.FeishuThreadSelectionView{
			Mode: control.FeishuThreadSelectionVSCodeRecent,
			CurrentInstance: &control.FeishuThreadSelectionInstanceContext{
				Label:  "droid",
				Status: "当前跟随中",
			},
			Entries: []control.FeishuThreadSelectionEntry{
				{
					ThreadID: "thread-current",
					Summary:  "droid · 当前会话",
					Current:  true,
				},
				{
					ThreadID: "thread-2",
					Summary:  "droid · 整理日志",
				},
				{
					ThreadID: "thread-3",
					Summary:  "droid · 旧会话",
					Disabled: true,
				},
			},
		},
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindSelection,
		DaemonLifecycleID: "life-2",
		SelectionView:     &view,
		SelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:     control.FeishuUIDTOwnerSelection,
			PromptKind:   control.SelectionPromptUseThread,
			Title:        "最近会话",
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := renderedV2BodyElements(t, ops[0])
	var selectElement map[string]any
	for _, element := range rendered {
		if cardStringValue(element["tag"]) == "select_static" && element["name"] == cardSelectionThreadFieldName {
			selectElement = element
			break
		}
	}
	if len(selectElement) == 0 {
		t.Fatalf("expected vscode thread view to render one dropdown, got %#v", rendered)
	}
	if got := cardStringValue(selectElement["initial_option"]); got != "thread-current" {
		t.Fatalf("expected current thread as initial option, got %#v", selectElement)
	}
	value := cardValueMap(selectElement)
	if value[cardActionPayloadKeyKind] != cardActionKindUseThread ||
		value[cardActionPayloadKeyFieldName] != cardSelectionThreadFieldName ||
		value[cardActionPayloadKeyDaemonLifecycleID] != "life-2" {
		t.Fatalf("unexpected dropdown callback payload: %#v", value)
	}
	var optionValues []string
	switch typed := selectElement["options"].(type) {
	case []map[string]any:
		for _, option := range typed {
			optionValues = append(optionValues, cardStringValue(option["value"]))
		}
	case []any:
		for _, raw := range typed {
			option, _ := raw.(map[string]any)
			optionValues = append(optionValues, cardStringValue(option["value"]))
		}
	}
	if strings.Join(optionValues, ",") != "thread-current,thread-2" {
		t.Fatalf("expected dropdown to omit disabled threads, got %#v", optionValues)
	}
	if !strings.Contains(renderedV2CardText(t, ops[0]), "已省略当前不可切换的会话。") {
		t.Fatalf("expected hidden-thread hint in rendered card, got %q", renderedV2CardText(t, ops[0]))
	}
}

func TestProjectKickThreadSelectionViewUsesStructuredButtons(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptKickThread,
		KickThread: &control.FeishuKickThreadSelectionView{
			ThreadID:       "thread-1",
			ThreadLabel:    "droid · 修复登录流程",
			ThreadSubtitle: "/data/dl/droid\n已被其他飞书会话占用，可强踢",
			Hint:           "只有对方当前空闲时才能强踢；确认前会再次校验状态。",
			CancelLabel:    "取消",
			ConfirmLabel:   "强踢并占用",
		},
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindSelection,
		DaemonLifecycleID: "life-kick",
		SelectionView:     &view,
		SelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:   control.FeishuUIDTOwnerSelection,
			PromptKind: control.SelectionPromptKickThread,
			Title:      "强踢当前会话？",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardTitle != "强踢当前会话？" {
		t.Fatalf("unexpected kick-thread title: %#v", ops[0])
	}
	rendered := renderedV2BodyElements(t, ops[0])
	if !strings.Contains(renderedV2CardText(t, ops[0]), "已被其他飞书会话占用，可强踢") {
		t.Fatalf("expected kick-thread subtitle in plain text block, got %q", renderedV2CardText(t, ops[0]))
	}
	var sawConfirm bool
	for _, element := range rendered {
		if cardStringValue(element["tag"]) != "button" && cardStringValue(element["tag"]) != "column_set" {
			continue
		}
		for _, button := range cardElementButtons(t, element) {
			if cardButtonLabel(t, button) != "强踢并占用" {
				continue
			}
			value := renderedButtonCallbackValue(t, button)
			if value[cardActionPayloadKeyKind] != cardActionKindKickThreadConfirm || value[cardActionPayloadKeyThreadID] != "thread-1" || value[cardActionPayloadKeyDaemonLifecycleID] != "life-kick" {
				t.Fatalf("unexpected kick confirm payload: %#v", value)
			}
			sawConfirm = true
		}
	}
	if !sawConfirm {
		t.Fatalf("expected structured kick-thread confirm button, got %#v", rendered)
	}
}
