package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) adminPageTriggeredFromMenu(surface *state.SurfaceConsoleRecord, sourceMessageID string) bool {
	if surface == nil {
		return false
	}
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return false
	}
	return s.activeCommandLauncherMessageID(surface) == sourceMessageID
}

func (s *Service) adminRootPageEvent(surface *state.SurfaceConsoleRecord, fromMenu bool) eventcontract.Event {
	view := control.BuildFeishuAdminRootPageView(fromMenu)
	if flow := s.activeCommandLauncherFlow(surface); flow != nil && flow.Role == frontstageFlowRoleLauncher {
		view.TrackingKey = strings.TrimSpace(flow.FlowID)
	}
	return s.pageEvent(surface, view)
}
