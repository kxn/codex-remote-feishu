package feishu

import (
	"context"
	"net/http"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

type gatewayPreviewRuntime interface {
	previewpkg.FinalBlockPreviewService
	previewpkg.FinalBlockPreviewMaintenanceService
	previewpkg.WebPreviewConfigurable
	previewpkg.WebPreviewRouteService
}

type noopGatewayPreviewer struct{}

func (noopGatewayPreviewer) RewriteFinalBlock(_ context.Context, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	return previewpkg.FinalBlockPreviewResult{Block: req.Block}, nil
}

func (noopGatewayPreviewer) RunBackgroundMaintenance(context.Context) {}

func (noopGatewayPreviewer) SetWebPreviewPublisher(previewpkg.WebPreviewPublisher) {}

func (noopGatewayPreviewer) ServeWebPreview(http.ResponseWriter, *http.Request, string, string, bool) bool {
	return false
}

func (c *MultiGatewayController) RewriteFinalBlock(ctx context.Context, req previewpkg.FinalBlockPreviewRequest) (result previewpkg.FinalBlockPreviewResult, err error) {
	result = previewpkg.FinalBlockPreviewResult{Block: req.Block}
	resolution := c.resolveGatewayTarget(eventcontract.TargetRef{
		GatewayID:        req.GatewayID,
		SurfaceSessionID: req.SurfaceSessionID,
		SelectionPolicy:  eventcontract.GatewaySelectionAllowSurfaceDerived,
		FailurePolicy:    eventcontract.GatewayFailureNoop,
	}, gatewayTargetRequirePreviewer)
	if !resolution.ok() {
		return result, nil
	}
	req.GatewayID = resolution.GatewayID
	return resolution.Worker.previewer.RewriteFinalBlock(ctx, req)
}

func (c *MultiGatewayController) SetWebPreviewPublisher(publisher previewpkg.WebPreviewPublisher) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.webPreviewPublisher = publisher
	for _, worker := range c.workers {
		if worker == nil || worker.previewer == nil {
			continue
		}
		worker.previewer.SetWebPreviewPublisher(publisher)
	}
}

func (c *MultiGatewayController) ServeWebPreview(w http.ResponseWriter, r *http.Request, scopePublicID, previewID string, download bool) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, worker := range c.workers {
		if worker == nil || worker.previewer == nil {
			continue
		}
		if worker.previewer.ServeWebPreview(w, r, scopePublicID, previewID, download) {
			return true
		}
	}
	return false
}
