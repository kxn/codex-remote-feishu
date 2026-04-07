package feishu

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

const (
	defaultPreviewRootFolderName = "Codex Remote Previews"
	defaultPreviewMaxFileBytes   = 20 * 1024 * 1024
	defaultPreviewLazyCleanupAge = 24 * time.Hour
	defaultPreviewLazyCleanupGap = 5 * time.Hour
	previewFileType              = "file"
	previewFolderType            = "folder"
	previewPermissionView        = "view"
	previewManagedFilePrefix     = "__crp__"
	previewRootMarkerPrefix      = "__codex_remote_gateway__"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)
var markdownLineSuffixPattern = regexp.MustCompile(`^(.*\.md)(:\d+(?::\d+)?)$`)

type MarkdownPreviewService interface {
	RewriteFinalBlock(context.Context, MarkdownPreviewRequest) (render.Block, error)
}

type PreviewDriveAdminService interface {
	Summary() (PreviewDriveSummary, error)
	CleanupBefore(context.Context, time.Time) (PreviewDriveCleanupResult, error)
	Reconcile(context.Context) (PreviewDriveReconcileResult, error)
}

type MarkdownPreviewRequest struct {
	GatewayID        string
	SurfaceSessionID string
	ChatID           string
	ActorUserID      string
	WorkspaceRoot    string
	ThreadCWD        string
	Block            render.Block
}

type MarkdownPreviewConfig struct {
	StatePath      string
	RootFolderName string
	GatewayID      string
	ProcessCWD     string
	MaxFileBytes   int64
}

type DriveMarkdownPreviewer struct {
	api    previewDriveAPI
	config MarkdownPreviewConfig

	mu     sync.Mutex
	loaded bool
	state  *previewState
	nowFn  func() time.Time
}

type previewDriveAPI interface {
	CreateFolder(context.Context, string, string) (previewRemoteNode, error)
	UploadFile(context.Context, string, string, []byte) (string, error)
	QueryMetaURL(context.Context, string, string) (string, error)
	GrantPermission(context.Context, string, string, previewPrincipal) error
	DeleteFile(context.Context, string, string) error
	ListFiles(context.Context, string) ([]previewRemoteNode, error)
	ListPermissionMembers(context.Context, string, string) (map[string]bool, error)
}

type previewRemoteNode struct {
	Token        string
	URL          string
	Type         string
	Name         string
	CreatedTime  time.Time
	ModifiedTime time.Time
}

type previewPrincipal struct {
	Key        string
	MemberType string
	MemberID   string
	Type       string
}

type previewState struct {
	Root          *previewFolderRecord           `json:"root,omitempty"`
	Scopes        map[string]*previewScopeRecord `json:"scopes,omitempty"`
	Files         map[string]*previewFileRecord  `json:"files,omitempty"`
	LastCleanupAt time.Time                      `json:"lastCleanupAt,omitempty"`
}

type previewScopeRecord struct {
	Folder     *previewFolderRecord `json:"folder,omitempty"`
	LastUsedAt time.Time            `json:"lastUsedAt,omitempty"`
}

type previewFolderRecord struct {
	Token            string          `json:"token,omitempty"`
	URL              string          `json:"url,omitempty"`
	Shared           map[string]bool `json:"shared,omitempty"`
	MarkerReady      bool            `json:"markerReady,omitempty"`
	LastReconciledAt time.Time       `json:"lastReconciledAt,omitempty"`
}

type previewFileRecord struct {
	Path       string          `json:"path,omitempty"`
	SHA256     string          `json:"sha256,omitempty"`
	Token      string          `json:"token,omitempty"`
	URL        string          `json:"url,omitempty"`
	Shared     map[string]bool `json:"shared,omitempty"`
	ScopeKey   string          `json:"scopeKey,omitempty"`
	SizeBytes  int64           `json:"sizeBytes,omitempty"`
	CreatedAt  time.Time       `json:"createdAt,omitempty"`
	LastUsedAt time.Time       `json:"lastUsedAt,omitempty"`
}

type PreviewDriveSummary struct {
	StatePath            string     `json:"statePath,omitempty"`
	RootToken            string     `json:"rootToken,omitempty"`
	RootURL              string     `json:"rootURL,omitempty"`
	FileCount            int        `json:"fileCount"`
	ScopeCount           int        `json:"scopeCount"`
	EstimatedBytes       int64      `json:"estimatedBytes"`
	UnknownSizeFileCount int        `json:"unknownSizeFileCount"`
	OldestLastUsedAt     *time.Time `json:"oldestLastUsedAt,omitempty"`
	NewestLastUsedAt     *time.Time `json:"newestLastUsedAt,omitempty"`
}

type PreviewDriveCleanupResult struct {
	DeletedFileCount            int                 `json:"deletedFileCount"`
	DeletedEstimatedBytes       int64               `json:"deletedEstimatedBytes"`
	SkippedUnknownLastUsedCount int                 `json:"skippedUnknownLastUsedCount"`
	Summary                     PreviewDriveSummary `json:"summary"`
}

type PreviewDriveReconcileResult struct {
	Summary                 PreviewDriveSummary `json:"summary"`
	RootMissing             bool                `json:"rootMissing"`
	RemoteMissingScopeCount int                 `json:"remoteMissingScopeCount"`
	RemoteMissingFileCount  int                 `json:"remoteMissingFileCount"`
	LocalOnlyScopeCount     int                 `json:"localOnlyScopeCount"`
	LocalOnlyFileCount      int                 `json:"localOnlyFileCount"`
	PermissionDriftCount    int                 `json:"permissionDriftCount"`
}

type driveAPIError struct {
	Code int
	Msg  string
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

func NewDriveMarkdownPreviewer(api previewDriveAPI, cfg MarkdownPreviewConfig) *DriveMarkdownPreviewer {
	if cfg.RootFolderName == "" {
		cfg.RootFolderName = defaultPreviewRootFolderName
	}
	cfg.GatewayID = normalizeGatewayID(cfg.GatewayID)
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultPreviewMaxFileBytes
	}
	if cfg.ProcessCWD == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.ProcessCWD = cwd
		}
	}
	return &DriveMarkdownPreviewer{
		api:    api,
		config: cfg,
		nowFn:  time.Now,
	}
}
