package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
)

type IMVideoSender interface {
	SendIMVideo(context.Context, IMVideoSendRequest) (IMVideoSendResult, error)
}

type IMVideoSendRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	Path             string
}

type IMVideoSendResult struct {
	GatewayID        string
	SurfaceSessionID string
	ReceiveID        string
	ReceiveIDType    string
	FileKey          string
	VideoName        string
	MessageID        string
}

type IMVideoSendErrorCode string

const (
	IMVideoSendErrorGatewayNotRunning    IMVideoSendErrorCode = "gateway_not_running"
	IMVideoSendErrorMissingReceiveTarget IMVideoSendErrorCode = "missing_receive_target"
	IMVideoSendErrorUploadFailed         IMVideoSendErrorCode = "upload_failed"
	IMVideoSendErrorSendFailed           IMVideoSendErrorCode = "send_failed"
)

type IMVideoSendError struct {
	Code IMVideoSendErrorCode
	Err  error
}

func (e *IMVideoSendError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}

func (e *IMVideoSendError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (g *LiveGateway) SendIMVideo(ctx context.Context, req IMVideoSendRequest) (IMVideoSendResult, error) {
	ctx, cancel := newFeishuTimeoutContext(ctx, sendIMFileTimeout)
	defer cancel()

	result := IMVideoSendResult{
		GatewayID:        g.config.GatewayID,
		SurfaceSessionID: strings.TrimSpace(req.SurfaceSessionID),
	}
	if gatewayID := normalizeGatewayID(req.GatewayID); gatewayID != "" && gatewayID != g.config.GatewayID {
		return result, &IMVideoSendError{
			Code: IMVideoSendErrorGatewayNotRunning,
			Err:  fmt.Errorf("send video failed: gateway mismatch: request=%s gateway=%s", gatewayID, g.config.GatewayID),
		}
	}
	receiveID, receiveIDType := gatewaypkg.ResolveReceiveTarget(req.ChatID, req.ActorUserID)
	if receiveID == "" || receiveIDType == "" {
		return result, &IMVideoSendError{
			Code: IMVideoSendErrorMissingReceiveTarget,
			Err:  fmt.Errorf("send video failed: missing receive target"),
		}
	}

	fileKey, videoName, err := g.uploadVideoPathFn(ctx, req.Path)
	if err != nil {
		return result, &IMVideoSendError{
			Code: IMVideoSendErrorUploadFailed,
			Err:  fmt.Errorf("upload video failed: %w", err),
		}
	}

	body, _ := json.Marshal(map[string]string{"file_key": fileKey})
	resp, err := g.createMessageFn(ctx, receiveIDType, receiveID, "media", string(body))
	if err != nil {
		return result, &IMVideoSendError{
			Code: IMVideoSendErrorSendFailed,
			Err:  fmt.Errorf("send video failed: %w", err),
		}
	}
	if !resp.Success() {
		return result, &IMVideoSendError{
			Code: IMVideoSendErrorSendFailed,
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
	result.VideoName = strings.TrimSpace(filepath.Base(videoName))
	result.MessageID = messageID
	return result, nil
}
