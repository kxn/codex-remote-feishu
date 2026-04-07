package feishu

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

type GatewayController interface {
	Gateway
	MarkdownPreviewService
	UpsertApp(context.Context, GatewayAppConfig) error
	RemoveApp(context.Context, string) error
	Verify(context.Context, GatewayAppConfig) (VerifyResult, error)
	Status() []GatewayStatus
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
	Client() *lark.Client
	SetStateHook(func(GatewayState, error))
}

type gatewayWorker struct {
	config     GatewayAppConfig
	status     GatewayStatus
	runtime    gatewayRuntime
	previewer  MarkdownPreviewService
	cancel     context.CancelFunc
	generation uint64
}

type MultiGatewayController struct {
	mu            sync.RWMutex
	workers       map[string]*gatewayWorker
	started       bool
	startCtx      context.Context
	actionHandler ActionHandler

	newGateway   func(GatewayAppConfig) gatewayRuntime
	newPreviewer func(gatewayRuntime, GatewayAppConfig) MarkdownPreviewService
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
	controller.newPreviewer = func(runtime gatewayRuntime, cfg GatewayAppConfig) MarkdownPreviewService {
		if runtime == nil || runtime.Client() == nil || strings.TrimSpace(cfg.PreviewStatePath) == "" {
			return nil
		}
		return NewDriveMarkdownPreviewer(
			NewLarkDrivePreviewAPI(runtime.Client()),
			MarkdownPreviewConfig{
				StatePath:      cfg.PreviewStatePath,
				RootFolderName: cfg.PreviewRootFolderName,
				GatewayID:      cfg.GatewayID,
			},
		)
	}
	return controller
}

func (c *MultiGatewayController) Start(ctx context.Context, handler ActionHandler) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = true
	c.startCtx = ctx
	c.actionHandler = handler
	workerIDs := c.workerIDsLocked()
	c.mu.Unlock()

	for _, gatewayID := range workerIDs {
		c.mu.Lock()
		_ = c.ensureWorkerRunningLocked(gatewayID)
		c.mu.Unlock()
	}

	<-ctx.Done()
	c.stopAllWorkers()
	c.mu.Lock()
	c.started = false
	c.startCtx = nil
	c.actionHandler = nil
	c.mu.Unlock()
	return nil
}

func (c *MultiGatewayController) Apply(ctx context.Context, operations []Operation) error {
	grouped := map[string][]Operation{}

	c.mu.RLock()
	workerCount := len(c.workers)
	soleGatewayID := ""
	if workerCount == 1 {
		for gatewayID := range c.workers {
			soleGatewayID = gatewayID
		}
	}
	c.mu.RUnlock()

	for _, operation := range operations {
		gatewayID := strings.TrimSpace(operation.GatewayID)
		if gatewayID == "" {
			if workerCount != 1 {
				return fmt.Errorf("gateway apply failed: missing gateway id")
			}
			gatewayID = soleGatewayID
		}
		grouped[gatewayID] = append(grouped[gatewayID], operation)
	}

	for gatewayID, group := range grouped {
		c.mu.RLock()
		worker := c.workers[gatewayID]
		c.mu.RUnlock()
		if worker == nil || worker.runtime == nil {
			return fmt.Errorf("gateway apply failed: gateway %s is not running", gatewayID)
		}
		if err := worker.runtime.Apply(ctx, group); err != nil {
			c.updateWorkerError(gatewayID, err)
			return err
		}
	}
	return nil
}

func (c *MultiGatewayController) RewriteFinalBlock(ctx context.Context, req MarkdownPreviewRequest) (renderedBlock render.Block, err error) {
	renderedBlock = req.Block
	gatewayID := normalizeGatewayID(firstNonEmpty(req.GatewayID, gatewayIDFromSurface(req.SurfaceSessionID)))
	c.mu.RLock()
	worker := c.workers[gatewayID]
	c.mu.RUnlock()
	if worker == nil || worker.previewer == nil {
		return renderedBlock, nil
	}
	return worker.previewer.RewriteFinalBlock(ctx, req)
}

