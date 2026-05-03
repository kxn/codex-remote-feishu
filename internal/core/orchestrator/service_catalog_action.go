package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) resolveCatalogActionFromSurfaceContext(surface *state.SurfaceConsoleRecord, action control.Action) control.Action {
	if strings.TrimSpace(action.CatalogFamilyID) != "" && strings.TrimSpace(action.CatalogVariantID) != "" && action.CatalogBackend != "" {
		return action
	}
	resolved, ok := control.ResolveFeishuActionCatalog(s.buildCatalogContext(surface), action)
	if !ok {
		return action
	}
	return resolved.Action
}
