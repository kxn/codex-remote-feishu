package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestSendIMImageUploadsThenCreatesImageMessage(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		uploadPath     string
		uploadCtx      context.Context
		createMsgType  string
		createContent  string
		createTargetID string
		createTargetTy string
		createCtx      context.Context
	)
	gateway.uploadImagePathFn = func(ctx context.Context, path string) (string, error) {
		uploadCtx = ctx
		uploadPath = path
		return "img-key-1", nil
	}
	gateway.createMessageFn = func(ctx context.Context, receiveIDType, receiveID, msgType, content string) (*larkim.CreateMessageResp, error) {
		createCtx = ctx
		createTargetTy = receiveIDType
		createTargetID = receiveID
		createMsgType = msgType
		createContent = content
		return &larkim.CreateMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.CreateMessageRespData{
				MessageId: stringRef("om-image-1"),
			},
		}, nil
	}

	result, err := gateway.SendIMImage(t.Context(), IMImageSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/preview.png",
	})
	if err != nil {
		t.Fatalf("SendIMImage returned error: %v", err)
	}
	if uploadPath != "/tmp/preview.png" {
		t.Fatalf("unexpected uploaded image path: %q", uploadPath)
	}
	if createTargetTy != "chat_id" || createTargetID != "oc_1" || createMsgType != "image" {
		t.Fatalf("unexpected create message target: type=%q id=%q msgType=%q", createTargetTy, createTargetID, createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("image create content is not valid json: %v", err)
	}
	if payload["image_key"] != "img-key-1" {
		t.Fatalf("unexpected image create payload: %#v", payload)
	}
	if result.ImageKey != "img-key-1" || result.ImageName != "preview.png" || result.MessageID != "om-image-1" {
		t.Fatalf("unexpected send result: %#v", result)
	}
	if gateway.messages["om-image-1"] != "surface-1" {
		t.Fatalf("expected created image message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
	assertContextHasDeadlineWithin(t, uploadCtx, sendIMFileTimeout)
	assertContextHasDeadlineWithin(t, createCtx, sendIMFileTimeout)
}

func TestSendIMImageReturnsUploadFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadImagePathFn = func(_ context.Context, _ string) (string, error) {
		return "", errors.New("bad image")
	}

	_, err := gateway.SendIMImage(t.Context(), IMImageSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/preview.png",
	})
	var sendErr *IMImageSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMImageSendErrorUploadFailed {
		t.Fatalf("expected upload failure, got %#v", err)
	}
}

func TestSendIMImageReturnsSendFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadImagePathFn = func(_ context.Context, _ string) (string, error) {
		return "img-key-1", nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		return nil, errors.New("network error")
	}

	_, err := gateway.SendIMImage(t.Context(), IMImageSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/preview.png",
	})
	var sendErr *IMImageSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMImageSendErrorSendFailed {
		t.Fatalf("expected send failure, got %#v", err)
	}
}

func assertContextHasDeadlineWithin(t *testing.T, ctx context.Context, max time.Duration) {
	t.Helper()
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("expected future deadline, got %s", deadline)
	}
	if remaining > max+time.Second {
		t.Fatalf("expected deadline within %s, got remaining %s", max, remaining)
	}
}
