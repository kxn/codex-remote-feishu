package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const feishuSurfaceResolverToolName = "feishu_resolve_surface_context"
const feishuSendIMFileToolName = "feishu_send_im_file"
const feishuSendIMImageToolName = "feishu_send_im_image"
const feishuSendIMVideoToolName = "feishu_send_im_video"
const feishuReadDriveFileCommentsToolName = "feishu_read_drive_file_comments"

const feishuSurfaceResolverDescription = "Resolve the current Feishu remote surface context. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. If the file is missing, invalid, or you are not in normal remote workspace mode, do not guess."
const feishuSendIMFileDescription = "Send a local file to the current Feishu remote surface as an IM file attachment. Use this when the artifact should be delivered as a downloadable file rather than rendered inline. For screenshots and other user-facing images, prefer feishu_send_im_image. For MP4 videos that should render as videos in chat, prefer feishu_send_im_video. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. Use a real local file path and do not guess a surface, chat, or remote URL."
const feishuSendIMImageDescription = "Send a local image to the current Feishu remote surface as an inline IM image message. Use this proactively when you created or saved a screenshot, visual diff, rendered preview, chart, mockup, or another image artifact that would directly help the current conversation. Prefer this tool over feishu_send_im_file for PNG, JPEG, GIF, WebP, or BMP images because the image will render directly in chat. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. Use a real local image path and do not guess a surface, chat, or remote URL."
const feishuSendIMVideoDescription = "Send a local MP4 video to the current Feishu remote surface as an inline IM video message. Use this when the artifact should render as a video in chat instead of appearing as a downloadable file attachment. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found. Use a real local .mp4 file path and do not guess a surface, chat, or remote URL."
const feishuReadDriveFileCommentsDescription = "Read comments from a Feishu file or document URL. Use this when the user gives you a Feishu link, or asks you to review comments on a markdown preview link that was already uploaded to Feishu. Before calling this tool, read .codex-remote/surface-context.json from the current workspace root and pass surface_session_id exactly as found so the request uses the correct Feishu app context. Pass the exact Feishu URL; this tool will extract the token and file type for supported URL forms such as /file/, /drive/file/, /docx/, /doc/, /sheets/, and /slides/. Do not manually extract tokens, and do not guess from wiki URLs in this version."

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

type toolErrorPayload struct {
	Error toolError `json:"error"`
}

type resolvedToolSurfaceContext struct {
	SurfaceSessionID   string
	Platform           string
	GatewayID          string
	ChatID             string
	ActorUserID        string
	ProductMode        string
	AttachedInstanceID string
	SelectedThreadID   string
	RouteMode          string
	WorkspaceKey       string
	WorkspaceRoot      string
	InstanceSource     string
	InstanceManaged    bool
	Attached           bool
}

func toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        feishuSurfaceResolverToolName,
			Description: feishuSurfaceResolverDescription,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"surface_session_id": map[string]any{
						"type":        "string",
						"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
					},
				},
				"required":             []string{"surface_session_id"},
				"additionalProperties": false,
			},
		},
		{
			Name:        feishuSendIMFileToolName,
			Description: feishuSendIMFileDescription,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"surface_session_id": map[string]any{
						"type":        "string",
						"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Existing local file path to send as a Feishu IM file message",
					},
				},
				"required":             []string{"surface_session_id", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name:        feishuSendIMImageToolName,
			Description: feishuSendIMImageDescription,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"surface_session_id": map[string]any{
						"type":        "string",
						"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Existing local image path to send as a Feishu IM image message",
					},
				},
				"required":             []string{"surface_session_id", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name:        feishuSendIMVideoToolName,
			Description: feishuSendIMVideoDescription,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"surface_session_id": map[string]any{
						"type":        "string",
						"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
					},
					"path": map[string]any{
						"type":        "string",
						"description": "Existing local .mp4 path to send as a Feishu IM video message",
					},
				},
				"required":             []string{"surface_session_id", "path"},
				"additionalProperties": false,
			},
		},
		{
			Name:        feishuReadDriveFileCommentsToolName,
			Description: feishuReadDriveFileCommentsDescription,
			InputSchema: driveFileCommentsToolInputSchema(),
		},
	}
}

