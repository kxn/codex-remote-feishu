package feishu

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestApplySendImageFallsBackToBase64WhenSavedPathUploadFails(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		pathCalls     int
		uploadedBytes []byte
		createMsgType string
		createContent string
	)
	gateway.uploadImagePathFn = func(_ context.Context, _ string) (string, error) {
		pathCalls++
		return "", errors.New("missing file")
	}
	gateway.uploadImageBytesFn = func(_ context.Context, data []byte) (string, error) {
		uploadedBytes = append([]byte(nil), data...)
		return "img-key-2", nil
	}
	gateway.createMessageFn = func(_ context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		if receiveIDType != "chat_id" || receiveID != "oc_1" {
			t.Fatalf("unexpected receive target: type=%q id=%q", receiveIDType, receiveID)
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
				MessageId: stringRef("om-image-2"),
			},
		}, nil
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("fake-image"))
	err := gateway.Apply(t.Context(), []Operation{{
		Kind:             OperationSendImage,
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		ReceiveID:        "oc_1",
		ReceiveIDType:    "chat_id",
		ImagePath:        "/tmp/missing.png",
		ImageBase64:      encoded,
	}})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if pathCalls != 1 {
		t.Fatalf("expected one saved-path upload attempt, got %d", pathCalls)
	}
	if string(uploadedBytes) != "fake-image" {
		t.Fatalf("unexpected uploaded fallback bytes: %q", uploadedBytes)
	}
	if createMsgType != "image" {
		t.Fatalf("unexpected image message type: %q", createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("image create content is not valid json: %v", err)
	}
	if payload["image_key"] != "img-key-2" {
		t.Fatalf("unexpected image create payload: %#v", payload)
	}
	if gateway.messages["om-image-2"] != "surface-1" {
		t.Fatalf("expected created image message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
}
