package feishu

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

type mappingResolver struct{}

func (mappingResolver) Resolve(_ context.Context, e ingressowner.Event, _ *ingressowner.PlatformMessage) (ingressowner.Owner, error) {
	if e.ChatType == "p2p" {
		return ingressowner.Owner{Application: ingressowner.Private, ID: "personal", Epoch: 1}, nil
	}
	return ingressowner.Owner{Application: ingressowner.Team, ID: "project", Epoch: 1}, nil
}
func TestSDKMappingKeepsStableFactsAndFrozenOwner(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	sdk := durableIngressEvent()
	raw, ok, err := gateway.rawMessageEnvelope(sdk)
	if err != nil || !ok {
		t.Fatalf("sdk parse: %v", err)
	}
	first, err := ingressmapper.Message(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Fresh receive time, routing gateway and identity generation are local facts,
	// not part of the SDK event fingerprint.
	raw.ReceivedAt = raw.ReceivedAt.Add(time.Hour)
	raw.GatewayRef = "new-gateway"
	replay, err := ingressmapper.Message(raw, 2)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Payload) != string(replay.Payload) || first.Key != replay.Key {
		t.Fatalf("local source changed fingerprint: %s %s", first.Payload, replay.Payload)
	}
	path := filepath.Join(t.TempDir(), "owner.db")
	r, err := ingressowner.Open(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, duplicate, err := r.Admit(t.Context(), first, mappingResolver{})
	if err != nil || duplicate || saved.Owner.Application != ingressowner.Team {
		t.Fatalf("group route: %+v %v", saved, err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = ingressowner.Open(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	saved, duplicate, err = r.Admit(t.Context(), replay, mappingResolver{})
	if err != nil || !duplicate || saved.Event.Source.BindingGeneration != 1 || saved.Event.Source.GatewayID != "gateway" {
		t.Fatalf("replay: %+v %v", saved, err)
	}
	restored, err := ingressmapper.RestoreMessage(saved.Event)
	if err != nil || restored.GatewayRef != "gateway" || !restored.ReceivedAt.Equal(first.Source.ReceivedAt) {
		t.Fatalf("first source was not restored: %+v %v", restored, err)
	}
	if !restored.MembershipObservedAt().Equal(first.Source.ReceivedAt) {
		t.Fatal("membership fallback changed across replay")
	}
	sdk.EventV2Base.Header.EventID = "private-event"
	sdk.Event.Message.MessageId = stringRef("private-message")
	sdk.Event.Message.ChatId = stringRef("private-chat")
	sdk.Event.Message.ChatType = stringRef("p2p")
	raw, ok, err = gateway.rawMessageEnvelope(sdk)
	if err != nil || !ok {
		t.Fatal(err)
	}
	private, err := ingressmapper.Message(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, _, err = r.Admit(t.Context(), private, mappingResolver{})
	if err != nil || saved.Owner.Application != ingressowner.Private {
		t.Fatalf("private route: %+v %v", saved, err)
	}
	var payload map[string]any
	if err = json.Unmarshal(private.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["gateway_ref"]; ok {
		t.Fatal("source gateway leaked into fingerprint")
	}
	if _, ok := payload["received_at"]; ok {
		t.Fatal("receive time leaked into fingerprint")
	}
}
