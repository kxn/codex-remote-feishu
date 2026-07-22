package gateway

import (
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestPlanInboundMessageEventRemovedLegacyCompatQueuesPlainTextMessage(t *testing.T) {
	env := InboundEnv{
		GatewayID:                     "app-2",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			t.Helper()
			if messageID != "om-msg-compat" {
				t.Fatalf("unexpected recorded message id: %q", messageID)
			}
			if surfaceSessionID == "" {
				t.Fatal("expected surface session id to be recorded")
			}
		},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-compat"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":" /newinstance "}`),
				Mentions: []*larkim.MentionEvent{{
					Key: stringRef("@_user_1"),
					Id:  &larkim.UserId{OpenId: stringRef("ou_bot")},
				}},
			},
		},
	}

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected removed compat command to remain handled")
	}
	if planned.Action != nil {
		t.Fatalf("expected removed compat command to avoid direct action fallback, got %#v", planned.Action)
	}
	if planned.Queue == nil {
		t.Fatal("expected removed compat command to queue as plain text work")
	}
	if planned.Queue.messageType != "text" || strings.TrimSpace(planned.Queue.text) != "/newinstance" {
		t.Fatalf("unexpected queued plain text work: %#v", planned.Queue)
	}
}

func TestPlanInboundMessageEventIgnoresGroupTextWhenNotMentionedCurrentBot(t *testing.T) {
	recorded := false
	env := InboundEnv{
		GatewayID:                     "app-2",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = true
		},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-other-bot"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"@_user_1 你好"}`),
				Mentions: []*larkim.MentionEvent{{
					Key: stringRef("@_user_1"),
					Id:  &larkim.UserId{OpenId: stringRef("ou_other_bot")},
				}},
			},
		},
	}

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if ok || planned.Action != nil || planned.Queue != nil || recorded {
		t.Fatalf("expected non-target group mention to be ignored without recording, ok=%v planned=%#v recorded=%v", ok, planned, recorded)
	}
}

func TestParseMessageEventIgnoresGroupTextWhenNotMentionedCurrentBot(t *testing.T) {
	env := InboundEnv{
		GatewayID:                     "app-2",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			t.Fatalf("expected non-target group mention not to record surface message")
		},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-other-bot-sync"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"@_user_1 你好"}`),
				Mentions: []*larkim.MentionEvent{{
					Key: stringRef("@_user_1"),
					Id:  &larkim.UserId{OpenId: stringRef("ou_other_bot")},
				}},
			},
		},
	}

	action, ok, err := ParseMessageEvent(t.Context(), env, event)
	if err != nil {
		t.Fatalf("ParseMessageEvent returned error: %v", err)
	}
	if ok || action.Kind != "" {
		t.Fatalf("expected non-target group mention to be ignored, ok=%v action=%#v", ok, action)
	}
}

func TestPlanInboundMessageEventIgnoresGroupTextWithoutBotIdentity(t *testing.T) {
	env := InboundEnv{
		GatewayID:                     "app-2",
		ParseTextActionWithoutCatalog: parseTextAction,
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			t.Fatalf("expected group message without bot identity not to record surface message")
		},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-no-identity"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"@_user_1 你好"}`),
				Mentions: []*larkim.MentionEvent{{
					Key: stringRef("@_user_1"),
					Id:  &larkim.UserId{OpenId: stringRef("ou_bot")},
				}},
			},
		},
	}

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if ok || planned.Action != nil || planned.Queue != nil {
		t.Fatalf("expected group message without bot identity to be ignored, ok=%v planned=%#v", ok, planned)
	}
}

func TestPlanInboundMessageEventKeepsP2PWithoutMention(t *testing.T) {
	env := InboundEnv{
		GatewayID:                     "app-2",
		ParseTextActionWithoutCatalog: parseTextAction,
		RecordSurfaceMessage:          func(messageID, surfaceSessionID string) {},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-p2p"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"你好"}`),
			},
		},
	}

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if !ok || planned.Queue == nil {
		t.Fatalf("expected p2p message without mention to be queued, ok=%v planned=%#v", ok, planned)
	}
}

func stringRef(value string) *string {
	return &value
}
