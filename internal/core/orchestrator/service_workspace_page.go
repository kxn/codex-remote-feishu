package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func workspacePageCommandText(commandID string) string {
	switch strings.TrimSpace(commandID) {
	case control.FeishuCommandWorkspace:
		return "/workspace"
	case control.FeishuCommandWorkspaceNew:
		return "/workspace new"
	default:
		return ""
	}
}

func (s *Service) clearWorkspacePageRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	s.clearSurfaceWorkspacePage(surface)
}

func (s *Service) workspacePageParentCommand(surface *state.SurfaceConsoleRecord, sourceMessageID string) string {
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return ""
	}
	record := s.activeWorkspacePage(surface)
	if record == nil {
		return ""
	}
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearWorkspacePageRuntime(surface)
		return ""
	}
	if strings.TrimSpace(record.MessageID) != sourceMessageID {
		return ""
	}
	return workspacePageCommandText(record.CommandID)
}

func (s *Service) workspacePageTriggeredFromMenu(surface *state.SurfaceConsoleRecord, sourceMessageID string) bool {
	if surface == nil || surface.MenuFlow == nil || surface.MenuFlow.EnteredBusiness {
		return false
	}
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return false
	}
	return strings.TrimSpace(surface.MenuFlow.MessageID) == sourceMessageID
}

func (s *Service) workspacePageEvent(surface *state.SurfaceConsoleRecord, commandID string, fromMenu bool) control.UIEvent {
	if surface != nil {
		s.clearThreadHistoryRuntime(surface)
		s.clearTargetPickerRuntime(surface)
		s.clearWorkspacePageRuntime(surface)
	}
	if fromMenu {
		s.ensureMenuFlow(surface, control.FeishuCommandGroupSwitchTarget, menuFlowPhaseGroup)
	}
	ownerUserID := ""
	if surface != nil {
		ownerUserID = firstNonEmpty(surface.ActorUserID)
	}
	flowID := s.pickers.nextMenuFlowToken()
	flow := newOwnerCardFlowRecord(ownerCardFlowKindWorkspacePage, flowID, ownerUserID, s.now(), defaultTargetPickerTTL, ownerCardFlowPhaseEditing)
	page := &activeWorkspacePageRecord{
		FlowID:      flowID,
		CommandID:   strings.TrimSpace(commandID),
		OwnerUserID: ownerUserID,
		CreatedAt:   s.now(),
		ExpiresAt:   s.now().Add(defaultTargetPickerTTL),
	}
	s.setActiveOwnerCardFlow(surface, flow)
	s.setActiveWorkspacePage(surface, page)

	var view control.FeishuPageView
	switch strings.TrimSpace(commandID) {
	case control.FeishuCommandWorkspaceNew:
		view = control.BuildFeishuWorkspaceNewPageView(fromMenu)
	default:
		view = control.BuildFeishuWorkspaceRootPageView(fromMenu)
	}
	view.TrackingKey = flowID
	return s.pageEvent(surface, view)
}
