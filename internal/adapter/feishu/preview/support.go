package preview

import (
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
