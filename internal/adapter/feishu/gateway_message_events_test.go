package feishu

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestParseMessageRecalledEventBuildsRecallAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-msg-1", "feishu:app-1:user:user-1")
	event := &larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: stringRef("om-msg-1"),
			ChatId:    stringRef("oc_1"),
		},
	}

	action, ok := gateway.parseMessageRecalledEvent(event)
	if !ok {
		t.Fatal("expected recalled event to be parsed")
	}
	if action.Kind != control.ActionMessageRecalled {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.GatewayID != "app-1" {
		t.Fatalf("unexpected gateway id: %#v", action)
	}
	if action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.TargetMessageID != "om-msg-1" || action.ChatID != "oc_1" {
		t.Fatalf("unexpected recalled action payload: %#v", action)
	}
}

func TestParseMessageRecalledEventIgnoresUnknownMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageRecalledV1{
		Event: &larkim.P2MessageRecalledV1Data{
			MessageId: stringRef("om-missing"),
		},
	}

	if action, ok := gateway.parseMessageRecalledEvent(event); ok || action.Kind != "" {
		t.Fatalf("expected unknown recalled message to be ignored, got %#v", action)
	}
}

func TestParseMessageReactionCreatedEventBuildsReactionAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-msg-1", "feishu:app-1:user:user-1")
	event := &larkim.P2MessageReactionCreatedV1{
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    stringRef("om-msg-1"),
			ReactionType: &larkim.Emoji{EmojiType: stringRef("ThumbsUp")},
			UserId:       &larkim.UserId{OpenId: stringRef("ou_user")},
		},
	}

	action, ok := gateway.parseMessageReactionCreatedEvent(event)
	if !ok {
		t.Fatal("expected reaction event to be parsed")
	}
	if action.Kind != control.ActionReactionCreated {
		t.Fatalf("unexpected action kind: %#v", action)
	}
	if action.GatewayID != "app-1" || action.SurfaceSessionID != "feishu:app-1:user:user-1" || action.ActorUserID != "ou_user" {
		t.Fatalf("unexpected reaction routing payload: %#v", action)
	}
	if action.TargetMessageID != "om-msg-1" || action.ReactionType != "ThumbsUp" {
		t.Fatalf("unexpected reaction payload: %#v", action)
	}
}

func TestParseMessageReactionCreatedEventIgnoresBotReactionAndUnknownMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})

	botEvent := &larkim.P2MessageReactionCreatedV1{
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    stringRef("om-msg-1"),
			ReactionType: &larkim.Emoji{EmojiType: stringRef("OneSecond")},
		},
	}
	if action, ok := gateway.parseMessageReactionCreatedEvent(botEvent); ok || action.Kind != "" {
		t.Fatalf("expected bot reaction without user id to be ignored, got %#v", action)
	}

	unknownEvent := &larkim.P2MessageReactionCreatedV1{
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    stringRef("om-missing"),
			ReactionType: &larkim.Emoji{EmojiType: stringRef("ThumbsUp")},
			UserId:       &larkim.UserId{OpenId: stringRef("ou_user")},
		},
	}
	if action, ok := gateway.parseMessageReactionCreatedEvent(unknownEvent); ok || action.Kind != "" {
		t.Fatalf("expected unknown reaction target to be ignored, got %#v", action)
	}
}

func TestParseMessageEventCarriesInboundMeta(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    "evt-msg-1",
				EventType:  "im.message.receive_v1",
				CreateTime: "1710000000000",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-msg-1"},
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-1"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"你好"}`),
				ChatType:    stringRef("p2p"),
				CreateTime:  stringRef("1710000001000"),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected message event to be parsed")
	}
	if action.Inbound == nil {
		t.Fatalf("expected inbound meta, got %#v", action)
	}
	if action.Inbound.EventID != "evt-msg-1" || action.Inbound.EventType != "im.message.receive_v1" || action.Inbound.RequestID != "req-msg-1" {
		t.Fatalf("unexpected message inbound meta: %#v", action.Inbound)
	}
	if action.Inbound.OpenMessageID != "om-msg-1" {
		t.Fatalf("unexpected open message id: %#v", action.Inbound)
	}
	if !action.Inbound.EventCreateTime.Equal(time.UnixMilli(1710000000000).UTC()) || !action.Inbound.MessageCreateTime.Equal(time.UnixMilli(1710000001000).UTC()) {
		t.Fatalf("unexpected inbound times: %#v", action.Inbound)
	}
}

