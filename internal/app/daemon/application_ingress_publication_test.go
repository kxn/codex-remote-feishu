package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"time"
)

type publicationBindingVerifier struct{}

func (publicationBindingVerifier) VerifyPrivateBinding(_ context.Context, _ ingressowner.PrivateBinding, proof []byte) error {
	if string(proof) != "verified-p2p" {
		return errors.New("unverified")
	}
	return nil
}
func TestPrivatePublicationRegistersOnlyConfirmedActualMessage(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	gateway := &privatePublishGateway{}
	host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	o := privatechat.Owner{GatewayID: "g", SurfaceID: gatewaypkg.SurfaceIDForInbound("g", "", "p2p", "user"), ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "bot"}
	j, err := privatechat.OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	binding := ingressowner.PrivateBinding{Tenant: "tenant", AppID: "bot", GatewayID: "g", BindingGeneration: 1, Operator: ingressowner.Operator{IDType: "open_id", ID: "user"}, ChatID: "chat", Owner: ingressowner.Owner{Application: ingressowner.Private, ID: "opaque", Epoch: 1}}
	adapter := &privatePublicationBinding{journal: j, bindingFor: func(context.Context, privatechat.Owner, int64) (ingressowner.PrivateBinding, error) {
		return binding, nil
	}}
	router, err := ingressowner.Open(filepath.Join(root, "ingress.db"), adapter, publicationBindingVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	adapter.router = router
	if err = router.RecordPrivateBinding(ctx, binding, []byte("verified-p2p")); err != nil {
		t.Fatal(err)
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	processor, err := privatechat.NewProcessor(app, j, privateActionDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	publisher := &privatePublisher{host: host, processor: processor, journal: j, ownership: adapter}
	action := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := processor.ApplyIngress(ctx, o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatalf("effects: %v", err)
	}
	id := effects[0].Effect.ID
	if err = adapter.ConfirmOperation(ctx, o, 1, id, 0); err == nil {
		t.Fatal("unpublished operation bound")
	}
	if _, err = publisher.Publish(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	message, err := router.PublishedMessage(ctx, "tenant", "bot", "chat", "platform-1")
	if err != nil || message.Owner != binding.Owner || message.CardKind != "private_view" || message.CardRef == "" {
		t.Fatalf("missing verified message binding: %+v %v", message, err)
	}
	if err = adapter.ConfirmOperation(ctx, o, 1, id, 0); err != nil {
		t.Fatalf("receipt replay: %v", err)
	}
	forged := message
	forged.MessageID = "other"
	proof, _ := json.Marshal(privatePublicationProof{Owner: o, Generation: 1, EffectID: id, Ordinal: 0})
	if err = adapter.VerifyPlatformReceipt(ctx, forged, proof); err == nil {
		t.Fatal("forged platform ID accepted")
	}
	// Route a real stamped callback through neutral owner, SDK parser and the
	// private application's durable admission; replay returns the same response.
	publication, err := j.LoadPublication(ctx, o, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(publication.Operations[0].Body, &document); err != nil {
		t.Fatal(err)
	}
	var callback map[string]any
	expectedToken := message.CardRef
	var find func(any)
	find = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["private_view"] == expectedToken && n["kind"] != nil {
				callback = n
			}
			for _, x := range n {
				find(x)
			}
		case []any:
			for _, x := range n {
				find(x)
			}
		}
	}
	find(document)
	if callback == nil {
		t.Fatal("stamped menu callback absent")
	}
	entry := &applicationIngress{host: host, router: router, resolver: applicationOwnerResolver{router: router}, private: privateApplicationAdmitter{host: host, router: router, journal: j, processor: processor, wake: func() {}, refresh: func(context.Context, *privatechat.Processor, privatechat.Owner, int64) error { return nil }}, team: noTeamAdmission{}}
	rawCard := platformingress.HumanInteractionEnvelope{GatewayRef: "g", AppID: "bot", EventID: "click", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "chat", MessageID: "platform-1", Kind: "card", Values: callback, ReceivedAt: time.Now()}
	event, err := ingressmapper.Card(rawCard, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := entry.deliver(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if first.State != ingressowner.Accepted {
		t.Fatalf("card not admitted: %+v", first)
	}
	if first.Receipt == nil || first.Receipt.Replacement == nil {
		t.Fatal("menu replacement missing private token")
	}
	expectedToken = first.Receipt.Replacement.Token
	callback = nil
	if err = json.Unmarshal(first.Response, &document); err != nil {
		t.Fatal(err)
	}
	find(document)
	if callback == nil {
		t.Fatal("replacement callback absent")
	}
	forgedCard := rawCard
	forgedCard.EventID = "foreign-click"
	forgedCard.Values = callback
	forgedCard.SenderIDs.OpenID = "other-user"
	foreign, e := ingressmapper.Card(forgedCard, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = entry.deliver(ctx, foreign); !errors.Is(e, ingressowner.ErrConflict) {
		t.Fatalf("foreign user was allowed: %v", e)
	}
	again, err := entry.deliver(ctx, event)
	if err != nil || string(again.Response) != string(first.Response) {
		t.Fatalf("callback replay: %v", err)
	}

}
