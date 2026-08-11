package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

func (a *App) sendIMVideoTool(ctx context.Context, arguments map[string]any) (map[string]any, *toolError) {
	result, toolErr := a.sendIMMediaTool(ctx, arguments, imMediaToolSpec{
		toolName:       feishuSendIMVideoToolName,
		unavailableMsg: "Feishu IM video sending is not available in this runtime",
		validate:       validateSendVideoPath,
		send: func(ctx context.Context, args imMediaSendArgs) (imMediaSendResult, *toolError) {
			sender, ok := a.gateway.(feishu.IMVideoSender)
			if !ok {
				return imMediaSendResult{}, &toolError{
					Code:    "tool_unavailable",
					Message: "Feishu IM video sending is not available in this runtime",
				}
			}
			result, err := sender.SendIMVideo(ctx, feishu.IMVideoSendRequest{
				GatewayID:        args.GatewayID,
				SurfaceSessionID: args.SurfaceSessionID,
				ChatID:           args.ChatID,
				ActorUserID:      args.ActorUserID,
				Path:             args.Path,
			})
			if err != nil {
				_ = a.observeFeishuPermissionError(args.GatewayID, err)
				var sendErr *feishu.IMVideoSendError
				if errors.As(err, &sendErr) {
					switch sendErr.Code {
					case feishu.IMVideoSendErrorUploadFailed:
						return imMediaSendResult{}, &toolError{Code: "upload_failed", Message: sendErr.Error()}
					case feishu.IMVideoSendErrorSendFailed, feishu.IMVideoSendErrorMissingReceiveTarget, feishu.IMVideoSendErrorGatewayNotRunning:
						return imMediaSendResult{}, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
					}
				}
				return imMediaSendResult{}, &toolError{
					Code:      "send_failed",
					Message:   err.Error(),
					Retryable: true,
				}
			}
			return imMediaSendResult{
				GatewayID:        result.GatewayID,
				SurfaceSessionID: result.SurfaceSessionID,
				Name:             result.VideoName,
				Key:              result.FileKey,
				MessageID:        result.MessageID,
			}, nil
		},
	})
	if toolErr != nil {
		return nil, toolErr
	}
	return map[string]any{
		"surface_session_id": result.SurfaceSessionID,
		"gateway_id":         result.GatewayID,
		"video_name":         result.Name,
		"file_key":           result.Key,
		"message_id":         result.MessageID,
	}, nil
}

func validateSendVideoPath(path string) *toolError {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &toolError{
			Code:    "video_not_found",
			Message: "path does not exist",
		}
	case err != nil:
		return &toolError{
			Code:    "video_access_failed",
			Message: "failed to access local video",
		}
	case info.IsDir():
		return &toolError{
			Code:    "invalid_video_path",
			Message: "path must point to a video file",
		}
	}
	if strings.ToLower(strings.TrimSpace(filepath.Ext(path))) != ".mp4" {
		return &toolError{
			Code:    "invalid_video_path",
			Message: "path must point to an .mp4 video file",
		}
	}
	return nil
}