func TestParseMenuEventCarriesInboundMeta(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkapplication.P2BotMenuV6{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    "evt-menu-1",
				EventType:  "application.bot.menu_v6",
				CreateTime: "1710000000000",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-menu-1"},
			},
		},
		Event: &larkapplication.P2BotMenuV6Data{
			Operator: &larkapplication.Operator{
				OperatorId: &larkapplication.UserId{UserId: stringRef("user-1")},
			},
			EventKey:  stringRef("list"),
			Timestamp: int64Ref(1710000002000),
		},
	}

	action, ok := gateway.parseMenuEvent(event)
	if !ok {
		t.Fatal("expected menu event to be parsed")
	}
	if action.Kind != control.ActionListInstances {
		t.Fatalf("unexpected menu action: %#v", action)
	}
	if action.Inbound == nil {
		t.Fatalf("expected inbound meta, got %#v", action)
	}
	if action.Inbound.EventID != "evt-menu-1" || action.Inbound.RequestID != "req-menu-1" {
		t.Fatalf("unexpected menu inbound meta: %#v", action.Inbound)
	}
	if !action.Inbound.MenuClickTime.Equal(time.UnixMilli(1710000002000).UTC()) {
		t.Fatalf("unexpected menu click time: %#v", action.Inbound)
	}
}

func TestParseMenuEventPrefersOpenIDOverUserID(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkapplication.P2BotMenuV6{
		Event: &larkapplication.P2BotMenuV6Data{
			Operator: &larkapplication.Operator{
				OperatorId: &larkapplication.UserId{
					UserId: stringRef("user-1"),
					OpenId: stringRef("ou_user"),
				},
			},
			EventKey: stringRef("list"),
		},
	}

	action, ok := gateway.parseMenuEvent(event)
	if !ok {
		t.Fatal("expected menu event to be parsed")
	}
	if action.SurfaceSessionID != "feishu:app-1:user:ou_user" {
		t.Fatalf("expected menu surface to use open id, got %#v", action)
	}
	if action.ActorUserID != "ou_user" {
		t.Fatalf("expected menu actor to use open id, got %#v", action)
	}
}

func TestParseMessageEventPrefersOpenIDOverUserIDForP2P(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{
					UserId: stringRef("user-1"),
					OpenId: stringRef("ou_user"),
				},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-open"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"你好"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected message event to be parsed")
	}
	if action.SurfaceSessionID != "feishu:app-1:user:ou_user" {
		t.Fatalf("expected p2p message surface to use open id, got %#v", action)
	}
	if action.ActorUserID != "ou_user" {
		t.Fatalf("expected message actor to use open id, got %#v", action)
	}
}

