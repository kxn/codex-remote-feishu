package feishu

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
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
	PrimaryGatewayForChat func(chatID string) string
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
	IMVideoSender
	DriveFileCommentReader
	Client() *lark.Client
	SetStateHook(func(GatewayState, error))
}

type gatewayWorker struct {
	config     GatewayAppConfig
	status     GatewayStatus
	runtime    gatewayRuntime
	previewer  gatewayPreviewRuntime
	cancel     context.CancelFunc
	actionGate *gatewayActionGate
	generation uint64
}

type MultiGatewayController struct {
	lifecycleMu         sync.Mutex
	mu                  sync.RWMutex
	workers             map[string]*gatewayWorker
	started             bool
	startCtx            context.Context
	actionHandler       ActionHandler
	webPreviewPublisher previewpkg.WebPreviewPublisher

	newGateway   func(GatewayAppConfig) gatewayRuntime
	newPreviewer func(gatewayRuntime, GatewayAppConfig) gatewayPreviewRuntime
}

type gatewayActionGate struct {
	mu     sync.Mutex
	cond   *sync.Cond
	closed bool
	active int
}

func newGatewayActionGate() *gatewayActionGate {
	gate := &gatewayActionGate{}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (g *gatewayActionGate) handler(next ActionHandler) ActionHandler {
	if g == nil || next == nil {
		return next
	}
	return func(ctx context.Context, action control.Action) *ActionResult {
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			return nil
		}
		g.active++
		g.mu.Unlock()

		defer func() {
			g.mu.Lock()
			g.active--
			if g.active == 0 {
				g.cond.Broadcast()
			}
			g.mu.Unlock()
		}()
		return next(ctx, action)
	}
}

func (g *gatewayActionGate) close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func (g *gatewayActionGate) wait() {
	if g == nil {
		return
	}
	g.mu.Lock()
	for g.active > 0 {
		g.cond.Wait()
	}
	g.mu.Unlock()
}

func NewMultiGatewayController() *MultiGatewayController {
	controller := &MultiGatewayController{
		workers: map[string]*gatewayWorker{},
	}
	controller.newGateway = func(cfg GatewayAppConfig) gatewayRuntime {
		return NewLiveGateway(LiveGatewayConfig{
			GatewayID:             cfg.GatewayID,
			AppID:                 cfg.AppID,
			AppSecret:             cfg.AppSecret,
			Domain:                cfg.Domain,
			TempDir:               cfg.ImageTempDir,
			UseSystemProxy:        cfg.UseSystemProxy,
			PrimaryGatewayForChat: cfg.PrimaryGatewayForChat,
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
	ref, ok := feishuidentity.ParseSurfaceRef(surfaceID)
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
