package orchestrator

import "strings"

func (s *Service) pendingTurnTextValue(instanceID, threadID, turnID string) string {
	item := s.pendingTurnTextItem(instanceID, threadID, turnID)
	if item == nil {
		return ""
	}
	return strings.TrimSpace(item.Text)
}

func (s *Service) pendingTurnTextItem(instanceID, threadID, turnID string) *completedTextItem {
	return s.progress.pendingTurnTextItem(instanceID, threadID, turnID)
}

func (s *Service) pendingPlanProposalItem(instanceID, threadID, turnID string) *completedTextItem {
	return s.progress.pendingPlanProposalItem(instanceID, threadID, turnID)
}

func (s *Service) clearPendingProgressTextForTurn(instanceID, threadID, turnID string) {
	s.progress.clearPendingTextForTurn(instanceID, threadID, turnID)
}

func (s *Service) clearPendingProgressTextForInstance(instanceID string) {
	s.progress.clearPendingTextForInstance(instanceID)
}
