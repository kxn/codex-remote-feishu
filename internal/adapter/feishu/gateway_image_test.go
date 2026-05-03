package feishu

import (
	"context"
	"encoding/json"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApplySendImageRepliesToSourceMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		uploadPath     string
		replyMessageID string
		replyMsgType   string
		replyContent   string
		createCalled   bool
	)
	gateway.uploadImagePathFn = func(_ context.Context, path string) (string, error) {
		uploadPath = path
		return "img-key-1", nil
	}
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
				MessageId: stringRef("om-image-1"),
			},
		}, nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		createCalled = true
		return nil, nil
	}

	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendImage,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ReplyToMessageID: "om-source-1",
		ImagePath:        "/tmp/generated.png",
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if createCalled {
		t.Fatalf("expected reply path without fallback create")
	}
	if uploadPath != "/tmp/generated.png" {
		t.Fatalf("unexpected uploaded image path: %q", uploadPath)
	}
	if replyMessageID != "om-source-1" || replyMsgType != "image" {
		t.Fatalf("unexpected image reply request: message=%q type=%q", replyMessageID, replyMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(replyContent), &payload); err != nil {
		t.Fatalf("image reply content is not valid json: %v", err)
	}
	if payload["image_key"] != "img-key-1" {
		t.Fatalf("unexpected image reply payload: %#v", payload)
	}
	if gateway.messages["om-image-1"] != "surface-1" {
		t.Fatalf("expected replied image message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}