func (a *App) requireToolAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !adminauth.IsLoopbackRequest(r) {
			writeToolError(w, http.StatusForbidden, toolError{
				Code:    "loopback_required",
				Message: "tool service only accepts loopback requests",
			})
			return
		}
		expected := strings.TrimSpace(a.toolRuntime.BearerToken)
		if expected == "" {
			writeToolError(w, http.StatusServiceUnavailable, toolError{
				Code:    "tool_service_not_ready",
				Message: "tool service is not ready",
			})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
			writeToolError(w, http.StatusUnauthorized, toolError{
				Code:    "invalid_token",
				Message: "missing or invalid bearer token",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) resolveSurfaceContextTool(arguments map[string]any) (map[string]any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	a.mu.Lock()
	resolved, apiErr := a.resolveToolSurfaceContextLocked(surfaceID)
	a.mu.Unlock()
	if apiErr != nil {
		return nil, apiErr
	}
	result := map[string]any{
		"surface_session_id":   resolved.SurfaceSessionID,
		"platform":             resolved.Platform,
		"gateway_id":           resolved.GatewayID,
		"chat_id":              resolved.ChatID,
		"actor_user_id":        resolved.ActorUserID,
		"product_mode":         resolved.ProductMode,
		"attached_instance_id": resolved.AttachedInstanceID,
		"selected_thread_id":   resolved.SelectedThreadID,
		"route_mode":           resolved.RouteMode,
	}
	if resolved.WorkspaceKey != "" {
		result["workspace_key"] = resolved.WorkspaceKey
	}
	if resolved.WorkspaceRoot != "" {
		result["workspace_root"] = resolved.WorkspaceRoot
		result["instance_source"] = resolved.InstanceSource
		result["instance_managed"] = resolved.InstanceManaged
	}
	log.Printf("tool call: tool=%s surface=%s status=ok", feishuSurfaceResolverToolName, surfaceID)
	return result, nil
}

func (a *App) sendIMFileTool(ctx context.Context, arguments map[string]any) (map[string]any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	path, _ := arguments["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, &toolError{
			Code:    "path_required",
			Message: "path is required",
		}
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, &toolError{
			Code:    "file_not_found",
			Message: "path does not exist",
		}
	case err != nil:
		return nil, &toolError{
			Code:    "file_access_failed",
			Message: "failed to access local file",
		}
	case info.IsDir():
		return nil, &toolError{
			Code:    "invalid_file_path",
			Message: "path must point to a file",
		}
	}

	a.mu.Lock()
	resolved, apiErr := a.resolveToolSurfaceContextLocked(surfaceID)
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

	sender, ok := a.gateway.(feishu.IMFileSender)
	if !ok {
		return nil, &toolError{
			Code:    "tool_unavailable",
			Message: "Feishu IM file sending is not available in this runtime",
		}
	}
	result, err := sender.SendIMFile(ctx, feishu.IMFileSendRequest{
		GatewayID:        resolved.GatewayID,
		SurfaceSessionID: resolved.SurfaceSessionID,
		ChatID:           resolved.ChatID,
		ActorUserID:      resolved.ActorUserID,
		Path:             path,
	})
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var sendErr *feishu.IMFileSendError
		if errors.As(err, &sendErr) {
			switch sendErr.Code {
			case feishu.IMFileSendErrorUploadFailed:
				return nil, &toolError{Code: "upload_failed", Message: sendErr.Error()}
			case feishu.IMFileSendErrorSendFailed, feishu.IMFileSendErrorMissingReceiveTarget:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			case feishu.IMFileSendErrorGatewayNotRunning:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			}
		}
		return nil, &toolError{
			Code:      "send_failed",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	log.Printf("tool call: tool=%s surface=%s path=%s status=ok message=%s", feishuSendIMFileToolName, resolved.SurfaceSessionID, path, result.MessageID)
	return map[string]any{
		"surface_session_id": result.SurfaceSessionID,
		"gateway_id":         result.GatewayID,
		"file_name":          result.FileName,
		"file_key":           result.FileKey,
		"message_id":         result.MessageID,
	}, nil
}

func (a *App) sendIMImageTool(ctx context.Context, arguments map[string]any) (map[string]any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	path, _ := arguments["path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, &toolError{
			Code:    "path_required",
			Message: "path is required",
		}
	}
	if apiErr := validateSendImagePath(path); apiErr != nil {
		return nil, apiErr
	}

	a.mu.Lock()
	resolved, apiErr := a.resolveToolSurfaceContextLocked(surfaceID)
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

	sender, ok := a.gateway.(feishu.IMImageSender)
	if !ok {
		return nil, &toolError{
			Code:    "tool_unavailable",
			Message: "Feishu IM image sending is not available in this runtime",
		}
	}
	result, err := sender.SendIMImage(ctx, feishu.IMImageSendRequest{
		GatewayID:        resolved.GatewayID,
		SurfaceSessionID: resolved.SurfaceSessionID,
		ChatID:           resolved.ChatID,
		ActorUserID:      resolved.ActorUserID,
		Path:             path,
	})
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var sendErr *feishu.IMImageSendError
		if errors.As(err, &sendErr) {
			switch sendErr.Code {
			case feishu.IMImageSendErrorUploadFailed:
				return nil, &toolError{Code: "upload_failed", Message: sendErr.Error()}
			case feishu.IMImageSendErrorSendFailed, feishu.IMImageSendErrorMissingReceiveTarget, feishu.IMImageSendErrorGatewayNotRunning:
				return nil, &toolError{Code: "send_failed", Message: sendErr.Error(), Retryable: true}
			}
		}
		return nil, &toolError{
			Code:      "send_failed",
			Message:   err.Error(),
			Retryable: true,
		}
	}
	log.Printf("tool call: tool=%s surface=%s path=%s status=ok message=%s", feishuSendIMImageToolName, resolved.SurfaceSessionID, path, result.MessageID)
	return map[string]any{
		"surface_session_id": result.SurfaceSessionID,
		"gateway_id":         result.GatewayID,
		"image_name":         result.ImageName,
		"image_key":          result.ImageKey,
		"message_id":         result.MessageID,
	}, nil
}

