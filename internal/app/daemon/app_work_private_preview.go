package daemon

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	previewpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/preview"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) previewWorkPrivateFinal(ctx context.Context, bot work.RoleBotBinding, req previewpkg.FinalBlockPreviewRequest) (previewpkg.FinalBlockPreviewResult, error) {
	fallback := previewpkg.FinalBlockPreviewResult{Block: req.Block}
	if a.teamRuntime == nil {
		return fallback, fmt.Errorf("work runtime unavailable")
	}
	directory := teamDirectory{app: a, store: a.teamRuntime.store}
	if _, err := directory.gateway(ctx, bot); err != nil {
		return fallback, err
	}
	controller, ok := a.finalBlockPreviewer.(*feishu.MultiGatewayController)
	if !ok {
		return fallback, fmt.Errorf("multi-application preview controller unavailable")
	}
	previewer, err := controller.ProjectPreviewer(bot.GatewayRef, bot.AppID)
	if err != nil {
		return fallback, err
	}
	req.PreviewGrantKey = a.previewGrantKey(req.GatewayID, req.SurfaceSessionID, req.Block)
	previewCtx, cancel := a.newTimeoutContext(ctx, a.finalPreviewTimeout)
	defer cancel()
	return previewer.RewriteFinalBlock(previewCtx, req)
}
