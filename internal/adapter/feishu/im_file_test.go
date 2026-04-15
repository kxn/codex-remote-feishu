package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestSendIMFileUploadsThenCreatesFileMessage(t *testing.T) {
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
	gateway.uploadFilePathFn = func(ctx context.Context, path string) (string, string, error) {
		uploadCtx = ctx
		uploadPath = path
		return "file-key-1", "report.txt", nil
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
				MessageId: stringRef("om-file-1"),
			},
		}, nil
	}

	result, err := gateway.SendIMFile(t.Context(), IMFileSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/report.txt",
	})
	if err != nil {
		t.Fatalf("SendIMFile returned error: %v", err)
	}
	if uploadPath != "/tmp/report.txt" {
		t.Fatalf("unexpected uploaded file path: %q", uploadPath)
	}
	if createTargetTy != "chat_id" || createTargetID != "oc_1" || createMsgType != "file" {
		t.Fatalf("unexpected create message target: type=%q id=%q msgType=%q", createTargetTy, createTargetID, createMsgType)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(createContent), &payload); err != nil {
		t.Fatalf("file create content is not valid json: %v", err)
	}
	if payload["file_key"] != "file-key-1" {
		t.Fatalf("unexpected file create payload: %#v", payload)
	}
	if result.FileKey != "file-key-1" || result.FileName != "report.txt" || result.MessageID != "om-file-1" {
		t.Fatalf("unexpected send result: %#v", result)
	}
	if gateway.messages["om-file-1"] != "surface-1" {
		t.Fatalf("expected created file message to be tracked for surface callbacks, got %#v", gateway.messages)
	}
	assertContextHasDeadlineWithin(t, uploadCtx, sendIMFileTimeout)
	assertContextHasDeadlineWithin(t, createCtx, sendIMFileTimeout)
}

func TestSendIMFileReturnsUploadFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadFilePathFn = func(_ context.Context, _ string) (string, string, error) {
		return "", "", errors.New("size exceeded")
	}

	_, err := gateway.SendIMFile(t.Context(), IMFileSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/report.txt",
	})
	var sendErr *IMFileSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMFileSendErrorUploadFailed {
		t.Fatalf("expected upload failure, got %#v", err)
	}
}

func TestSendIMFileReturnsSendFailure(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.uploadFilePathFn = func(_ context.Context, path string) (string, string, error) {
		return "file-key-1", filepath.Base(path), nil
	}
	gateway.createMessageFn = func(_ context.Context, _, _, _, _ string) (*larkim.CreateMessageResp, error) {
		return nil, errors.New("network error")
	}

	_, err := gateway.SendIMFile(t.Context(), IMFileSendRequest{
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "oc_1",
		Path:             "/tmp/report.txt",
	})
	var sendErr *IMFileSendError
	if !errors.As(err, &sendErr) || sendErr.Code != IMFileSendErrorSendFailed {
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
