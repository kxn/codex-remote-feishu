package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
)

type IMFileSender interface {
	SendIMFile(context.Context, IMFileSendRequest) (IMFileSendResult, error)
}

type IMFileSendRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	Path             string
}

type IMFileSendResult struct {
	GatewayID        string
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	FileKey          string
	FileName         string
	MessageID        string
}

type IMFileSendErrorCode string

const (
	IMFileSendErrorGatewayNotRunning    IMFileSendErrorCode = "gateway_not_running"
	IMFileSendErrorMissingReceiveTarget IMFileSendErrorCode = "missing_receive_target"
	IMFileSendErrorUploadFailed         IMFileSendErrorCode = "upload_failed"
	IMFileSendErrorSendFailed           IMFileSendErrorCode = "send_failed"
)

type IMFileSendError struct {
	Code IMFileSendErrorCode
	Err  error
}

func (e *IMFileSendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *IMFileSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (g *LiveGateway) SendIMFile(ctx context.Context, req IMFileSendRequest) (IMFileSendResult, error) {
	ctx, cancel := newFeishuTimeoutContext(ctx, sendIMFileTimeout)
	defer cancel()

	result := IMFileSendResult{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: strings.TrimSpace(req.SurfaceSessionID),
	}
	if gatewayID := normalizeGatewayID(req.GatewayID); gatewayID != "" && gatewayID != g.config.GatewayID {
		return result, &IMFileSendError{
			Code: IMFileSendErrorGatewayNotRunning,
			Err:  fmt.Errorf("send file failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID),
		}
	}
	receiveID, receiveIDType := gatewaypkg.ResolveReceiveTarget(req.ChatID, req.ActorUserID)
	if receiveID == "" || receiveIDType == "" {
		return result, &IMFileSendError{
			Code: IMFileSendErrorMissingReceiveTarget,
			Err:  fmt.Errorf("send file failed: missing receive target"),
		}
	}
	fileKey, fileName, err := g.uploadFilePathFn(ctx, req.Path)
	if err != nil {
		var sendErr *IMFileSendError
		if errors.As(err, &sendErr) {
			return result, sendErr
		}
		return result, &IMFileSendError{
			Code: IMFileSendErrorUploadFailed,
			Err:  fmt.Errorf("upload file failed: %w", err),
		}
	}
	body, _ := json.Marshal(map[string]string{"file_key": fileKey})
	resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "file", string(body))
	if err != nil {
		return result, &IMFileSendError{
			Code: IMFileSendErrorSendFailed,
			Err:  fmt.Errorf("send file failed: %w", err),
		}
	}
	if !resp.Success() {
		return result, &IMFileSendError{
			Code: IMFileSendErrorSendFailed,
			Err:  newAPIError("im.v1.message.create", resp.ApiResp, resp.CodeError),
		}
	}
	messageID := ""
	if resp.Data != nil {
		messageID = strings.TrimSpace(stringPtr(resp.Data.MessageId))
		g.recordSurfaceMessage(messageID, result.SurfaceSessionID)
	}
	result.ReceiveID = receiveID
	result.ReceiveIDType = receiveIDType
	result.FileKey = fileKey
	result.FileName = fileName
	result.MessageID = messageID
	return result, nil
}