func TestParseMessageEventBuildsMixedInputsForPost(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		if messageID != "om-post-1" || imageKey != "img-post-1" {
			t.Fatalf("unexpected post image download request: message=%s image=%s", messageID, imageKey)
		}
		return "/tmp/post-1.png", "image/png", nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-post-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("post"),
				Content:     stringRef(`{"title":"","content":[[{"tag":"img","image_key":"img-post-1"}],[{"tag":"text","text":"这是图文混合消息"}]]}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected post message to become text action, got ok=%v action=%#v", ok, action)
	}
	if action.Text != "这是图文混合消息" {
		t.Fatalf("unexpected post text summary: %#v", action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected image + text inputs, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputLocalImage || action.Inputs[0].Path != "/tmp/post-1.png" {
		t.Fatalf("unexpected first post input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "这是图文混合消息" {
		t.Fatalf("unexpected second post input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventEnrichesReplyWithQuotedText(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-1" {
			t.Fatalf("unexpected parent message lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   messageID,
			MessageType: "text",
			Content:     `{"text":"原始消息"}`,
		}, nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-1"),
				Content:     stringRef(`{"text":"这是回复内容"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected quoted text + current text inputs, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<被引用内容>\n原始消息\n</被引用内容>" {
		t.Fatalf("unexpected quoted input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "这是回复内容" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventEnrichesReplyWithQuotedPost(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-post-1" {
			t.Fatalf("unexpected parent post lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   messageID,
			MessageType: "post",
			Content:     `{"title":"","content":[[{"tag":"img","image_key":"img-quoted-1"}],[{"tag":"text","text":"被引用的图文"}]]}`,
		}, nil
	}
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		if messageID != "om-parent-post-1" || imageKey != "img-quoted-1" {
			t.Fatalf("unexpected quoted post image download request: message=%s image=%s", messageID, imageKey)
		}
		return "/tmp/quoted-1.png", "image/png", nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-2"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-post-1"),
				Content:     stringRef(`{"text":"请继续处理"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 3 {
		t.Fatalf("expected quoted text + quoted image + current text, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<被引用内容>\n被引用的图文\n</被引用内容>" {
		t.Fatalf("unexpected quoted text input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputLocalImage || action.Inputs[1].Path != "/tmp/quoted-1.png" {
		t.Fatalf("unexpected quoted image input: %#v", action.Inputs[1])
	}
	if action.Inputs[2].Type != agentproto.InputText || action.Inputs[2].Text != "请继续处理" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[2])
	}
}

func TestParseMessageEventIgnoresQuoteFetchFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, _ string) (*gatewayMessage, error) {
		return nil, errors.New("lark temporary error")
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-3"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-err"),
				Content:     stringRef(`{"text":"只保留当前消息"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || len(action.Inputs) != 1 || action.Inputs[0].Text != "只保留当前消息" {
		t.Fatalf("expected current text to survive quote fetch failure, got ok=%v action=%#v", ok, action)
	}
}

func TestParseMessageEventHandlesMergeForwardMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:   "evt-forward-1",
				EventType: "im.message.receive_v1",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-forward-1"},
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-forward-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				ThreadId:    stringRef("omt-thread-1"),
				MessageType: stringRef("merge_forward"),
				RootId:      stringRef("om-root-1"),
				ParentId:    stringRef("om-parent-1"),
				Content:     stringRef(`{"title":"Forwarded chat","items":[{"text":"first line"}]}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected merge forward message to be handled, got ok=%v action=%#v", ok, action)
	}
	if action.Text != "Forwarded chat\nfirst line" {
		t.Fatalf("unexpected merge forward summary: %#v", action)
	}
	if len(action.Inputs) != 1 {
		t.Fatalf("expected single forwarded transcript input, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<转发聊天记录>\nForwarded chat\nfirst line\n</转发聊天记录>" {
		t.Fatalf("unexpected merge forward input: %#v", action.Inputs[0])
	}
}

func TestParseMessageEventHandlesMergeForwardPlainTextFallback(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-forward-plain-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("merge_forward"),
				Content:     stringRef("Merged and Forwarded Message"),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected plain-text merge forward to be handled, got ok=%v action=%#v", ok, action)
	}
	if action.Text != "Merged and Forwarded Message" {
		t.Fatalf("unexpected merge forward fallback text: %#v", action)
	}
}

func TestParseMessageEventExpandsMergeForwardFromFetchedChildren(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-forward-expand-1" {
			t.Fatalf("unexpected merge forward lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   "om-forward-expand-1",
			MessageType: "merge_forward",
			Content:     "Merged and Forwarded Message",
			Children: []*gatewayMessage{
				{MessageID: "om-forward-child-1", MessageType: "text", SenderID: "ou_user_a", SenderType: "user", Content: `{"text":"/compact"}`},
				{MessageID: "om-forward-child-2", MessageType: "text", SenderID: "ou_user_b", SenderType: "user", Content: `{"text":"已请求压缩当前线程上下文。"}`},
				{MessageID: "om-forward-child-3", MessageType: "text", SenderID: "cli_bot_1", SenderType: "app", Content: `{"text":"当前线程上下文已压缩完成。"}`},
			},
		}, nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-forward-expand-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("merge_forward"),
				Content:     stringRef("Merged and Forwarded Message"),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected fetched merge forward transcript to be handled, got ok=%v action=%#v", ok, action)
	}
	want := "用户(ou_user_a): /compact\n用户(ou_user_b): 已请求压缩当前线程上下文。\n应用(cli_bot_1): 当前线程上下文已压缩完成。"
	if action.Text != want {
		t.Fatalf("unexpected expanded merge forward text: got %q want %q", action.Text, want)
	}
}

func TestParseMessageEventQuotesMergeForwardMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-forward-1" {
			t.Fatalf("unexpected parent merge forward lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   "om-parent-forward-1",
			MessageType: "merge_forward",
			Content:     `{"title":"讨论记录","items":[{"name":"张三","text":"先看日志"},{"name":"李四","text":"确认 message_type"}]}`,
		}, nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-forward-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-forward-1"),
				Content:     stringRef(`{"text":"按这个继续查"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected quoted merge forward + current text, got %#v", action.Inputs)
	}
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != "<转发聊天记录>\n讨论记录\n张三: 先看日志\n李四: 确认 message_type\n</转发聊天记录>" {
		t.Fatalf("unexpected quoted merge forward input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "按这个继续查" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventQuotesFetchedMergeForwardMessageWithSpeakerLabels(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-parent-forward-2" {
			t.Fatalf("unexpected parent merge forward lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   "om-parent-forward-2",
			MessageType: "merge_forward",
			Content:     "Merged and Forwarded Message",
			Children: []*gatewayMessage{
				{MessageID: "om-forward-child-4", MessageType: "text", SenderID: "ou_user_a", SenderType: "user", Content: `{"text":"先看 inbound 事件"}`},
				{MessageID: "om-forward-child-5", MessageType: "text", SenderID: "ou_user_b", SenderType: "user", Content: `{"text":"然后核对 fetch 分支"}`},
			},
		}, nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-forward-2"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				ParentId:    stringRef("om-parent-forward-2"),
				Content:     stringRef(`{"text":"照这个排查"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected reply text to be handled, got ok=%v action=%#v", ok, action)
	}
	if len(action.Inputs) != 2 {
		t.Fatalf("expected quoted merge forward + current text, got %#v", action.Inputs)
	}
	wantQuoted := "<转发聊天记录>\n用户(ou_user_a): 先看 inbound 事件\n用户(ou_user_b): 然后核对 fetch 分支\n</转发聊天记录>"
	if action.Inputs[0].Type != agentproto.InputText || action.Inputs[0].Text != wantQuoted {
		t.Fatalf("unexpected quoted merge forward input: %#v", action.Inputs[0])
	}
	if action.Inputs[1].Type != agentproto.InputText || action.Inputs[1].Text != "照这个排查" {
		t.Fatalf("unexpected current text input: %#v", action.Inputs[1])
	}
}

func TestParseMessageEventLogsUnsupportedMessageType(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:   "evt-file-1",
				EventType: "im.message.receive_v1",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-file-1"},
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-file-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				ThreadId:    stringRef("omt-thread-1"),
				MessageType: stringRef("file"),
				Content:     stringRef(`{"file_name":"notes.txt"}`),
			},
		},
	}

	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	oldPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		log.SetPrefix(oldPrefix)
	}()

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if ok || action.Kind != "" {
		t.Fatalf("expected unsupported file message to be ignored, got ok=%v action=%#v", ok, action)
	}
	got := buf.String()
	for _, want := range []string{
		"feishu inbound message ignored:",
		"gateway=app-1",
		"message=om-file-1",
		"type=file",
		"thread=omt-thread-1",
		"event=evt-file-1",
		"request=req-file-1",
		"reason=unsupported_message_type",
		"notes.txt",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log output to contain %q, got %q", want, got)
		}
	}
}
