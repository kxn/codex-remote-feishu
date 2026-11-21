package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"sync/atomic"
	"testing"
	"time"
)

func durableIngressEvent() *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "event", AppID: "app", TenantKey: "tenant", Token: "MUST_NOT_PERSIST_CALLBACK_TOKEN"}}, Event: &larkim.P2MessageReceiveV1Data{Sender: &larkim.EventSender{SenderType: stringRef("user"), TenantKey: stringRef("tenant"), SenderId: &larkim.UserId{OpenId: stringRef("open"), UserId: stringRef("user")}}, Message: &larkim.EventMessage{MessageId: stringRef("message"), ChatId: stringRef("group"), ChatType: stringRef("group"), MessageType: stringRef("text"), Content: stringRef(`{"text":"/attach ordinary group input"}`)}}}
}
func TestDurableIngressDoesNotReturnBeforePersistenceAndNeverRunsLegacyLane(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app", BotOpenID: "bot"})
	entered := make(chan struct{})
	commit := make(chan struct{})
	done := make(chan error, 1)
	var legacy atomic.Int32
	if err := gateway.SetRawIngressHook(RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) { return true, nil }, Receive: func(_ context.Context, envelope work.RawIngressEnvelope) error {
		close(entered)
		<-commit
		data, _ := json.Marshal(envelope)
		if string(data) == "" || envelope.EventID != "event" || envelope.SenderIDs.UserID != "user" {
			t.Error("lost minimal actual SDK metadata")
		}
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	go func() {
		done <- gateway.handleMessageIngress(t.Context(), durableIngressEvent(), func() error { legacy.Add(1); return nil })
	}()
	<-entered
	select {
	case err := <-done:
		t.Fatalf("ACK returned before commit: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(commit)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if legacy.Load() != 0 {
		t.Fatal("message entered both durable and legacy slash owners")
	}
}
func TestDurableIngressStorageFailureAndAdmissionFailureAreNotSuccess(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	sentinel := errors.New("storage unavailable")
	if err := gateway.SetRawIngressHook(RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) { return true, nil }, Receive: func(context.Context, work.RawIngressEnvelope) error { return sentinel }}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.handleMessageIngress(t.Context(), durableIngressEvent(), func() error { t.Fatal("legacy fallback after failed durability"); return nil }); !errors.Is(err, sentinel) {
		t.Fatalf("storage failure swallowed: %v", err)
	}
	var received bool
	if err := gateway.SetRawIngressHook(RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) { return false, nil }, Receive: func(context.Context, work.RawIngressEnvelope) error { received = true; return nil }}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.handleMessageIngress(t.Context(), durableIngressEvent(), func() error { t.Fatal("denied input went to old owner"); return nil }); err != nil {
		t.Fatal(err)
	}
	if received {
		t.Fatal("cutover bypassed admission policy")
	}
}
func TestControllerFactoryCarriesDurableHookWithoutNetwork(t *testing.T) {
	hook := &RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) { return true, nil }, Receive: func(context.Context, work.RawIngressEnvelope) error { return nil }}
	controller := NewMultiGatewayController()
	runtime := controller.newGateway(GatewayAppConfig{GatewayID: "gateway", AppID: "app", RawIngressHook: hook})
	gateway, ok := runtime.(*LiveGateway)
	if !ok || gateway.rawIngressHook == nil || gateway.rawIngressHook.Receive == nil {
		t.Fatal("controller reload factory dropped durable hook")
	}
	event := durableIngressEvent()
	event.EventV2Base.Header.AppID = "another-app"
	if _, _, err := gateway.rawMessageEnvelope(event); err == nil {
		t.Fatal("SDK app mismatch accepted")
	}
}
