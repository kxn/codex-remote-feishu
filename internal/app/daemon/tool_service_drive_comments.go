package daemon

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
)

const (
	toolDriveFileCommentsDefaultPageSize = 20
	toolDriveFileCommentsMaxPageSize     = 100
)

type driveFileCommentsToolResult struct {
	SurfaceSessionID string `json:"surface_session_id"`
	feishu.DriveFileCommentReadResult
}

func driveFileCommentsToolInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"surface_session_id": map[string]any{
				"type":        "string",
				"description": "Feishu surface session id loaded from .codex-remote/surface-context.json",
			},
			"file_token": map[string]any{
				"type":        "string",
				"description": "Feishu Drive file token for the file whose comments should be read",
			},
			"file_type": map[string]any{
				"type":        "string",
				"description": "Feishu Drive file type. V1 supports doc, docx, sheet, file, and slides.",
				"enum":        []string{"doc", "docx", "sheet", "file", "slides"},
			},
			"page_token": map[string]any{
				"type":        "string",
				"description": "Optional pagination token returned by a previous call",
			},
			"page_size": map[string]any{
				"type":        "integer",
				"description": "Optional page size for the current comments page. Defaults to 20 and is capped at 100.",
				"minimum":     1,
				"maximum":     toolDriveFileCommentsMaxPageSize,
			},
		},
		"required":             []string{"surface_session_id", "file_token", "file_type"},
		"additionalProperties": false,
	}
}

func (a *App) readDriveFileCommentsTool(ctx context.Context, arguments map[string]any) (any, *toolError) {
	surfaceID, _ := arguments["surface_session_id"].(string)
	fileToken, _ := arguments["file_token"].(string)
	fileType, _ := arguments["file_type"].(string)
	pageToken, _ := arguments["page_token"].(string)

	fileToken = strings.TrimSpace(fileToken)
	if fileToken == "" {
		return nil, &toolError{
			Code:    "file_token_required",
			Message: "file_token is required",
		}
	}
	fileType = strings.ToLower(strings.TrimSpace(fileType))
	if !feishuDriveFileCommentTypeSupported(fileType) {
		return nil, &toolError{
			Code:    "unsupported_file_type",
			Message: "file_type must be one of: doc, docx, sheet, file, slides",
		}
	}
	pageSize, apiErr := toolOptionalIntegerArgument(arguments, "page_size", toolDriveFileCommentsDefaultPageSize, 1, toolDriveFileCommentsMaxPageSize)
	if apiErr != nil {
		return nil, apiErr
	}

	a.mu.Lock()
	resolved, resolveErr := a.resolveToolSurfaceContextLocked(surfaceID)
	a.mu.Unlock()
	if resolveErr != nil {
		return nil, resolveErr
	}
	if !resolved.Attached {
		return nil, &toolError{
			Code:    "surface_not_attached",
			Message: "surface is not attached to a workspace",
		}
	}

	reader, ok := a.gateway.(feishu.DriveFileCommentReader)
	if !ok {
		return nil, &toolError{
			Code:    "tool_unavailable",
			Message: "Feishu Drive comment reading is not available in this runtime",
		}
	}
	result, err := reader.ReadDriveFileComments(ctx, feishu.DriveFileCommentReadRequest{
		GatewayID: resolved.GatewayID,
		FileToken: fileToken,
		FileType:  fileType,
		PageToken: strings.TrimSpace(pageToken),
		PageSize:  pageSize,
	})
	if err != nil {
		_ = a.observeFeishuPermissionError(resolved.GatewayID, err)
		var readErr *feishu.DriveFileCommentReadError
		if errors.As(err, &readErr) && readErr.Code == feishu.DriveFileCommentReadErrorInvalidFileType {
			return nil, &toolError{
				Code:    "unsupported_file_type",
				Message: readErr.Error(),
			}
		}
		retryable := true
		if _, ok := feishu.ExtractPermissionGap(err); ok {
			retryable = false
		}
		return nil, &toolError{
			Code:      "read_failed",
			Message:   err.Error(),
			Retryable: retryable,
		}
	}
	return driveFileCommentsToolResult{
		SurfaceSessionID:           resolved.SurfaceSessionID,
		DriveFileCommentReadResult: result,
	}, nil
}

func feishuDriveFileCommentTypeSupported(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "doc", "docx", "sheet", "file", "slides":
		return true
	default:
		return false
	}
}

func toolOptionalIntegerArgument(arguments map[string]any, key string, defaultValue, minValue, maxValue int) (int, *toolError) {
	raw, ok := arguments[key]
	if !ok || raw == nil {
		return defaultValue, nil
	}
	var value int
	switch typed := raw.(type) {
	case float64:
		if math.Trunc(typed) != typed {
			return 0, &toolError{
				Code:    "invalid_" + key,
				Message: key + " must be an integer",
			}
		}
		value = int(typed)
	case int:
		value = typed
	case int32:
		value = int(typed)
	case int64:
		value = int(typed)
	default:
		return 0, &toolError{
			Code:    "invalid_" + key,
			Message: key + " must be an integer",
		}
	}
	if value < minValue || value > maxValue {
		return 0, &toolError{
			Code:    "invalid_" + key,
			Message: key + " is out of range",
		}
	}
	return value, nil
}
