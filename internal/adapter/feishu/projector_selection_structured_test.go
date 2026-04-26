package feishu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectInstanceSelectionViewUsesStructuredButtons(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind:       control.SelectionPromptAttachInstance,
		CatalogFamilyID:  control.FeishuCommandList,
		CatalogVariantID: "list.codex.vscode",
		CatalogBackend:   "codex",
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
			if value[cardActionPayloadKeyCatalogFamilyID] != control.FeishuCommandList ||
				value[cardActionPayloadKeyCatalogVariantID] != "list.codex.vscode" ||
				value[cardActionPayloadKeyCatalogBackend] != "codex" {
				t.Fatalf("expected instance button to carry catalog provenance, got %#v", value)
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
		PromptKind:       control.SelectionPromptUseThread,
		CatalogFamilyID:  control.FeishuCommandUseAll,
		CatalogVariantID: "useall.codex.vscode",
		CatalogBackend:   "codex",
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
	if value[cardActionPayloadKeyCatalogFamilyID] != control.FeishuCommandUseAll ||
		value[cardActionPayloadKeyCatalogVariantID] != "useall.codex.vscode" ||
		value[cardActionPayloadKeyCatalogBackend] != "codex" {
		t.Fatalf("expected dropdown callback payload to carry catalog provenance: %#v", value)
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

func TestProjectVSCodeThreadSelectionViewPaginatesLargeDropdown(t *testing.T) {
	projector := NewProjector()
	entries := make([]control.FeishuThreadSelectionEntry, 0, 160)
	for i := 0; i < 160; i++ {
		entries = append(entries, control.FeishuThreadSelectionEntry{
			ThreadID: fmt.Sprintf("thread-%02d", i),
			Summary:  fmt.Sprintf("droid · 会话 %02d %s", i, strings.Repeat("日志聚合 ", 120)),
			Current:  i == 0,
		})
	}
	view := control.FeishuSelectionView{
		PromptKind:       control.SelectionPromptUseThread,
		CatalogFamilyID:  control.FeishuCommandUseAll,
		CatalogVariantID: "useall.codex.vscode",
		CatalogBackend:   "codex",
		Thread: &control.FeishuThreadSelectionView{
			Mode:   control.FeishuThreadSelectionVSCodeAll,
			Cursor: 60,
			CurrentInstance: &control.FeishuThreadSelectionInstanceContext{
				Label:  "droid",
				Status: "当前跟随中",
			},
			Entries: entries,
		},
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:              eventcontract.KindSelection,
		DaemonLifecycleID: "life-3",
		SelectionView:     &view,
		SelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:     control.FeishuUIDTOwnerSelection,
			PromptKind:   control.SelectionPromptUseThread,
			Title:        "当前实例全部会话",
			ContextTitle: "当前实例",
			ContextText:  "droid · 当前跟随中",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	rendered := renderedV2BodyElements(t, ops[0])

	var row map[string]any
	for _, element := range rendered {
		if cardStringValue(element["tag"]) == "column_set" {
			row = element
			break
		}
	}
	if len(row) == 0 {
		t.Fatalf("expected paginated dropdown row, got %#v", rendered)
	}
	columns, _ := row["columns"].([]map[string]any)
	if len(columns) != 3 {
		t.Fatalf("expected prev/select/next columns, got %#v", row)
	}
	prev := columns[0]["elements"].([]map[string]any)[0]
	next := columns[2]["elements"].([]map[string]any)[0]
	selectElement := columns[1]["elements"].([]map[string]any)[0]

	prevValue := renderedButtonCallbackValue(t, prev)
	if prevValue[cardActionPayloadKeyKind] != "thread_selection_page" ||
		prevValue[cardActionPayloadKeyViewMode] != string(control.FeishuThreadSelectionVSCodeAll) ||
		prevValue[cardActionPayloadKeyDaemonLifecycleID] != "life-3" {
		t.Fatalf("unexpected prev payload: %#v", prevValue)
	}
	nextValue := renderedButtonCallbackValue(t, next)
	if nextValue[cardActionPayloadKeyKind] != "thread_selection_page" ||
		nextValue[cardActionPayloadKeyViewMode] != string(control.FeishuThreadSelectionVSCodeAll) ||
		nextValue[cardActionPayloadKeyDaemonLifecycleID] != "life-3" {
		t.Fatalf("unexpected next payload: %#v", nextValue)
	}
	if got := cardStringValue(selectElement["initial_option"]); got != "" {
		t.Fatalf("expected off-page current thread to clear initial option, got %#v", selectElement)
	}
	value := cardValueMap(selectElement)
	if value[cardActionPayloadKeyKind] != cardActionKindUseThread ||
		value[cardActionPayloadKeyFieldName] != cardSelectionThreadFieldName ||
		value[cardActionPayloadKeyDaemonLifecycleID] != "life-3" {
		t.Fatalf("unexpected dropdown callback payload: %#v", value)
	}
	if value[cardActionPayloadKeyCatalogFamilyID] != control.FeishuCommandUseAll ||
		value[cardActionPayloadKeyCatalogVariantID] != "useall.codex.vscode" ||
		value[cardActionPayloadKeyCatalogBackend] != "codex" {
		t.Fatalf("expected dropdown callback payload to carry catalog provenance: %#v", value)
	}
	if !strings.Contains(renderedV2CardText(t, ops[0]), "超出卡片大小，如未找到请翻页。") {
		t.Fatalf("expected pagination hint in rendered card, got %q", renderedV2CardText(t, ops[0]))
	}
}

func TestProjectKickThreadSelectionViewUsesStructuredButtons(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptKickThread,
		KickThread: &control.FeishuKickThreadSelectionView{
			ThreadID:     "thread-1",
			ThreadLabel:  "droid · 修复登录流程",
			Hint:         "只有对方当前空闲时才能强踢；确认前会再次校验状态。",
			CancelLabel:  "取消",
			ConfirmLabel: "强踢并占用",
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
