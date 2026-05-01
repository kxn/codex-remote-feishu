package daemon

import (
	"context"
	"errors"
	"math"
	"net/url"
	"path"
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
			"url": map[string]any{
				"type":        "string",
				"description": "Full Feishu file or document URL whose comments should be read",
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
		"required":             []string{"url"},
		"additionalProperties": true,
	}
}

func (a *App) readDriveFileCommentsTool(ctx context.Context, arguments map[string]any) (any, *toolError) {
	rawURL, _ := arguments["url"].(string)
	pageToken, _ := arguments["page_token"].(string)

	fileToken, fileType, apiErr := parseDriveCommentsURL(rawURL)
	if apiErr != nil {
		return nil, apiErr
	}
	pageSize, apiErr := toolOptionalIntegerArgument(arguments, "page_size", toolDriveFileCommentsDefaultPageSize, 1, toolDriveFileCommentsMaxPageSize)
	if apiErr != nil {
		return nil, apiErr
	}

	a.mu.Lock()
	resolved, resolveErr := a.resolveToolCallerSurfaceContextLocked(toolCallerInstanceIDFromContext(ctx))
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

func parseDriveCommentsURL(raw string) (string, string, *toolError) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", &toolError{
			Code:    "url_required",
			Message: "url is required",
		}
	}

	parsed, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", "", &toolError{
			Code:    "invalid_url",
			Message: "url must be a valid absolute Feishu URL",
		}
	}
	if !isSupportedFeishuURLHost(parsed.Hostname()) {
		return "", "", &toolError{
			Code:    "unsupported_document_url",
			Message: "url must point to a supported Feishu file or document",
		}
	}

	segments := normalizedURLPathSegments(parsed.Path)
	if len(segments) < 2 {
		return "", "", &toolError{
			Code:    "unsupported_document_url",
			Message: "url must point to a supported Feishu file or document",
		}
	}
	switch {
	case len(segments) == 2 && segments[0] == "file":
		return segments[1], "file", nil
	case len(segments) == 3 && segments[0] == "drive" && segments[1] == "file":
		return segments[2], "file", nil
	case len(segments) == 2 && segments[0] == "docx":
		return segments[1], "docx", nil
	case len(segments) == 2 && segments[0] == "doc":
		return segments[1], "doc", nil
	case len(segments) == 2 && segments[0] == "sheets":
		return segments[1], "sheet", nil
	case len(segments) == 2 && segments[0] == "slides":
		return segments[1], "slides", nil
	case len(segments) == 2 && segments[0] == "wiki":
		return "", "", &toolError{
			Code:    "unsupported_document_url",
			Message: "wiki urls are not supported by this tool yet",
		}
	default:
		return "", "", &toolError{
			Code:    "unsupported_document_url",
			Message: "url must point to a supported Feishu file or document",
		}
	}
}

func isSupportedFeishuURLHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch {
	case host == "feishu.cn", host == "feishu.net", host == "larksuite.com", host == "larksuite.com.cn", host == "larkoffice.com":
		return true
	case strings.HasSuffix(host, ".feishu.cn"),
		strings.HasSuffix(host, ".feishu.net"),
		strings.HasSuffix(host, ".larksuite.com"),
		strings.HasSuffix(host, ".larksuite.com.cn"),
		strings.HasSuffix(host, ".larkoffice.com"):
		return true
	default:
		return false
	}
}

func normalizedURLPathSegments(rawPath string) []string {
	cleaned := strings.TrimSpace(path.Clean(rawPath))
	if cleaned == "" || cleaned == "." || cleaned == "/" {
		return nil
	}
	parts := strings.Split(strings.Trim(cleaned, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, strings.ToLower(item))
	}
	if len(out) >= 2 {
		out[len(out)-1] = strings.TrimSpace(path.Base(strings.Trim(cleaned, "/")))
	}
	return out
}
