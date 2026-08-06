package gateway

import (
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

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

func TestPlanInboundMessageEventQueuesUnmentionedGroupTextForPrimaryGateway(t *testing.T) {
	recorded := false
	env := InboundEnv{
		GatewayID:                     "app-1",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		PrimaryGatewayForChat: func(chatID string) string {
			if chatID != "oc_chat" {
				t.Fatalf("primary lookup chat = %q, want oc_chat", chatID)
			}
			return "app-1"
		},
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = true
			if messageID != "om-msg-primary" || surfaceSessionID != "feishu:app-1:chat:oc_chat" {
				t.Fatalf("record = %s/%s, want om-msg-primary/feishu:app-1:chat:oc_chat", messageID, surfaceSessionID)
			}
		},
	}
	event := groupTextEventWithoutMention("om-msg-primary", "oc_chat", "请接一下")

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if !ok || planned.Queue == nil || planned.Queue.text != "请接一下" || !recorded {
		t.Fatalf("expected primary unmentioned group text to queue and record, ok=%v planned=%#v recorded=%v", ok, planned, recorded)
	}
}

func TestPlanInboundMessageEventQueuesUnmentionedGroupTextWhenPrimaryPermissionCacheMissing(t *testing.T) {
	recorded := false
	env := InboundEnv{
		GatewayID:                     "app-1",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		PrimaryGatewayForChat: func(chatID string) string {
			return "app-1"
		},
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = true
		},
	}
	event := groupTextEventWithoutMention("om-msg-primary-missing", "oc_chat", "请接一下")

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if !ok || planned.Queue == nil || planned.Queue.text != "请接一下" || !recorded {
		t.Fatalf("expected primary unmentioned group text to queue even when permission cache is missing, ok=%v planned=%#v recorded=%v", ok, planned, recorded)
	}
}

func TestPlanInboundMessageEventIgnoresUnmentionedGroupTextForNonPrimaryGateway(t *testing.T) {
	recorded := false
	env := InboundEnv{
		GatewayID:                     "app-2",
		BotOpenID:                     "ou_bot",
		ParseTextActionWithoutCatalog: parseTextAction,
		PrimaryGatewayForChat: func(chatID string) string {
			return "app-1"
		},
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = true
		},
	}
	event := groupTextEventWithoutMention("om-msg-non-primary", "oc_chat", "请接一下")

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if ok || planned.Action != nil || planned.Queue != nil || recorded {
		t.Fatalf("expected non-primary gateway to ignore without recording, ok=%v planned=%#v recorded=%v", ok, planned, recorded)
	}
}

func TestPlanInboundMessageEventQueuesUnmentionedGroupImageAndFileForPrimaryGateway(t *testing.T) {
	recorded := []string{}
	env := InboundEnv{
		GatewayID: "app-1",
		BotOpenID: "ou_bot",
		PrimaryGatewayForChat: func(chatID string) string {
			return "app-1"
		},
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = append(recorded, messageID+"@"+surfaceSessionID)
		},
	}

	imagePlanned, imageOK, err := PlanInboundMessageEvent(env, groupMediaEventWithoutMention("om-img-primary", "oc_chat", "image", `{"image_key":"img-key"}`))
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent image returned error: %v", err)
	}
	if !imageOK || imagePlanned.Queue == nil || imagePlanned.Queue.messageType != "image" || imagePlanned.Queue.imageKey != "img-key" {
		t.Fatalf("expected primary unmentioned group image to queue, ok=%v planned=%#v", imageOK, imagePlanned)
	}

	filePlanned, fileOK, err := PlanInboundMessageEvent(env, groupMediaEventWithoutMention("om-file-primary", "oc_chat", "file", `{"file_key":"file-key","file_name":"report.md"}`))
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent file returned error: %v", err)
	}
	if !fileOK || filePlanned.Queue == nil || filePlanned.Queue.messageType != "file" || filePlanned.Queue.fileKey != "file-key" || filePlanned.Queue.fileName != "report.md" {
		t.Fatalf("expected primary unmentioned group file to queue, ok=%v planned=%#v", fileOK, filePlanned)
	}

	want := []string{
		"om-img-primary@feishu:app-1:chat:oc_chat",
		"om-file-primary@feishu:app-1:chat:oc_chat",
	}
	if strings.Join(recorded, "|") != strings.Join(want, "|") {
		t.Fatalf("recorded = %#v, want %#v", recorded, want)
	}
}

func TestPlanInboundMessageEventIgnoresUnmentionedGroupMediaForNonPrimaryBeforeParsing(t *testing.T) {
	recorded := false
	env := InboundEnv{
		GatewayID: "app-2",
		BotOpenID: "ou_bot",
		PrimaryGatewayForChat: func(chatID string) string {
			return "app-1"
		},
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			recorded = true
		},
	}

	for _, messageType := range []string{"image", "file"} {
		planned, ok, err := PlanInboundMessageEvent(env, groupMediaEventWithoutMention("om-"+messageType+"-non-primary", "oc_chat", messageType, `not-json`))
		if err != nil {
			t.Fatalf("non-primary %s should be ignored before parsing, got error: %v", messageType, err)
		}
		if ok || planned.Action != nil || planned.Queue != nil || recorded {
			t.Fatalf("expected non-primary %s to ignore without recording, ok=%v planned=%#v recorded=%v", messageType, ok, planned, recorded)
		}
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

func groupTextEventWithoutMention(messageID, chatID, text string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: stringRef("ou_user")},
				SenderType: stringRef("user"),
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef(messageID),
				ChatId:      stringRef(chatID),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"` + text + `"}`),
			},
		},
	}
}

func groupMediaEventWithoutMention(messageID, chatID, messageType, content string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId:   &larkim.UserId{OpenId: stringRef("ou_user")},
				SenderType: stringRef("user"),
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef(messageID),
				ChatId:      stringRef(chatID),
				ChatType:    stringRef("group"),
				MessageType: stringRef(messageType),
				Content:     stringRef(content),
			},
		},
	}
}

func stringRef(value string) *string {
	return &value
}
