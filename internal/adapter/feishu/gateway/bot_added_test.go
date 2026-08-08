package gateway

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestParseBotAddedToGroupEventBuildsPrimaryBootstrapAction(t *testing.T) {
	event := &larkim.P2ChatMemberBotAddedV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    "evt-bot-added-1",
				EventType:  "im.chat.member.bot.added_v1",
				CreateTime: "1710000000000",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-bot-added-1"},
			},
		},
		Event: &larkim.P2ChatMemberBotAddedV1Data{
			ChatId:     stringRef("oc_chat"),
			OperatorId: &larkim.UserId{OpenId: stringRef("ou_operator")},
		},
	}

	action, ok := ParseBotAddedToGroupEvent(InboundEnv{GatewayID: "app-1"}, event)
	if !ok {
		t.Fatal("expected bot added event to parse")
	}
	if action.Kind != control.ActionFeishuBotAddedToGroup {
		t.Fatalf("kind = %q, want bot-added action", action.Kind)
	}
	if action.GatewayID != "app-1" || action.ChatID != "oc_chat" || action.ActorUserID != "ou_operator" {
		t.Fatalf("unexpected action routing: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:chat:oc_chat" {
		t.Fatalf("surface = %q, want chat surface", action.SurfaceSessionID)
	}
	if action.Inbound == nil {
		t.Fatalf("expected inbound meta: %#v", action)
	}
	if action.Inbound.EventID != "evt-bot-added-1" || action.Inbound.EventType != "im.chat.member.bot.added_v1" || action.Inbound.RequestID != "req-bot-added-1" {
		t.Fatalf("unexpected inbound meta: %#v", action.Inbound)
	}
	if !action.Inbound.EventCreateTime.Equal(time.UnixMilli(1710000000000).UTC()) {
		t.Fatalf("event create time = %s", action.Inbound.EventCreateTime)
	}
}

func TestParseBotAddedToGroupEventRejectsMissingChat(t *testing.T) {
	action, ok := ParseBotAddedToGroupEvent(InboundEnv{GatewayID: "app-1"}, &larkim.P2ChatMemberBotAddedV1{
		Event: &larkim.P2ChatMemberBotAddedV1Data{},
	})
	if ok || action.Kind != "" {
		t.Fatalf("expected missing chat event to be ignored, got %#v", action)
	}
}
