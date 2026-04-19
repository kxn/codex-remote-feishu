package feishu

import (
	"context"
	"encoding/json"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApplySendTextRepliesToSourceMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyMessageID string
		replyMsgType   string
		replyContent   string
		createCalled   bool
	)
	gateway.replyMessageFn = func(_ context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
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
				MessageId: stringRef("om-text-1"),
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		createCalled = true
		return nil, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendText,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		Text:             "继续处理 Linux 部分。",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected reply path without fallback create")
	}
	if replyMessageID != "om-source-1" || replyMsgType != "text" {
		t.Fatalf("unexpected text reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("text reply content is not valid json: %v", err)
	}
	if payload["text"] != "继续处理 Linux 部分。" {
		t.Fatalf("unexpected text reply payload: %#v", payload)
	}
	if gateway.messages["om-text-1"] != "surface-1" {
		t.Fatalf("expected replied text message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}

func TestApplySendTextFallsBackToCreateWhenReplyFails(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		replyCalls    int
		createCalls   int
		createMsgType string
		createContent string
	)
	gateway.replyMessageFn = func(_ context.Context, _, _, _ string) (*larkim.ReplyMessageResp, error) {
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
			t.Fatalf("unexpected fallback receive target: type=%q id=%q", receiveIDType, receiveID)
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
				MessageId: stringRef("om-text-2"),
			},
		}, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendText,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		Text:             "继续处理 Linux 部分。",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if replyCalls != 1 || createCalls != 1 {
		t.Fatalf("expected one reply attempt and one fallback create, got reply=%d create=%d", replyCalls, createCalls)
	}
	if createMsgType != "text" {
		t.Fatalf("unexpected fallback message type: %q", createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("fallback text content is not valid json: %v", err)
	}
	if payload["text"] != "继续处理 Linux 部分。" {
		t.Fatalf("unexpected fallback text payload: %#v", payload)
	}
	if gateway.messages["om-text-2"] != "surface-1" {
		t.Fatalf("expected fallback text message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}
