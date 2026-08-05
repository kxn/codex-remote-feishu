package preview

import (
	"context"
	"strings"
	"time"
)

const (
	DefaultRootFolderName = defaultPreviewRootFolderName
	FileType              = previewFileType
	FolderType            = previewFolderType
	PermissionFullAccess  = previewPermissionFullAccess
)

const (
	previewDriveSummaryTimeout           = 20 * time.Second
	previewDriveCleanupTimeout           = 45 * time.Second
	previewDriveBackgroundCleanupTimeout = 45 * time.Second
)

type DriveAPI = previewDriveAPI
type RemoteNode = previewRemoteNode
type Principal = previewPrincipal

func normalizeGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func newFeishuTimeoutContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = parent
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}
