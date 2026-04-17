package feishu

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultPreviewRootFolderName        = "Codex Remote Previews"
	defaultPreviewMaxFileBytes          = 20 * 1024 * 1024
	defaultPreviewBackgroundCleanupAge  = 24 * time.Hour
	defaultPreviewBackgroundCleanupTick = 1 * time.Hour
	previewFileType                     = "file"
	previewFolderType                   = "folder"
	previewPermissionView               = "view"
)

var previewColonLocationSuffixPattern = regexp.MustCompile(`^(.*?)(:\d+(?::\d+)?)$`)

type PreviewDriveAdminService interface {
	Summary(context.Context) (PreviewDriveSummary, error)
	CleanupBefore(context.Context, time.Time) (PreviewDriveCleanupResult, error)
}

type MarkdownPreviewConfig struct {
	StatePath               string
	CacheDir                string
	GatewayID               string
	ProcessCWD              string
	MaxFileBytes            int64
	BackgroundCleanupEvery  time.Duration
	BackgroundCleanupMaxAge time.Duration
}

type DriveMarkdownPreviewer struct {
	api    previewDriveAPI
	config MarkdownPreviewConfig

	handlers      []FinalBlockPreviewHandler
	publishers    []FinalBlockPreviewPublisher
	webPublisher  WebPreviewPublisher
	stateMu       sync.Mutex
	webPreviewMu  sync.Mutex
	maintenanceMu sync.Mutex
	inflightMu    sync.Mutex
	inflightOps   map[string]*previewOpCall
	loaded        bool
	state         *previewState
	nowFn         func() time.Time
}

type previewOpCall struct {
	done  chan struct{}
	value any
	err   error
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
	Status               string     `json:"status,omitempty"`
	StatusMessage        string     `json:"statusMessage,omitempty"`
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

func NewDriveMarkdownPreviewer(api previewDriveAPI, cfg MarkdownPreviewConfig) *DriveMarkdownPreviewer {
	cfg.GatewayID = normalizeGatewayID(cfg.GatewayID)
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultPreviewMaxFileBytes
	}
	if cfg.BackgroundCleanupEvery <= 0 {
		cfg.BackgroundCleanupEvery = defaultPreviewBackgroundCleanupTick
	}
	if cfg.BackgroundCleanupMaxAge <= 0 {
		cfg.BackgroundCleanupMaxAge = defaultPreviewBackgroundCleanupAge
	}
	if cfg.ProcessCWD == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.ProcessCWD = cwd
		}
	}
	previewer := &DriveMarkdownPreviewer{
		api:    api,
		config: cfg,
		nowFn:  time.Now,
	}
	previewer.RegisterHandler(markdownFilePreviewHandler{previewer: previewer})
	previewer.RegisterPublisher(driveMarkdownLinkPublisher{previewer: previewer})
	previewer.RegisterPublisher(webPreviewLinkPublisher{previewer: previewer})
	return previewer
}

func (p *DriveMarkdownPreviewer) RegisterHandler(handler FinalBlockPreviewHandler) {
	if p == nil || handler == nil {
		return
	}
	p.handlers = append(p.handlers, handler)
}

func (p *DriveMarkdownPreviewer) RegisterPublisher(publisher FinalBlockPreviewPublisher) {
	if p == nil || publisher == nil {
		return
	}
	p.publishers = append(p.publishers, publisher)
}

func (p *DriveMarkdownPreviewer) SetWebPreviewPublisher(publisher WebPreviewPublisher) {
	if p == nil {
		return
	}
	p.webPublisher = publisher
}
