package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// FeishuDirectCommandCatalogFromView projects the UI-owned command view into the transition
// command catalog shape currently consumed by the Feishu renderer.
func FeishuDirectCommandCatalogFromView(view control.FeishuCommandView, ctx *control.FeishuUICommandContext) (control.FeishuDirectCommandCatalog, bool) {
	switch {
	case view.Menu != nil:
		return commandMenuCatalogFromView(*view.Menu, ctx), true
	case view.Config != nil:
		return commandConfigCatalogFromView(*view.Config), true
	default:
		return control.FeishuDirectCommandCatalog{}, false
	}
}

func commandMenuCatalogFromView(view control.FeishuCommandMenuView, ctx *control.FeishuUICommandContext) control.FeishuDirectCommandCatalog {
	stage := strings.TrimSpace(view.Stage)
	if stage == "" && ctx != nil {
		stage = strings.TrimSpace(ctx.MenuStage)
	}
	productMode := ""
	if ctx != nil {
		productMode = strings.TrimSpace(ctx.Surface.ProductMode)
	}
	groupID := strings.TrimSpace(view.GroupID)
	if groupID == "" {
		return control.BuildFeishuCommandMenuHomeCatalog()
	}
	return control.BuildFeishuCommandMenuGroupCatalog(groupID, productMode, stage)
}

func commandConfigCatalogFromView(view control.FeishuCommandConfigView) control.FeishuDirectCommandCatalog {
	return control.BuildFeishuCommandConfigCatalog(view)
}
