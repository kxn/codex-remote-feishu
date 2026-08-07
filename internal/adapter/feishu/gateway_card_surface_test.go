package feishu

import (
	"testing"

	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParseCardActionTriggerEventUsesPayloadSurfaceWhenMessageMapMisses(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := cardActionSurfaceEvent("feishu:app-1:user:user-1", "user-1", "oc_1", "om-card-rebuilt")

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected card callback to be parsed from payload surface")
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" {
		t.Fatalf("unexpected card callback surface: %#v", action)
	}
}

func TestParseCardActionTriggerEventUsesPayloadChatSurfaceWhenMessageMapMisses(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := cardActionSurfaceEvent("feishu:app-1:chat:oc_1", "user-1", "oc_1", "om-card-rebuilt")

	action, ok := gateway.parseCardActionTriggerEvent(event)
	if !ok {
		t.Fatal("expected chat card callback to be parsed from payload surface")
	}
	if action.SurfaceSessionID != "feishu:app-1:chat:oc_1" {
		t.Fatalf("unexpected chat card callback surface: %#v", action)
	}
}

func TestParseCardActionTriggerEventRejectsUnknownSurfaceWhenMessageMapMisses(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := cardActionSurfaceEvent("", "user-1", "oc_1", "om-card-unknown")

	if action, ok := gateway.parseCardActionTriggerEvent(event); ok {
		t.Fatalf("expected callback without reliable surface to be ignored, got %#v", action)
	}
}

func TestParseCardActionTriggerEventRejectsUntrustedPayloadSurfaceWhenMessageMapMisses(t *testing.T) {
	cases := []struct {
		name             string
		gatewayID        string
		payloadSurfaceID string
		operatorID       string
		chatID           string
	}{
		{
			name:             "different gateway",
			gatewayID:        "app-1",
			payloadSurfaceID: "feishu:app-2:user:user-1",
			operatorID:       "user-1",
			chatID:           "oc_1",
		},
		{
			name:             "different user actor",
			gatewayID:        "app-1",
			payloadSurfaceID: "feishu:app-1:user:user-2",
			operatorID:       "user-1",
			chatID:           "oc_1",
		},
		{
			name:             "different chat",
			gatewayID:        "app-1",
			payloadSurfaceID: "feishu:app-1:chat:oc_2",
			operatorID:       "user-1",
			chatID:           "oc_1",
		},
		{
			name:             "malformed surface",
			gatewayID:        "app-1",
			payloadSurfaceID: "feishu:app-1:user",
			operatorID:       "user-1",
			chatID:           "oc_1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: tc.gatewayID})
			event := cardActionSurfaceEvent(tc.payloadSurfaceID, tc.operatorID, tc.chatID, "om-card-rebuilt")

			if action, ok := gateway.parseCardActionTriggerEvent(event); ok {
				t.Fatalf("expected untrusted payload surface to be ignored, got %#v", action)
			}
		})
	}
}

func cardActionSurfaceEvent(surfaceID, operatorID, chatID, messageID string) *larkcallback.CardActionTriggerEvent {
	userID := "user-1"
	if operatorID != "" {
		userID = operatorID
	}
	value := map[string]interface{}{
		"kind": "show_all_workspaces",
	}
	if surfaceID != "" {
		value[frontstagecontract.CardActionPayloadKeySurfaceSessionID] = surfaceID
	}
	event := &larkcallback.CardActionTriggerEvent{
		Event: &larkcallback.CardActionTriggerRequest{
			Operator: &larkcallback.Operator{UserID: &userID},
			Action: &larkcallback.CallBackAction{
				Value: value,
			},
			Context: &larkcallback.Context{
				OpenChatID:    chatID,
				OpenMessageID: messageID,
			},
		},
	}
	return event
}
