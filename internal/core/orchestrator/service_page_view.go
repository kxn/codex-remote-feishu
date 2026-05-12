package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	commandLauncherPhaseHome            = "home"
	commandLauncherPhaseGroup           = "group"
	commandLauncherPhaseConfig          = "config"
	commandLauncherPhaseBusinessHandoff = "business_handoff"
	commandLauncherPhaseTerminal        = "terminal"
)

func (s *Service) pageEvent(surface *state.SurfaceConsoleRecord, view control.FeishuPageView) eventcontract.Event {
	return surfaceEventFromPayload(
		surface,
		eventcontract.PagePayload{
			View:    view,
			Context: s.buildFeishuPageContextFromView(surface, view),
		},
		eventcontract.EventMeta{
			InlineReplaceMode: eventcontract.InlineReplaceCurrentCard,
		},
	)
}

func (s *Service) menuPageEvent(surface *state.SurfaceConsoleRecord, raw, sourceMessageID string) eventcontract.Event {
	groupID := parseCommandMenuView(raw)
	if commandID, ok := control.ResolveFeishuCommandMenuGroupRootCommandID(s.buildCatalogContext(surface), groupID); ok {
		switch commandID {
		case control.FeishuCommandWorkspace:
			return s.workspacePageEvent(surface, commandID, true, sourceMessageID)
		}
	}
	view := s.buildCommandMenuView(surface, raw)
	page := control.NormalizeFeishuPageView(s.commandPageFromView(surface, view))
	phase := commandLauncherPhaseHome
	if strings.TrimSpace(groupID) != "" {
		phase = commandLauncherPhaseGroup
	}
	if flow := s.ensureCommandLauncherFlow(surface, groupID, phase); flow != nil {
		if messageID := strings.TrimSpace(sourceMessageID); messageID != "" {
			flow.MessageID = messageID
		}
		page.TrackingKey = strings.TrimSpace(flow.FlowID)
	}
	return s.pageEvent(surface, page)
}

func (s *Service) configPageEventFromCatalogView(surface *state.SurfaceConsoleRecord, view control.FeishuCatalogView) eventcontract.Event {
	page := control.NormalizeFeishuPageView(s.commandPageFromView(surface, view))
	flow := s.activeCommandLauncherFlow(surface)
	if flow != nil && flow.Role == frontstageFlowRoleLauncher {
		s.markCommandLauncherConfigPhase(surface)
		page.RelatedButtons = []control.CommandCatalogButton{{
			Label:       "返回菜单",
			Kind:        control.CommandCatalogButtonAction,
			CommandText: s.commandLauncherBackCommand(surface),
		}}
		page.TrackingKey = strings.TrimSpace(flow.FlowID)
	} else {
		page.RelatedButtons = nil
	}
	return s.pageEvent(surface, page)
}

func (s *Service) helpTerminalPageEvent(surface *state.SurfaceConsoleRecord) eventcontract.Event {
	view := s.buildCommandHelpView(surface)
	page := control.NormalizeFeishuPageView(s.commandPageFromView(surface, view))
	page.Sealed = true
	page.Interactive = false
	page.RelatedButtons = nil
	if flow := s.markCommandLauncherTerminal(surface); flow != nil {
		page.TrackingKey = strings.TrimSpace(flow.FlowID)
	}
	return s.pageEvent(surface, control.NormalizeFeishuPageView(page))
}

func (s *Service) activeCommandLauncherFlow(surface *state.SurfaceConsoleRecord) *activeOwnerCardFlowRecord {
	flow := s.activeOwnerCardFlow(surface)
	if flow == nil || flow.Kind != ownerCardFlowKindCommandMenu {
		return nil
	}
	return flow
}

