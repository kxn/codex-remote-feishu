package feishu

import (
	"testing"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestParseCardActionTriggerEventBuildsUpgradeOwnerFlowAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-card-1", "feishu:app-1:user:user-1")
	userID := "user-1"
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: map[string]interface{}{
					"kind":      "upgrade_owner_flow",
					"picker_id": "flow-1",
					"option_id": "confirm",
				},
			},
			Context: &larkcallback.Context{
				OpenChatID:    "oc_1",
				OpenMessageID: "om-card-1",
			},
		},
	}

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed")
	}
	if action.Kind != control.ActionUpgradeOwnerFlow {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.PickerID != "flow-1" || action.OptionID != "confirm" {
		t.Fatalf("unexpected upgrade owner payload: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ChatID != "oc_1" || action.ActorUserID != "user-1" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
}
