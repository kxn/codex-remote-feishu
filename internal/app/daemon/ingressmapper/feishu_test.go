package ingressmapper

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestCardUsesActualVersionTokens(t *testing.T) {
	raw := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "chat", MessageID: "message", Kind: "card", ReceivedAt: time.Now().UTC()}
	for _, tc := range []struct {
		values    map[string]any
		kind, ref string
	}{{map[string]any{"team_view": "team-1"}, "team_view", "team-1"}, {map[string]any{"private_view": "private-1", "daemon_lifecycle_id": "process"}, "private_view", "private-1"}, {map[string]any{"daemon_lifecycle_id": "old-process"}, "", ""}, {map[string]any{"team_setup_ingress": "ingress-1"}, "team_setup_ingress", "ingress-1"}} {
		raw.Values = tc.values
		e, err := Card(raw, 1)
		if err != nil || e.CardKind != tc.kind || e.CardRef != tc.ref {
			t.Fatalf("got %+v %v", e, err)
		}
	}
	raw.Values = map[string]any{"team_view": "a", "private_view": "b"}
	if _, err := Card(raw, 1); err == nil {
		t.Fatal("mixed owner tokens accepted")
	}
}
