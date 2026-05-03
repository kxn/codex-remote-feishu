package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventBuildsLocalPageAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-local-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":               "page_local_action",
					"action_kind":        string(control.ActionShowCommandMenu),
					"action_arg":         "send_settings",
					"catalog_family_id":  "menu",
					"catalog_variant_id": "menu.default",
					"catalog_backend":    "codex",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-local-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected page_local_action callback to be parsed")
	}
	if action.Kind != control.ActionShowCommandMenu || action.Text != "/menu send_settings" {
		t.Fatalf("unexpected action payload: %#v", action)
	}
	if !action.LocalPageAction {
		t.Fatalf("expected local page action marker, got %#v", action)
	}
	if action.CatalogFamilyID != "" || action.CatalogVariantID != "" || action.CatalogBackend != "" {
		t.Fatalf("did not expect catalog provenance on local page action, got %#v", action)
	}
}

func TestParseCardActionTriggerEventBuildsLocalPageSubmitActionFromFormValue(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-form-local-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":               "page_local_submit",
					"action_kind":        string(control.ActionShowCommandMenu),
					"action_arg_prefix":  "",
					"field_name":         "command_args",
					"catalog_family_id":  "menu",
					"catalog_variant_id": "menu.default",
					"catalog_backend":    "codex",
				},
				FormValue: map[string]interface{}{
					"command_args": "send_settings",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-form-local-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected page_local_submit callback to be parsed")
	}
	if action.Kind != control.ActionShowCommandMenu || action.Text != "/menu send_settings" {
		t.Fatalf("unexpected local form submit action: %#v", action)
	}
	if !action.LocalPageAction {
		t.Fatalf("expected local page submit marker, got %#v", action)
	}
	if action.CatalogFamilyID != "" || action.CatalogVariantID != "" || action.CatalogBackend != "" {
		t.Fatalf("did not expect catalog provenance on local page submit, got %#v", action)
	}
}
