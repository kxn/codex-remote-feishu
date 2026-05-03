package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) resolveCatalogActionFromSurfaceContext(surface *state.SurfaceConsoleRecord, action control.Action) control.Action {
	if !control.ActionRoutesThroughFeishuCommandCatalog(action) {
		return action
	}
	if control.HasStrictFeishuCommandCatalogProvenance(action) {
		return action
	}
	if action.IsCardAction() {
		return action
	}
	resolved, ok := control.ResolveFeishuActionCatalog(s.buildCatalogContext(surface), action)
	if !ok {
		return action
	}
	return resolved.Action
}
