package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type IMImageSender interface {
	SendIMImage(context.Context, IMImageSendRequest) (IMImageSendResult, error)
}

type IMImageSendRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	Path             string
}

type IMImageSendResult struct {
	GatewayID        string
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	ImageKey         string
	ImageName        string
	MessageID        string
}

type IMImageSendErrorCode string

const (
	IMImageSendErrorGatewayNotRunning    IMImageSendErrorCode = "gateway_not_running"
	IMImageSendErrorMissingReceiveTarget IMImageSendErrorCode = "missing_receive_target"
	IMImageSendErrorUploadFailed         IMImageSendErrorCode = "upload_failed"
	IMImageSendErrorSendFailed           IMImageSendErrorCode = "send_failed"
)

type IMImageSendError struct {
	Code IMImageSendErrorCode
	Err  error
}

func (e *IMImageSendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *IMImageSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (g *LiveGateway) SendIMImage(ctx context.Context, req IMImageSendRequest) (IMImageSendResult, error) {
	ctx, cancel := newFeishuTimeoutContext(ctx, sendIMFileTimeout)
	defer cancel()

	result := IMImageSendResult{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: strings.TrimSpace(req.SurfaceSessionID),
	}
	if gatewayID := normalizeGatewayID(req.GatewayID); gatewayID != "" && gatewayID != g.config.GatewayID {
		return result, &IMImageSendError{
			Code: IMImageSendErrorGatewayNotRunning,
			Err:  fmt.Errorf("send image failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID),
		}
	}
	receiveID, receiveIDType := ResolveReceiveTarget(req.ChatID, req.ActorUserID)
	if receiveID == "" || receiveIDType == "" {
		return result, &IMImageSendError{
			Code: IMImageSendErrorMissingReceiveTarget,
			Err:  fmt.Errorf("send image failed: missing receive target"),
		}
	}

	path := strings.TrimSpace(req.Path)
	imageKey, err := g.uploadImagePathFn(ctx, path)
	if err != nil {
		return result, &IMImageSendError{
			Code: IMImageSendErrorUploadFailed,
			Err:  fmt.Errorf("upload image failed: %w", err),
		}
	}

	body, _ := json.Marshal(map[string]string{"image_key": imageKey})
	resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "image", string(body))
	if err != nil {
		return result, &IMImageSendError{
			Code: IMImageSendErrorSendFailed,
			Err:  fmt.Errorf("send image failed: %w", err),
		}
	}
	if !resp.Success() {
		return result, &IMImageSendError{
			Code: IMImageSendErrorSendFailed,
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
	result.ImageKey = imageKey
	result.ImageName = strings.TrimSpace(filepath.Base(path))
	result.MessageID = messageID
	return result, nil
}
