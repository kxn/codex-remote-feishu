package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectCommandCatalogSupportsPatchableUpdateAndTheme(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventFeishuDirectCommandCatalog,
		SurfaceSessionID: "surface-1",
		FeishuDirectCommandCatalog: &control.FeishuDirectCommandCatalog{
			Title:       "Upgrade",
			Summary:     "正在检查升级。",
			MessageID:   "om-card-1",
			Patchable:   true,
			ThemeKey:    "progress",
			Interactive: false,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one op, got %#v", ops)
	}
	if ops[0].Kind != OperationUpdateCard || ops[0].MessageID != "om-card-1" {
		t.Fatalf("expected update-card op, got %#v", ops[0])
	}
	if !ops[0].CardUpdateMulti {
		t.Fatalf("expected patchable command catalog to keep update_multi, got %#v", ops[0])
	}
	if ops[0].CardThemeKey != "progress" {
		t.Fatalf("unexpected theme key: %#v", ops[0])
	}
}

func TestCommandCatalogButtonsSupportCallbackActions(t *testing.T) {
	elements := commandCatalogElements(control.FeishuDirectCommandCatalog{
		Interactive: true,
		RelatedButtons: []control.CommandCatalogButton{{
			Label: "确认升级",
			Kind:  control.CommandCatalogButtonCallbackAction,
			CallbackValue: map[string]any{
				"kind":      "upgrade_owner_flow",
				"picker_id": "flow-1",
				"option_id": "confirm",
			},
		}},
	}, "life-1")

	actions := cardActionsFromElements(elements)
	if len(actions) != 1 {
		t.Fatalf("expected one callback action button, got %#v", elements)
	}
	value := cardValueMap(actions[0])
	if value[cardActionPayloadKeyKind] != "upgrade_owner_flow" {
		t.Fatalf("unexpected callback kind: %#v", value)
	}
	if value[cardActionPayloadKeyPickerID] != "flow-1" || value[cardActionPayloadKeyOptionID] != "confirm" {
		t.Fatalf("unexpected callback payload: %#v", value)
	}
	if value[cardActionPayloadKeyDaemonLifecycleID] != "life-1" {
		t.Fatalf("expected daemon lifecycle stamp, got %#v", value)
	}
}

func TestCommandCatalogButtonsSupportOpenURLActions(t *testing.T) {
	elements := commandCatalogElements(control.FeishuDirectCommandCatalog{
		Interactive: true,
		RelatedButtons: []control.CommandCatalogButton{{
			Label:   "打开配置表",
			Kind:    control.CommandCatalogButtonOpenURL,
			OpenURL: "https://example.com/cron",
		}},
	}, "life-1")

	actions := cardActionsFromElements(elements)
	if len(actions) != 1 {
		t.Fatalf("expected one open-url action button, got %#v", elements)
	}
	behaviors, _ := actions[0]["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "open_url" {
		t.Fatalf("expected open_url behavior, got %#v", actions[0])
	}
	if behaviors[0]["default_url"] != "https://example.com/cron" {
		t.Fatalf("unexpected open_url target: %#v", behaviors[0])
	}
}
