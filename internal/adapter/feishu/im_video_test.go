package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestSendIMVideoUploadsThenCreatesMediaMessage(t *testing.T) {
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
	gateway.uploadVideoPathFn = func(ctx context.Context, path string) (string, string, error) {
		uploadCtx = ctx
		uploadPath = path
		return "file-key-1", "demo.mp4", nil
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
				MessageId: stringRef("om-video-1"),
			},
		}, nil
	}

	result, err := gateway.SendIMVideo(t.Context(), IMVideoSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/demo.mp4",
	})
	if err != nil {
		t.Fatalf("SendIMVideo returned error: %v", err)
	}
	if uploadPath != "/tmp/demo.mp4" {
		t.Fatalf("unexpected uploaded video path: %q", uploadPath)
	}
	if createTargetTy != "chat_id" || createTargetID != "oc_1" || createMsgType != "media" {
		t.Fatalf("unexpected create message target: type=%q id=%q msgType=%q", createTargetTy, createTargetID, createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("video create content is not valid json: %v", err)
	}
	if payload["file_key"] != "file-key-1" {
		t.Fatalf("unexpected video create payload: %#v", payload)
	}
	if result.FileKey != "file-key-1" || result.VideoName != "demo.mp4" || result.MessageID != "om-video-1" {
		t.Fatalf("unexpected send result: %#v", result)
	}
	if gateway.messages["om-video-1"] != "surface-1" {
		t.Fatalf("expected created video message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
	assertContextHasDeadlineWithin(t, uploadCtx, sendIMFileTimeout)
	assertContextHasDeadlineWithin(t, createCtx, sendIMFileTimeout)
}

func TestSendIMVideoReturnsUploadFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadVideoPathFn = func(_ context.Context, _ string) (string, string, error) {
		return "", "", errors.New("bad video")
	}

	_, err := gateway.SendIMVideo(t.Context(), IMVideoSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/demo.mp4",
	})
	var sendErr *IMVideoSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMVideoSendErrorUploadFailed {
		t.Fatalf("expected upload failure, got %#v", err)
	}
}

func TestSendIMVideoReturnsSendFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadVideoPathFn = func(_ context.Context, _ string) (string, string, error) {
		return "file-key-1", "demo.mp4", nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		return nil, errors.New("network error")
	}

	_, err := gateway.SendIMVideo(t.Context(), IMVideoSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/demo.mp4",
	})
	var sendErr *IMVideoSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMVideoSendErrorSendFailed {
		t.Fatalf("expected send failure, got %#v", err)
	}
}