func (s *Service) ensureCommandLauncherFlow(surface *state.SurfaceConsoleRecord, currentNode, phase string) *activeOwnerCardFlowRecord {
	if surface == nil {
		return nil
	}
	now := s.now()
	node := strings.TrimSpace(currentNode)
	flow := s.activeCommandLauncherFlow(surface)
	recreate := flow == nil || flow.Role != frontstageFlowRoleLauncher
	if flow != nil && !flow.ExpiresAt.IsZero() && !flow.ExpiresAt.After(now) {
		recreate = true
	}
	if recreate {
		flow = newOwnerCardFlowRecord(
			ownerCardFlowKindCommandMenu,
			s.pickers.nextLauncherFlowToken(),
			firstNonEmpty(surface.ActorUserID),
			now,
			defaultTargetPickerTTL,
			ownerCardFlowPhaseEditing,
		)
		flow.Role = frontstageFlowRoleLauncher
		s.setActiveOwnerCardFlow(surface, flow)
	} else {
		bumpOwnerCardFlowRevision(flow)
	}
	flow.Role = frontstageFlowRoleLauncher
	flow.Phase = ownerCardFlowPhaseEditing
	flow.CreatedAt = now
	flow.ExpiresAt = now.Add(defaultTargetPickerTTL)
	if strings.TrimSpace(flow.OriginMenuNode) == "" {
		flow.OriginMenuNode = node
	}
	flow.CurrentMenuNode = node
	flow.BackTarget = ""
	flow.LauncherPhase = strings.TrimSpace(phase)
	if flow.LauncherPhase == "" {
		flow.LauncherPhase = commandLauncherPhaseHome
	}
	return flow
}

func (s *Service) commandLauncherBackCommand(surface *state.SurfaceConsoleRecord) string {
	flow := s.activeCommandLauncherFlow(surface)
	if flow == nil {
		return ""
	}
	target := strings.TrimSpace(flow.BackTarget)
	if target == "" {
		return "/menu"
	}
	return "/menu " + target
}

func (s *Service) markCommandLauncherConfigPhase(surface *state.SurfaceConsoleRecord) {
	flow := s.activeCommandLauncherFlow(surface)
	if flow == nil || flow.Role != frontstageFlowRoleLauncher {
		return
	}
	flow.CreatedAt = s.now()
	flow.ExpiresAt = flow.CreatedAt.Add(defaultTargetPickerTTL)
	flow.LauncherPhase = commandLauncherPhaseConfig
	flow.BackTarget = strings.TrimSpace(flow.CurrentMenuNode)
	flow.Phase = ownerCardFlowPhaseEditing
	bumpOwnerCardFlowRevision(flow)
}

func (s *Service) markCommandLauncherTerminal(surface *state.SurfaceConsoleRecord) *activeOwnerCardFlowRecord {
	flow := s.activeCommandLauncherFlow(surface)
	if flow == nil {
		return nil
	}
	flow.Role = frontstageFlowRoleOwner
	flow.Phase = ownerCardFlowPhaseCompleted
	flow.LauncherPhase = commandLauncherPhaseTerminal
	flow.BackTarget = ""
	flow.CreatedAt = s.now()
	flow.ExpiresAt = flow.CreatedAt.Add(defaultTargetPickerTTL)
	bumpOwnerCardFlowRevision(flow)
	return flow
}

func (s *Service) markCommandLauncherEnteredBusiness(surface *state.SurfaceConsoleRecord, phase string) {
	flow := s.activeCommandLauncherFlow(surface)
	if flow == nil || flow.Role != frontstageFlowRoleLauncher {
		return
	}
	flow.Role = frontstageFlowRoleOwner
	flow.BackTarget = ""
	flow.Phase = ownerCardFlowPhaseRunning
	flow.LauncherPhase = strings.TrimSpace(phase)
	if flow.LauncherPhase == "" {
		flow.LauncherPhase = commandLauncherPhaseBusinessHandoff
	}
	flow.CreatedAt = s.now()
	flow.ExpiresAt = flow.CreatedAt.Add(defaultTargetPickerTTL)
	bumpOwnerCardFlowRevision(flow)
}

func (s *Service) applyCommandLauncherDisposition(surface *state.SurfaceConsoleRecord, action control.Action) {
	switch control.ResolveFeishuFrontstageActionContract(action).LauncherDisposition {
	case control.FeishuFrontstageLauncherEnterOwner:
		s.markCommandLauncherEnteredBusiness(surface, commandLauncherPhaseBusinessHandoff)
	case control.FeishuFrontstageLauncherKeep,
		control.FeishuFrontstageLauncherEnterTerminal:
		return
	default:
		return
	}
}

func (s *Service) activeCommandLauncherMessageID(surface *state.SurfaceConsoleRecord) string {
	flow := s.activeCommandLauncherFlow(surface)
	if flow == nil || flow.Role != frontstageFlowRoleLauncher {
		return ""
	}
	return strings.TrimSpace(flow.MessageID)
}
