package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func workspacePageParentPayloadForCommand(commandID string) map[string]any {
	switch strings.TrimSpace(commandID) {
	case control.FeishuCommandWorkspace:
		return frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionWorkspaceRoot), "")
	case control.FeishuCommandWorkspaceNew:
		return frontstagecontract.ActionPayloadPageLocalAction(string(control.ActionWorkspaceNew), "")
	default:
		return nil
	}
}

func (s *Service) clearWorkspacePageRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	s.clearSurfaceWorkspacePage(surface)
}

func (s *Service) workspacePageParentPayload(surface *state.SurfaceConsoleRecord, sourceMessageID string) map[string]any {
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return nil
	}
	record := s.activeWorkspacePage(surface)
	if record == nil {
		return nil
	}
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
		s.clearWorkspacePageRuntime(surface)
		return nil
	}
	if strings.TrimSpace(record.MessageID) != sourceMessageID {
		return nil
	}
	return workspacePageParentPayloadForCommand(record.CommandID)
}

func (s *Service) workspacePageTriggeredFromMenu(surface *state.SurfaceConsoleRecord, sourceMessageID string) bool {
	if surface == nil {
		return false
	}
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID == "" {
		return false
	}
	record := s.activeWorkspacePage(surface)
	if record != nil {
		if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(s.now()) {
			s.clearWorkspacePageRuntime(surface)
			return false
		}
		if strings.TrimSpace(record.MessageID) == sourceMessageID {
			return record.FromMenu
		}
	}
	return s.activeCommandLauncherMessageID(surface) == sourceMessageID
}

func (s *Service) workspacePageEvent(surface *state.SurfaceConsoleRecord, commandID string, fromMenu bool, sourceMessageID string) eventcontract.Event {
	if surface != nil {
		s.clearThreadHistoryRuntime(surface)
		s.clearTargetPickerRuntime(surface)
		s.clearWorkspacePageRuntime(surface)
	}
	ownerUserID := ""
	if surface != nil {
		ownerUserID = firstNonEmpty(surface.ActorUserID)
	}
	flowID := s.pickers.nextLauncherFlowToken()
	flow := newOwnerCardFlowRecord(ownerCardFlowKindWorkspacePage, flowID, ownerUserID, s.now(), defaultTargetPickerTTL, ownerCardFlowPhaseEditing)
	sourceMessageID = strings.TrimSpace(sourceMessageID)
	if sourceMessageID != "" {
		flow.MessageID = sourceMessageID
	}
	page := &activeWorkspacePageRecord{
		FlowID:      flowID,
		CommandID:   strings.TrimSpace(commandID),
		OwnerUserID: ownerUserID,
		MessageID:   sourceMessageID,
		FromMenu:    fromMenu,
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