func validateSendImagePath(path string) *toolError {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &toolError{
			Code:    "image_not_found",
			Message: "path does not exist",
		}
	case err != nil:
		return &toolError{
			Code:    "image_access_failed",
			Message: "failed to access local image",
		}
	case info.IsDir():
		return &toolError{
			Code:    "invalid_image_path",
			Message: "path must point to an image file",
		}
	}
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return nil
	default:
		return &toolError{
			Code:    "invalid_image_path",
			Message: "path must point to a supported image file",
		}
	}
}

func (a *App) resolveToolSurfaceContextLocked(surfaceID string) (resolvedToolSurfaceContext, *toolError) {
	surfaceID = strings.TrimSpace(surfaceID)
	if surfaceID == "" {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_session_id_required",
			Message: "surface_session_id is required",
		}
	}

	var surfaceRecord *state.SurfaceConsoleRecord
	for _, current := range a.service.Surfaces() {
		if current != nil && strings.TrimSpace(current.SurfaceSessionID) == surfaceID {
			surfaceRecord = current
			break
		}
	}
	if surfaceRecord == nil {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_not_found",
			Message: "surface_session_id does not exist",
		}
	}
	if state.NormalizeProductMode(surfaceRecord.ProductMode) != state.ProductModeNormal {
		return resolvedToolSurfaceContext{}, &toolError{
			Code:    "surface_mode_unsupported",
			Message: "Feishu MCP tools are only available in normal mode",
		}
	}

	resolved := resolvedToolSurfaceContext{
		SurfaceSessionID:   surfaceID,
		Platform:           strings.TrimSpace(surfaceRecord.Platform),
		GatewayID:          strings.TrimSpace(surfaceRecord.GatewayID),
		ChatID:             strings.TrimSpace(surfaceRecord.ChatID),
		ActorUserID:        strings.TrimSpace(surfaceRecord.ActorUserID),
		ProductMode:        string(state.NormalizeProductMode(surfaceRecord.ProductMode)),
		AttachedInstanceID: strings.TrimSpace(surfaceRecord.AttachedInstanceID),
		SelectedThreadID:   strings.TrimSpace(surfaceRecord.SelectedThreadID),
		RouteMode:          strings.TrimSpace(string(surfaceRecord.RouteMode)),
	}
	if snapshot := a.service.SurfaceSnapshot(surfaceID); snapshot != nil {
		resolved.WorkspaceKey = strings.TrimSpace(snapshot.WorkspaceKey)
	}
	if inst := a.service.Instance(resolved.AttachedInstanceID); inst != nil {
		resolved.Attached = true
		resolved.WorkspaceRoot = strings.TrimSpace(inst.WorkspaceRoot)
		resolved.InstanceSource = strings.TrimSpace(inst.Source)
		resolved.InstanceManaged = inst.Managed
	}
	return resolved, nil
}

func writeToolError(w http.ResponseWriter, status int, apiErr toolError) {
	writeJSON(w, status, toolErrorPayload{Error: apiErr})
}

func writeJSONFileAtomic(path string, payload any, mode os.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if err := tmpFile.Chmod(mode); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
