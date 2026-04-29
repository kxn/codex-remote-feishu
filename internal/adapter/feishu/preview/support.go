package preview

import (
	"context"
	"strings"
	"time"
)

const (
	PlatformFeishu = "feishu"
	ScopeKindUser  = "user"
	ScopeKindChat  = "chat"
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

type SurfaceRef struct {
	Platform  string
	GatewayID string
	ScopeKind string
	ScopeID   string
}

func ParseSurfaceRef(surfaceID string) (SurfaceRef, bool) {
	parts := strings.Split(strings.TrimSpace(surfaceID), ":")
	if len(parts) != 4 {
		return SurfaceRef{}, false
	}
	if parts[0] != PlatformFeishu {
		return SurfaceRef{}, false
	}
	ref := SurfaceRef{
		Platform:  parts[0],
		GatewayID: normalizeGatewayID(parts[1]),
		ScopeKind: strings.TrimSpace(parts[2]),
		ScopeID:   strings.TrimSpace(parts[3]),
	}
	if !ref.valid() {
		return SurfaceRef{}, false
	}
	return ref, true
}

func (r SurfaceRef) SurfaceID() string {
	if !r.valid() {
		return ""
	}
	return strings.Join([]string{
		PlatformFeishu,
		normalizeGatewayID(r.GatewayID),
		r.ScopeKind,
		r.ScopeID,
	}, ":")
}

func (r SurfaceRef) valid() bool {
	if strings.TrimSpace(r.Platform) != PlatformFeishu {
		return false
	}
	if strings.TrimSpace(r.GatewayID) == "" {
		return false
	}
	if strings.TrimSpace(r.ScopeID) == "" {
		return false
	}
	switch strings.TrimSpace(r.ScopeKind) {
	case ScopeKindUser, ScopeKindChat:
		return true
	default:
		return false
	}
}

func normalizeGatewayID(gatewayID string) string {
	return strings.TrimSpace(gatewayID)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
