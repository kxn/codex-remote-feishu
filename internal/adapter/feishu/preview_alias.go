package feishu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

type FinalBlockPreviewService = previewpkg.FinalBlockPreviewService
type FinalBlockPreviewMaintenanceService = previewpkg.FinalBlockPreviewMaintenanceService
type FinalBlockPreviewRequest = previewpkg.FinalBlockPreviewRequest
type FinalBlockPreviewResult = previewpkg.FinalBlockPreviewResult
type PreviewLocation = previewpkg.PreviewLocation
type PreviewReference = previewpkg.PreviewReference
type PreviewPlan = previewpkg.PreviewPlan
type PreparedPreviewArtifact = previewpkg.PreparedPreviewArtifact
type PreviewDeliveryKind = previewpkg.PreviewDeliveryKind
type PreviewDeliveryPlan = previewpkg.PreviewDeliveryPlan
type PreviewPublishMode = previewpkg.PreviewPublishMode
type PreviewPublishResult = previewpkg.PreviewPublishResult
type PreviewPublishRequest = previewpkg.PreviewPublishRequest
type FinalBlockPreviewHandler = previewpkg.FinalBlockPreviewHandler
type FinalBlockPreviewPublisher = previewpkg.FinalBlockPreviewPublisher
type MarkdownPreviewService = previewpkg.MarkdownPreviewService
type MarkdownPreviewRequest = previewpkg.MarkdownPreviewRequest
type WebPreviewGrantRequest = previewpkg.WebPreviewGrantRequest
type WebPreviewPublisher = previewpkg.WebPreviewPublisher
type WebPreviewConfigurable = previewpkg.WebPreviewConfigurable
type WebPreviewRouteService = previewpkg.WebPreviewRouteService
type PreviewDriveAdminService = previewpkg.PreviewDriveAdminService
type MarkdownPreviewConfig = previewpkg.MarkdownPreviewConfig
type DriveMarkdownPreviewer = previewpkg.DriveMarkdownPreviewer
type PreviewDriveSummary = previewpkg.PreviewDriveSummary
type PreviewDriveCleanupResult = previewpkg.PreviewDriveCleanupResult

type previewDriveAPI = previewpkg.DriveAPI
type previewRemoteNode = previewpkg.RemoteNode
type previewPrincipal = previewpkg.Principal

const (
	PreviewDeliveryDriveFileLink = previewpkg.PreviewDeliveryDriveFileLink
	PreviewDeliveryWebFileLink   = previewpkg.PreviewDeliveryWebFileLink
	PreviewPublishModeInlineLink = previewpkg.PreviewPublishModeInlineLink
	defaultPreviewRootFolderName = previewpkg.DefaultRootFolderName
	previewFileType              = previewpkg.FileType
	previewFolderType            = previewpkg.FolderType
	previewPermissionView        = previewpkg.PermissionView
)

func NewDriveMarkdownPreviewer(api previewDriveAPI, cfg MarkdownPreviewConfig) *DriveMarkdownPreviewer {
	return previewpkg.NewDriveMarkdownPreviewer(api, cfg)
}

type driveAPIError struct {
	API       string
	Code      int
	Msg       string
	RequestID string
	LogID     string
}

func (e *driveAPIError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Msg) == "" {
		return fmt.Sprintf("feishu drive api error %d", e.Code)
	}
	return fmt.Sprintf("feishu drive api error %d: %s", e.Code, strings.TrimSpace(e.Msg))
}

func isPreviewDriveAccessDeniedError(err error) bool {
	var apiErr *driveAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case 99991672:
		return true
	default:
		return false
	}
}

func parsePreviewRemoteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case value > 1_000_000_000_000:
			return time.UnixMilli(value).UTC()
		case value > 0:
			return time.Unix(value, 0).UTC()
		default:
			return time.Time{}
		}
	}
	if value, err := time.Parse(time.RFC3339, raw); err == nil {
		return value.UTC()
	}
	return time.Time{}
}
