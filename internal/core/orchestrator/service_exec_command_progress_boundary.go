package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) insertExecCommandProgressBoundary(instanceID, threadID, turnID string, events []eventcontract.Event) []eventcontract.Event {
	if len(events) == 0 || strings.TrimSpace(instanceID) == "" || strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return events
	}
	boundaryIndex := -1
	for i, event := range events {
		if !execCommandProgressBoundaryEvent(event) {
			continue
		}
		boundaryIndex = i
		break
	}
	if boundaryIndex < 0 {
		return events
	}
	flush := s.flushAndSealExecCommandProgressForTurn(instanceID, threadID, turnID)
	if len(flush) == 0 {
		return events
	}
	out := make([]eventcontract.Event, 0, len(events)+len(flush))
	out = append(out, events[:boundaryIndex]...)
	out = append(out, flush...)
	out = append(out, events[boundaryIndex:]...)
	return out
}

func execCommandProgressBoundaryEvent(event eventcontract.Event) bool {
	event = event.Normalized()
	if event.Command != nil || event.DaemonCommand != nil || event.InlineReplaceCurrentCard {
		return false
	}
	switch event.CanonicalKind() {
	case eventcontract.KindRequest,
		eventcontract.KindPlanUpdate,
		eventcontract.KindNotice,
		eventcontract.KindTimelineText,
		eventcontract.KindImageOutput:
		return true
	default:
		return false
	}
}
