package feishu

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestDurableInteractionReturnsCommittedRawCardAndKeepsTeamToast(t *testing.T) {
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	event := cardActionSurfaceEvent("surface", "user", "chat", "message")
	event.EventV2Base = teamEventBase()
	g.rawIngressHook = &RawIngressHook{ReceiveInteractionResponse: func(context.Context, platformingress.HumanInteractionEnvelope) (InteractionResponse, error) {
		return InteractionResponse{RawBody: json.RawMessage(`{"card":{"type":"raw","data":{"schema":"2.0","body":{"elements":[]}}}}`)}, nil
	}}
	response, handled, err := g.handleTeamCard(t.Context(), event)
	if err != nil || !handled || response == nil || response.Card == nil || response.Card.Type != "raw" {
		t.Fatalf("raw callback missing: %+v %v", response, err)
	}
	g.rawIngressHook = &RawIngressHook{ReceiveInteraction: func(context.Context, platformingress.HumanInteractionEnvelope) (string, error) {
		return "team toast", nil
	}}
	response, handled, err = g.handleTeamCard(t.Context(), event)
	if err != nil || !handled || response == nil || response.Toast == nil || response.Toast.Content != "team toast" {
		t.Fatalf("team callback changed: %+v %v", response, err)
	}
}
