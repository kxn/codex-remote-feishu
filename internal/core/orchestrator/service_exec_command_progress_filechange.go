package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
)

func (s *Service) handleFileChangeProgressStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	if !execprogress.UpsertFileChangeProgressEntries(progress, event, false) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleFileChangeProgressCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	if !execprogress.UpsertFileChangeProgressEntries(progress, event, true) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}
