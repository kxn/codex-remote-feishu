package daemon

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

func (a *App) sendIMVideoTool(ctx context.Context, arguments map[string]any) (map[string]any, *toolError) {
	path, _ := arguments["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, &toolError{
			Code:    "path_required",
			Message: "path is required",
		}
	}
	if apiErr := validateSendVideoPath(path); apiErr != nil {
		return nil, apiErr
	}

	a.mu.Lock()
	resolved, apiErr := a.resolveToolCallerSurfaceContextLocked(toolCallerInstanceIDFromContext(ctx))
	a.mu.Unlock()
	if apiErr != nil {
		return nil, apiErr
	}
	if !resolved.Attached {
		return nil, &toolError{
			Code:    "surface_not_attached",
			Message: "surface is not attached to a workspace",
		}
	}

	sender, ok := a.gateway.(feishu.IMVideoSender)
	if !ok {
		return nil, &toolError{
			Code:    "tool_unavailable",
			Message: "Feishu IM video sending is not available in this runtime",
		}
	}
	result, err := sender.SendIMVideo(ctx, feishu.IMVideoSendRequest{
		GatewayID:        resolved.GatewayID,
		SurfaceSessionID: resolved.SurfaceSessionID,
		ChatID:           resolved.ChatID,
		ActorUserID:      resolved.ActorUserID,
		Path:             path,
	})
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var sendErr *feishu.IMVideoSendError
		if errors.As(err, &sendErr) {
			switch sendErr.Code {
			case feishu.IMVideoSendErrorUploadFailed:
				return nil, &toolError{Code: "upload_failed", Message: sendErr.Error()}
			case feishu.IMVideoSendErrorSendFailed, feishu.IMVideoSendErrorMissingReceiveTarget, feishu.IMVideoSendErrorGatewayNotRunning:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			}
		}
		return nil, &toolError{
			Code:      "send_failed",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	log.Printf("tool call: tool=%s surface=%s path=%s status=ok message=%s", feishuSendIMVideoToolName, resolved.SurfaceSessionID, path, result.MessageID)
	return map[string]any{
		"surface_session_id": result.SurfaceSessionID,
		"gateway_id":         result.GatewayID,
		"video_name":         result.VideoName,
		"file_key":           result.FileKey,
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