func (c *MultiGatewayController) UpsertApp(ctx context.Context, cfg GatewayAppConfig) error {
	cfg = normalizeGatewayAppConfig(cfg)

	c.mu.Lock()
	worker := c.workers[cfg.GatewayID]
	if worker == nil {
		worker = &gatewayWorker{}
		c.workers[cfg.GatewayID] = worker
	}
	c.stopWorkerLocked(worker)
	worker.config = cfg
	worker.status = GatewayStatus{
		GatewayID:      cfg.GatewayID,
		Name:           cfg.Name,
		State:          GatewayStateStopped,
		Disabled:       !cfg.Enabled,
		LastVerifiedAt: worker.status.LastVerifiedAt,
	}
	if !cfg.Enabled {
		worker.status.State = GatewayStateDisabled
		c.mu.Unlock()
		return nil
	}
	if !workerHasCredentials(cfg) {
		worker.status.State = GatewayStateAuthFailed
		worker.status.LastError = "missing app credentials"
		c.mu.Unlock()
		return nil
	}
	if c.started && c.startCtx != nil {
		err := c.ensureWorkerRunningLocked(cfg.GatewayID)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
	_ = ctx
	return nil
}

func (c *MultiGatewayController) RemoveApp(_ context.Context, gatewayID string) error {
	gatewayID = normalizeGatewayID(gatewayID)
	c.mu.Lock()
	defer c.mu.Unlock()
	worker := c.workers[gatewayID]
	if worker == nil {
		return nil
	}
	c.stopWorkerLocked(worker)
	delete(c.workers, gatewayID)
	return nil
}

func (c *MultiGatewayController) Verify(ctx context.Context, cfg GatewayAppConfig) (VerifyResult, error) {
	cfg = normalizeGatewayAppConfig(cfg)
	result, err := VerifyGatewayConnection(ctx, LiveGatewayConfig{
		GatewayID:      cfg.GatewayID,
		AppID:          cfg.AppID,
		AppSecret:      cfg.AppSecret,
		Domain:         cfg.Domain,
		TempDir:        cfg.ImageTempDir,
		UseSystemProxy: cfg.UseSystemProxy,
	})
	if err == nil {
		c.mu.Lock()
		if worker := c.workers[cfg.GatewayID]; worker != nil {
			worker.status.LastVerifiedAt = time.Now().UTC()
			worker.status.LastError = ""
		}
		c.mu.Unlock()
	}
	return result, err
}

func (c *MultiGatewayController) Status() []GatewayStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	values := make([]GatewayStatus, 0, len(c.workers))
	for _, worker := range c.workers {
		if worker == nil {
			continue
		}
		values = append(values, worker.status)
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].GatewayID < values[j].GatewayID
	})
	return values
}

func (c *MultiGatewayController) ensureWorkerRunningLocked(gatewayID string) error {
	worker := c.workers[gatewayID]
	if worker == nil || !worker.config.Enabled || !workerHasCredentials(worker.config) {
		return nil
	}
	worker.generation++
	generation := worker.generation
	runtime := c.newGateway(worker.config)
	if runtime == nil {
		return fmt.Errorf("gateway %s runtime factory returned nil", gatewayID)
	}
	runtime.SetStateHook(func(state GatewayState, err error) {
		c.applyStateHook(gatewayID, generation, state, err)
	})
	worker.runtime = runtime
	worker.previewer = c.newPreviewer(runtime, worker.config)
	worker.status.Disabled = false
	worker.status.State = GatewayStateConnecting
	worker.status.LastError = ""

	workerCtx, cancel := context.WithCancel(c.startCtx)
	worker.cancel = cancel
	handler := c.actionHandler
	go func(runtime gatewayRuntime, ctx context.Context) {
		err := runtime.Start(ctx, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.applyStateHook(gatewayID, generation, GatewayStateDegraded, err)
			return
		}
		c.applyStateHook(gatewayID, generation, GatewayStateStopped, nil)
	}(runtime, workerCtx)
	return nil
}

func (c *MultiGatewayController) applyStateHook(gatewayID string, generation uint64, state GatewayState, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	worker := c.workers[gatewayID]
	if worker == nil || worker.generation != generation {
		return
	}
	worker.status.State = state
	worker.status.Disabled = state == GatewayStateDisabled
	if state == GatewayStateConnected {
		worker.status.LastConnectedAt = time.Now().UTC()
		worker.status.LastError = ""
		return
	}
	if err != nil {
		worker.status.LastError = err.Error()
	}
}

func (c *MultiGatewayController) updateWorkerError(gatewayID string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	worker := c.workers[gatewayID]
	if worker == nil {
		return
	}
	worker.status.LastError = err.Error()
	if worker.status.State == GatewayStateConnected {
		worker.status.State = GatewayStateDegraded
	}
}

func (c *MultiGatewayController) stopAllWorkers() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, worker := range c.workers {
		c.stopWorkerLocked(worker)
	}
}

func (c *MultiGatewayController) stopWorkerLocked(worker *gatewayWorker) {
	if worker == nil {
		return
	}
	if worker.cancel != nil {
		worker.cancel()
		worker.cancel = nil
	}
	worker.runtime = nil
	worker.previewer = nil
	if worker.config.Enabled {
		worker.status.State = GatewayStateStopped
	}
}

func (c *MultiGatewayController) workerIDsLocked() []string {
	ids := make([]string, 0, len(c.workers))
	for gatewayID := range c.workers {
		ids = append(ids, gatewayID)
	}
	sort.Strings(ids)
	return ids
}

func normalizeGatewayAppConfig(cfg GatewayAppConfig) GatewayAppConfig {
	cfg.GatewayID = normalizeGatewayID(cfg.GatewayID)
	if strings.TrimSpace(cfg.PreviewRootFolderName) == "" {
		cfg.PreviewRootFolderName = defaultPreviewRootFolderName
	}
	if strings.TrimSpace(cfg.PreviewStatePath) == "" {
		cfg.PreviewStatePath = filepath.Join(".", "feishu-preview-"+cfg.GatewayID+".json")
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
