package feishu

import (
	"context"
	"fmt"

	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
)

// ProjectGateway selects a configured application explicitly. The returned
// adapter shares its credential broker; callers never supply tokens or choose
// an implicit "current bot". Lifecycle replacement can invalidate transport but
// cannot silently substitute a different application identity.
func (c *MultiGatewayController) ProjectGateway(gatewayID, appID string) (*LiveGateway, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	w := c.workers[gatewayID]
	if w == nil || !w.config.Enabled || w.config.AppID != appID {
		return nil, fmt.Errorf("configured application is unavailable or has changed")
	}
	gateway, ok := w.runtime.(*LiveGateway)
	if !ok || gateway == nil {
		return nil, fmt.Errorf("application has no live platform adapter")
	}
	return gateway, nil
}

// PrivatePublicationGateway returns the exact adapter for a frozen private
// publication. It preserves the selected credentials across worker replacement.
func (c *MultiGatewayController) PrivatePublicationGateway(gatewayID, appID string) (Gateway, error) {
	gateway, err := c.ProjectGateway(gatewayID, appID)
	if err != nil {
		return nil, err
	}
	return privatePublicationGateway{LiveGateway: gateway, controller: c, gatewayID: gatewayID}, nil
}

type privatePublicationGateway struct {
	*LiveGateway
	controller *MultiGatewayController
	gatewayID  string
}

func (g privatePublicationGateway) Apply(ctx context.Context, operations []Operation) error {
	err := g.LiveGateway.Apply(ctx, operations)
	if err != nil {
		g.controller.updateWorkerRuntimeError(g.gatewayID, g.LiveGateway, err)
	}
	return err
}

// ProjectPreviewer freezes the selected application's preview implementation.
func (c *MultiGatewayController) ProjectPreviewer(gatewayID, appID string) (previewpkg.FinalBlockPreviewService, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	w := c.workers[gatewayID]
	if w == nil || !w.config.Enabled || w.config.AppID != appID || w.previewer == nil {
		return nil, fmt.Errorf("configured preview application is unavailable or has changed")
	}
	return w.previewer, nil
}
