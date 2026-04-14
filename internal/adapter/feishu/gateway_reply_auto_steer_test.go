package feishu

import (
	"context"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestParseMessageEventReplyTextCarriesReplyTargetAndSteerInputs(t *testing.T) {
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
	if action.TargetMessageID != "om-parent-1" {
		t.Fatalf("expected reply target message id, got %#v", action)
	}
	if len(action.SteerInputs) != 1 || action.SteerInputs[0].Type != agentproto.InputText || action.SteerInputs[0].Text != "这是回复内容" {
		t.Fatalf("unexpected steer inputs: %#v", action.SteerInputs)
	}
	if len(action.Inputs) != 2 || action.Inputs[0].Text != "<被引用内容>\n原始消息\n</被引用内容>" || action.Inputs[1].Text != "这是回复内容" {
		t.Fatalf("unexpected full reply inputs: %#v", action.Inputs)
	}
}

func TestParseMessageEventReplyImageCarriesReplyTargetAndSteerInputs(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		if messageID != "om-reply-image-1" || imageKey != "img-1" {
			t.Fatalf("unexpected image download request: message=%s image=%s", messageID, imageKey)
		}
		return "/tmp/reply-image-1.png", "image/png", nil
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-reply-image-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("image"),
				ParentId:    stringRef("om-parent-image-1"),
				Content:     stringRef(`{"image_key":"img-1"}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionImageMessage {
		t.Fatalf("expected reply image to be handled, got ok=%v action=%#v", ok, action)
	}
	if action.TargetMessageID != "om-parent-image-1" {
		t.Fatalf("expected reply target message id, got %#v", action)
	}
	if len(action.SteerInputs) != 1 || action.SteerInputs[0].Type != agentproto.InputLocalImage || action.SteerInputs[0].Path != "/tmp/reply-image-1.png" {
		t.Fatalf("unexpected steer inputs: %#v", action.SteerInputs)
	}
}

func TestParseMessageEventReplyMergeForwardDoesNotExposeSteerInputs(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		switch messageID {
		case "om-forward-1":
			return &gatewayMessage{
				MessageID:   messageID,
				MessageType: "merge_forward",
				Content:     `{"title":"Forwarded","items":[{"text":"line 1"}]}`,
			}, nil
		case "om-parent-forward-1":
			return &gatewayMessage{
				MessageID:   messageID,
				MessageType: "text",
				Content:     `{"text":"原始消息"}`,
			}, nil
		default:
			t.Fatalf("unexpected message lookup: %s", messageID)
			return nil, nil
		}
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-forward-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("merge_forward"),
				ParentId:    stringRef("om-parent-forward-1"),
				Content:     stringRef(`{"title":"Forwarded","items":[{"text":"line 1"}]}`),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok || action.Kind != control.ActionTextMessage {
		t.Fatalf("expected merge forward reply to be handled, got ok=%v action=%#v", ok, action)
	}
	if action.TargetMessageID != "om-parent-forward-1" {
		t.Fatalf("expected reply target message id, got %#v", action)
	}
	if len(action.SteerInputs) != 0 {
		t.Fatalf("expected merge forward reply to stay out of auto-steer v1, got %#v", action.SteerInputs)
	}
}
