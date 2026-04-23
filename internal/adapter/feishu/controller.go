package feishu

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

type GatewayAdminController interface {
	UpsertApp(context.Context, GatewayAppConfig) error
	RemoveApp(context.Context, string) error
	Verify(context.Context, GatewayAppConfig) (VerifyResult, error)
	Status() []GatewayStatus
}

type GatewayController interface {
	Gateway
	GatewayAdminController
}

type PermissionBlockController interface {
	ClearGrantedPermissionBlocks(gatewayID string, scopes []AppScopeStatus)
}

type GatewayAppConfig struct {
	GatewayID             string
	Name                  string
	AppID                 string
	AppSecret             string
	Domain                string
	Enabled               bool
	UseSystemProxy        bool
	ImageTempDir          string
	PreviewStatePath      string
	PreviewCacheDir       string
	PreviewRootFolderName string
}

type GatewayStatus struct {
	GatewayID       string       `json:"gatewayId"`
	Name            string       `json:"name,omitempty"`
	State           GatewayState `json:"state"`
	Disabled        bool         `json:"disabled"`
	LastError       string       `json:"lastError,omitempty"`
	LastConnectedAt time.Time    `json:"lastConnectedAt,omitempty"`
	LastVerifiedAt  time.Time    `json:"lastVerifiedAt,omitempty"`
}

type gatewayRuntime interface {
	Gateway
	IMFileSender
	IMImageSender
	Client() *lark.Client
	SetStateHook(func(GatewayState, error))
}

type gatewayWorker struct {
	config     GatewayAppConfig
	status     GatewayStatus
	runtime    gatewayRuntime
	previewer  gatewayPreviewRuntime
	cancel     context.CancelFunc
	generation uint64
}

type MultiGatewayController struct {
	mu                  sync.RWMutex
	workers             map[string]*gatewayWorker
	started             bool
	startCtx            context.Context
	actionHandler       ActionHandler
	webPreviewPublisher previewpkg.WebPreviewPublisher

	newGateway   func(GatewayAppConfig) gatewayRuntime
	newPreviewer func(gatewayRuntime, GatewayAppConfig) gatewayPreviewRuntime
}

func NewMultiGatewayController() *MultiGatewayController {
	controller := &MultiGatewayController{
		workers: map[string]*gatewayWorker{},
	}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		return NewLiveGateway(LiveGatewayConfig{
			GatewayID:      cfg.GatewayID,
			AppID:          cfg.AppID,
			AppSecret:      cfg.AppSecret,
			Domain:         cfg.Domain,
			TempDir:        cfg.ImageTempDir,
			UseSystemProxy: cfg.UseSystemProxy,
		})
	}
	controller.newPreviewer = func(runtime gatewayRuntime, cfg GatewayAppConfig) gatewayPreviewRuntime {
		if strings.TrimSpace(cfg.PreviewCacheDir) == "" {
			return noopGatewayPreviewer{}
		}
		var api previewpkg.DriveAPI
		if runtime != nil && runtime.Client() != nil {
			api = NewLarkDrivePreviewAPI(cfg.GatewayID, runtime.Client())
		}
		return previewpkg.NewDriveMarkdownPreviewer(
			api,
			previewpkg.MarkdownPreviewConfig{
				StatePath: cfg.PreviewStatePath,
				CacheDir:  cfg.PreviewCacheDir,
				GatewayID: cfg.GatewayID,
			},
		)
	}
	return controller
}

func normalizeGatewayAppConfig(cfg GatewayAppConfig) GatewayAppConfig {
	cfg.GatewayID = normalizeGatewayID(cfg.GatewayID)
	if strings.TrimSpace(cfg.PreviewRootFolderName) == "" {
		cfg.PreviewRootFolderName = previewpkg.DefaultRootFolderName
	}
	if strings.TrimSpace(cfg.PreviewStatePath) == "" {
		cfg.PreviewStatePath = filepath.Join(".", "feishu-preview-"+cfg.GatewayID+".json")
	}
	if strings.TrimSpace(cfg.PreviewCacheDir) == "" {
		cfg.PreviewCacheDir = filepath.Join(".", "preview-cache", cfg.GatewayID)
	}
	return cfg
}

func workerHasCredentials(cfg GatewayAppConfig) bool {
	return strings.TrimSpace(cfg.AppID) != "" && strings.TrimSpace(cfg.AppSecret) != ""
}

func gatewayIDFromSurface(surfaceID string) string {
	ref, ok := ParseSurfaceRef(surfaceID)
	if !ok {
		return ""
	}
	return ref.GatewayID
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
