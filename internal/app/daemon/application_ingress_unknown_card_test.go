package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type acceptedUnknownCardGateway struct{ calls int }

func (*acceptedUnknownCardGateway) Start(context.Context, feishu.ActionHandler) error { return nil }
func (g *acceptedUnknownCardGateway) Apply(context.Context, []feishu.Operation) error {
	g.calls++
	return errors.New("fixture accepted the card; response connection lost")
}

// The sender has no platform message ID after a connection loss. A later real
// SDK callback supplies a candidate ID, then the production Feishu message API
// must prove the entire frozen card before that callback can navigate it.
func TestPrivateUnknownMenuReadbackThenSDKCallbackJourney(t *testing.T) {
	app, _ := newApplicationRuntimeTestApp(t)
	r := app.applications
	lost := &acceptedUnknownCardGateway{}
	app.gateway = lost
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "unknown-menu", MessageID: "menu-input", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := r.runner.pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	r.runner.effectWG.Wait()
	pending, err := r.journal.PendingHostEffects(t.Context(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("menu effect: %v %#v", err, pending)
	}
	item := pending[0]
	if _, err = r.publisher.Publish(t.Context(), item.Owner, item.Generation, item.Effect.Effect.ID); !errors.Is(err, errPrivatePublicationUnknown) {
		t.Fatalf("unknown send: %v", err)
	}
	publication, err := r.journal.LoadPublication(t.Context(), item.Owner, item.Generation, item.Effect.Effect.ID)
	if err != nil || len(publication.Operations) != 1 || publication.Operations[0].State != "unknown" {
		t.Fatalf("publication uncertainty: %v %#v", err, publication)
	}
	card, err := feishu.PrivateOperationCard(publication.Operations[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err = json.Unmarshal(card, &document); err != nil {
		t.Fatal(err)
	}
	var callback map[string]any
	var find func(any)
	find = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			if value["private_view"] != nil && value["kind"] != nil && callback == nil {
				callback = value
			}
			for _, child := range value {
				find(child)
			}
		case []any:
			for _, child := range value {
				find(child)
			}
		}
	}
	find(document)
	if callback == nil {
		t.Fatal("no immutable callback token")
	}
	var readCount atomic.Int32
	var reject atomic.Bool
	reject.Store(true)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(request.URL.Path, "tenant_access_token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "fixture-token", "expire": 7200})
		case strings.Contains(request.URL.Path, "/im/v1/messages/platform-observed"):
			readCount.Add(1)
			if request.URL.Query().Get("card_msg_content_type") != "user_card_content" {
				t.Error("readback used lossy rendered card")
			}
			sender := "app"
			if reject.Load() {
				sender = "foreign-app"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{map[string]any{"message_id": "platform-observed", "chat_id": "private-chat", "msg_type": "interactive", "body": map[string]any{"content": string(card)}, "sender": map[string]any{"id": sender, "id_type": "app_id", "sender_type": "app"}}}}})
		default:
			http.Error(w, "no live websocket in fixture", http.StatusServiceUnavailable)
		}
	}))
	defer api.Close()
	controller := feishu.NewMultiGatewayController()
	if err = controller.UpsertApp(t.Context(), feishu.GatewayAppConfig{GatewayID: "gateway", AppID: "app", AppSecret: "fixture-secret", BotOpenID: "bot", Enabled: true, Domain: api.URL}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- controller.Start(ctx, func(context.Context, control.Action) *feishu.ActionResult { return nil })
	}()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err = controller.ProjectGateway("gateway", "app"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	app.gateway = controller
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "unknown-click-foreign", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: "platform-observed", Kind: "card", Values: callback, ReceivedAt: time.Now()}
	if rejected, err := r.hook.ReceiveInteractionResponse(t.Context(), click); err != nil || !strings.Contains(string(rejected.RawBody), "菜单未改变") {
		t.Fatalf("foreign Bot readback did not retain menu: %v %s", err, rejected.RawBody)
	}
	if _, err = r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", "platform-observed"); err == nil {
		t.Fatal("rejected readback registered ownership")
	}
	reject.Store(false)
	click.EventID = "unknown-click-valid"
	response, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
	if err != nil || len(response.RawBody) == 0 {
		t.Fatalf("recovered same-card navigation: %v %#v", err, response)
	}
	if _, err = r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", "platform-observed"); err != nil {
		t.Fatal(err)
	}
	// The normal publisher can finish the original journal effect after the
	// click, without resending the card or overwriting its newer navigation.
	if _, err = r.publisher.Publish(t.Context(), item.Owner, item.Generation, item.Effect.Effect.ID); err != nil {
		t.Fatalf("finish recovered original publication: %v", err)
	}
	again, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
	if err != nil || string(again.RawBody) != string(response.RawBody) {
		t.Fatalf("callback replay: %v", err)
	}
	if lost.calls != 1 || readCount.Load() != 2 {
		t.Fatalf("replayed send or readback: sends=%d reads=%d", lost.calls, readCount.Load())
	}
}

func TestObservedPrivateCardRequiresActualBotChatMessageAndFrozenToken(t *testing.T) {
	frozen := json.RawMessage(`{"body":{"elements":[{"behaviors":[{"type":"callback","value":{"private_view":"opaque","action":"open"}}]}]}}`)
	actual := feishu.ObservedPublication{MessageID: "message", ChatID: "chat", MessageType: "interactive", SenderType: "app", SenderIDType: "app_id", SenderID: "bot", Content: string(frozen)}
	if err := verifyObservedPrivateCard(frozen, actual, "bot", "chat", "message"); err != nil {
		t.Fatal(err)
	}
	bad := actual
	bad.SenderID = "other"
	if verifyObservedPrivateCard(frozen, bad, "bot", "chat", "message") == nil {
		t.Fatal("forged sender accepted")
	}
	bad = actual
	bad.MessageID = "other"
	if verifyObservedPrivateCard(frozen, bad, "bot", "chat", "message") == nil {
		t.Fatal("wrong message accepted")
	}
	bad = actual
	bad.Content = `{"body":{"elements":[{"behaviors":[{"type":"callback","value":{"private_view":"other","action":"open"}}]}]}}`
	if verifyObservedPrivateCard(frozen, bad, "bot", "chat", "message") == nil {
		t.Fatal("different token accepted")
	}
}
