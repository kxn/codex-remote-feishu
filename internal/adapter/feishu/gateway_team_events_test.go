package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func teamEventBase() *larkevent.EventV2Base {
	return &larkevent.EventV2Base{Header: &larkevent.EventHeader{EventID: "event", AppID: "app", TenantKey: "tenant", Token: "SECRET_CALLBACK_TOKEN"}}
}
func teamEventGateway(t *testing.T, menu func(context.Context, MenuIngressEvent) error, platform func(context.Context, PlatformIngressEvent) error) *LiveGateway {
	t.Helper()
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	err := g.SetRawIngressHook(RawIngressHook{Authorize: func(context.Context, work.RawIngressEnvelope) (bool, error) {
		t.Fatal("events must not impersonate messages")
		return false, nil
	}, Receive: func(context.Context, work.RawIngressEnvelope) error {
		t.Fatal("events must not impersonate messages")
		return nil
	}, ReceiveMenu: menu, ReceivePlatformEvent: platform})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func TestTeamMenuPreservesSDKIdentityAndPropagatesDurabilityError(t *testing.T) {
	failure := errors.New("durability unavailable")
	g := teamEventGateway(t, func(_ context.Context, v MenuIngressEvent) error {
		if v.EventID != "event" || v.Tenant != "tenant" || v.SenderIDs.OpenID != "person" || v.EventKey != "settings" {
			t.Fatalf("lost identity: %+v", v)
		}
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "chat_id") || strings.Contains(string(raw), "message_id") {
			t.Fatalf("manufactured identity or credentials: %s", raw)
		}
		return failure
	}, nil)
	event := &larkapplication.P2BotMenuV6{EventV2Base: teamEventBase(), Event: &larkapplication.P2BotMenuV6Data{Operator: &larkapplication.Operator{OperatorId: &larkapplication.UserId{OpenId: stringRef("person")}}, EventKey: stringRef("settings")}}
	handled, err := g.handleTeamMenu(t.Context(), event)
	if !handled || !errors.Is(err, failure) {
		t.Fatalf("lost durable failure %v %v", handled, err)
	}
	event.EventV2Base.Header.TenantKey = ""
	if _, err = g.handleTeamMenu(t.Context(), event); err == nil {
		t.Fatal("missing tenant accepted")
	}
	event.EventV2Base.Header.TenantKey = "tenant"
	event.EventV2Base.Header.AppID = "foreign"
	if _, err = g.handleTeamMenu(t.Context(), event); err == nil {
		t.Fatal("foreign app accepted")
	}
}
func TestTeamPlatformEventsPreserveObservedFacts(t *testing.T) {
	var got []PlatformIngressEvent
	g := teamEventGateway(t, nil, func(_ context.Context, v PlatformIngressEvent) error { got = append(got, v); return nil })
	if _, err := g.handleTeamRecall(t.Context(), &larkim.P2MessageRecalledV1{EventV2Base: teamEventBase(), Event: &larkim.P2MessageRecalledV1Data{MessageId: stringRef("message"), ChatId: stringRef("chat"), RecallType: stringRef("admin")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.handleTeamReaction(t.Context(), &larkim.P2MessageReactionCreatedV1{EventV2Base: teamEventBase(), Event: &larkim.P2MessageReactionCreatedV1Data{MessageId: stringRef("message"), OperatorType: stringRef("app"), AppId: stringRef("other-bot"), ReactionType: &larkim.Emoji{EmojiType: stringRef("OK")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.handleTeamBotAdded(t.Context(), &larkim.P2ChatMemberBotAddedV1{EventV2Base: teamEventBase(), Event: &larkim.P2ChatMemberBotAddedV1Data{ChatId: stringRef("chat"), OperatorId: &larkim.UserId{OpenId: stringRef("person")}, OperatorTenantKey: stringRef("operator-tenant")}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].OperatorType != "" || got[0].OperatorIDs != (work.SenderIDs{}) || got[1].OperatorType != "app" || got[1].OperatorAppID != "other-bot" || got[1].ChatID != "" || got[2].OperatorTenant != "operator-tenant" {
		t.Fatalf("invented/forgotten facts: %+v", got)
	}
}
func TestTeamEventsNeverFallBackWhenReceiverMissing(t *testing.T) {
	g := teamEventGateway(t, nil, nil)
	event := &larkapplication.P2BotMenuV6{EventV2Base: teamEventBase(), Event: &larkapplication.P2BotMenuV6Data{Operator: &larkapplication.Operator{OperatorId: &larkapplication.UserId{OpenId: stringRef("person")}}, EventKey: stringRef("settings")}}
	if handled, err := g.handleTeamMenu(t.Context(), event); !handled || err == nil {
		t.Fatal("missing receiver silently fell back")
	}
	g = NewLiveGateway(LiveGatewayConfig{GatewayID: "gateway", AppID: "app"})
	if handled, err := g.handleTeamMenu(t.Context(), nil); handled || err != nil {
		t.Fatal("legacy-only gateway was intercepted")
	}
}

func TestTeamUnavailableEventsPreserveBotAndChatSDKFacts(t *testing.T) {
	var got []PlatformIngressEvent
	g := teamEventGateway(t, nil, func(_ context.Context, v PlatformIngressEvent) error { got = append(got, v); return nil })
	if handled, err := g.handleTeamBotRemoved(t.Context(), &larkim.P2ChatMemberBotDeletedV1{EventV2Base: teamEventBase(), Event: &larkim.P2ChatMemberBotDeletedV1Data{ChatId: stringRef("chat"), OperatorId: &larkim.UserId{OpenId: stringRef("actual-remover")}}}); !handled || err != nil {
		t.Fatalf("%v %v", handled, err)
	}
	if handled, err := g.handleTeamChatDeleted(t.Context(), &larkim.P2ChatDisbandedV1{EventV2Base: teamEventBase(), Event: &larkim.P2ChatDisbandedV1Data{ChatId: stringRef("chat"), OperatorTenantKey: stringRef("operator-tenant")}}); !handled || err != nil {
		t.Fatalf("%v %v", handled, err)
	}
	if len(got) != 2 || got[0].Kind != "bot.removed" || got[0].AppID != "app" || got[0].ChatID != "chat" || got[0].OperatorIDs.OpenID != "actual-remover" || got[1].Kind != "chat.deleted" {
		t.Fatalf("lost actual SDK facts %+v", got)
	}
}
