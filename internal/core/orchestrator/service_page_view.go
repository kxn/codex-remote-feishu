package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	menuFlowPhaseHome            = "home"
	menuFlowPhaseGroup           = "group"
	menuFlowPhaseConfig          = "config"
	menuFlowPhaseBusinessHandoff = "business_handoff"
	menuFlowPhaseTerminal        = "terminal"
)

func (s *Service) pageEvent(surface *state.SurfaceConsoleRecord, view control.FeishuPageView) control.UIEvent {
	return control.UIEvent{
		Kind:                     control.UIEventFeishuPageView,
		GatewayID:                surface.GatewayID,
		SurfaceSessionID:         surface.SurfaceSessionID,
		InlineReplaceCurrentCard: true,
		FeishuPageView:           &view,
		FeishuPageContext:        s.buildFeishuPageContextFromView(surface, view),
	}
}

func (s *Service) pageEventFromCatalogView(surface *state.SurfaceConsoleRecord, view control.FeishuCatalogView) control.UIEvent {
	page := s.commandPageFromView(surface, view)
	return s.pageEvent(surface, control.FeishuPageViewFromCommandPageView(page))
}

func (s *Service) menuPageEvent(surface *state.SurfaceConsoleRecord, raw string) control.UIEvent {
	groupID := parseCommandMenuView(raw)
	if groupID == control.FeishuCommandGroupSwitchTarget && s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
		return s.workspacePageEvent(surface, control.FeishuCommandWorkspace, true)
	}
	view := s.buildCommandMenuView(surface, raw)
	page := control.FeishuPageViewFromCommandPageView(s.commandPageFromView(surface, view))
	phase := menuFlowPhaseHome
	if strings.TrimSpace(groupID) != "" {
		phase = menuFlowPhaseGroup
	}
	s.ensureMenuFlow(surface, groupID, phase)
	return s.pageEvent(surface, page)
}

func (s *Service) configPageEventFromCatalogView(surface *state.SurfaceConsoleRecord, view control.FeishuCatalogView) control.UIEvent {
	page := control.FeishuPageViewFromCommandPageView(s.commandPageFromView(surface, view))
	if surface != nil && surface.MenuFlow != nil && !surface.MenuFlow.EnteredBusiness {
		s.markMenuFlowConfigPhase(surface)
		page.RelatedButtons = []control.CommandCatalogButton{{
			Label:       "返回菜单",
			Kind:        control.CommandCatalogButtonAction,
			CommandText: s.menuFlowBackCommand(surface),
		}}
	} else {
		page.RelatedButtons = nil
	}
	return s.pageEvent(surface, page)
}

func (s *Service) helpTerminalPageEvent(surface *state.SurfaceConsoleRecord) control.UIEvent {
	view := s.buildCommandHelpView(surface)
	page := control.FeishuPageViewFromCommandPageView(s.commandPageFromView(surface, view))
	page.Sealed = true
	page.Interactive = false
	page.RelatedButtons = nil
	s.markMenuFlowTerminal(surface)
	return s.pageEvent(surface, control.NormalizeFeishuPageView(page))
}

func (s *Service) ensureMenuFlow(surface *state.SurfaceConsoleRecord, currentNode, phase string) *state.MenuFlowRuntimeRecord {
	if surface == nil {
		return nil
	}
	node := strings.TrimSpace(currentNode)
	if surface.MenuFlow == nil || surface.MenuFlow.EnteredBusiness {
		flowID := s.pickers.nextMenuFlowToken()
		surface.MenuFlow = &state.MenuFlowRuntimeRecord{
			FlowID:          flowID,
			Revision:        1,
			OriginMenuNode:  node,
			CurrentMenuNode: node,
			BackTarget:      "",
			Phase:           strings.TrimSpace(phase),
		}
		if surface.MenuFlow.Phase == "" {
			surface.MenuFlow.Phase = menuFlowPhaseHome
		}
		return surface.MenuFlow
	}
	flow := surface.MenuFlow
	flow.Revision++
	if strings.TrimSpace(flow.OriginMenuNode) == "" {
		flow.OriginMenuNode = node
	}
	flow.CurrentMenuNode = node
	flow.BackTarget = ""
	flow.Phase = strings.TrimSpace(phase)
	if flow.Phase == "" {
		flow.Phase = menuFlowPhaseHome
	}
	return flow
}

func (s *Service) menuFlowBackCommand(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || surface.MenuFlow == nil {
		return ""
	}
	target := strings.TrimSpace(surface.MenuFlow.BackTarget)
	if target == "" {
		return "/menu"
	}
	return "/menu " + target
}

func (s *Service) markMenuFlowConfigPhase(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.MenuFlow == nil || surface.MenuFlow.EnteredBusiness {
		return
	}
	surface.MenuFlow.Revision++
	surface.MenuFlow.Phase = menuFlowPhaseConfig
	surface.MenuFlow.BackTarget = strings.TrimSpace(surface.MenuFlow.CurrentMenuNode)
}

func (s *Service) markMenuFlowTerminal(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.MenuFlow == nil {
		return
	}
	surface.MenuFlow.Revision++
	surface.MenuFlow.Phase = menuFlowPhaseTerminal
	surface.MenuFlow.BackTarget = ""
}

func (s *Service) markMenuFlowEnteredBusiness(surface *state.SurfaceConsoleRecord, phase string) {
	if surface == nil || surface.MenuFlow == nil {
		return
	}
	surface.MenuFlow.Revision++
	surface.MenuFlow.EnteredBusiness = true
	surface.MenuFlow.BackTarget = ""
	surface.MenuFlow.Phase = strings.TrimSpace(phase)
	if surface.MenuFlow.Phase == "" {
		surface.MenuFlow.Phase = menuFlowPhaseBusinessHandoff
	}
}

func shouldSealMenuFlowForAction(kind control.ActionKind) bool {
	switch kind {
	case control.ActionShowCommandMenu,
		control.ActionShowCommandHelp,
		control.ActionModeCommand,
		control.ActionAutoContinueCommand,
		control.ActionReasoningCommand,
		control.ActionAccessCommand,
		control.ActionPlanCommand,
		control.ActionModelCommand,
		control.ActionVerboseCommand,
		control.ActionStatus:
		return false
	default:
		return true
	}
}

func (s *Service) setMenuFlowMessageID(surface *state.SurfaceConsoleRecord, messageID string) {
	if surface == nil || surface.MenuFlow == nil {
		return
	}
	surface.MenuFlow.MessageID = strings.TrimSpace(messageID)
}

func (s *Service) RecordMenuFlowMessage(surfaceID, flowID, messageID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil || surface.MenuFlow == nil {
		return
	}
	if strings.TrimSpace(flowID) == "" || strings.TrimSpace(surface.MenuFlow.FlowID) != strings.TrimSpace(flowID) {
		return
	}
	s.setMenuFlowMessageID(surface, messageID)
}
