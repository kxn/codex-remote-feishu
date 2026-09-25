package feishu

import (
	"context"
	"encoding/json"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApplySendCardRepliesWithInCardAttention(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyMessageID string
		replyMsgType   string
		replyContent   string
	)
	gateway.replyMessageFn = func(_ context.Context, messageID, msgType, content string, replyInThread bool) (*larkim.ReplyMessageResp, error) {
		replyMessageID = messageID
		replyMsgType = msgType
		replyContent = content
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.ReplyMessageRespData{
				MessageId: stringRef("om-final-attention-1"),
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		CardTitle:        "需要确认",
		CardThemeKey:     cardThemeApproval,
		AttentionText:    "需要你回来处理：请确认这条请求。",
		AttentionUserID:  "ou-user-1",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if replyMessageID != "om-source-1" || replyMsgType != "interactive" {
		t.Fatalf("unexpected attention card reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("reply attention card content is not valid json: %v", err)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]any)
	if len(elements) == 0 {
		t.Fatalf("expected attention card payload to render body elements, got %#v", payload)
	}
	first, _ := elements[0].(map[string]any)
	if first["tag"] != "markdown" || first["content"] != "<at id=ou-user-1></at>" {
		t.Fatalf("unexpected attention card payload: %#v", payload)
	}
}

func TestApplySendCardAttentionFallsBackToCreate(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyCalls    int
		createCalls   int
		createMsgType string
		createContent string
	)
	gateway.replyMessageFn = func(_ context.Context, _, _, _ string, _ bool) (*larkim.ReplyMessageResp, error) {
		replyCalls++
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 230001,
				Msg:  "message not found",
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		createCalls++
		if receiveIDType != "chat_id" || receiveID != "oc_1" {
			t.Fatalf("unexpected attention fallback receive target: type=%q id=%q", receiveIDType, receiveID)
		}
		createMsgType = msgType
		createContent = content
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{
				MessageId: stringRef("om-final-attention-2"),
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendCard,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		CardTitle:        "链路错误",
		CardThemeKey:     cardThemeError,
		AttentionText:    "需要你回来处理：飞书投递失败。",
		AttentionUserID:  "ou-user-1",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if replyCalls != 1 || createCalls != 1 {
		t.Fatalf("expected one attention reply attempt and one fallback create, got reply=%d create=%d", replyCalls, createCalls)
	}
	if createMsgType != "interactive" {
		t.Fatalf("unexpected attention fallback message type: %q", createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("fallback attention card content is not valid json: %v", err)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]any)
	if len(elements) == 0 {
		t.Fatalf("expected fallback attention card payload to render body elements, got %#v", payload)
	}
	first, _ := elements[0].(map[string]any)
	if first["tag"] != "markdown" || first["content"] != "<at id=ou-user-1></at>" {
		t.Fatalf("unexpected fallback attention card payload: %#v", payload)
	}
}
