package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleFileChangeProgressStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Status = normalizeExecCommandProgressStatus(event.Status, false)
	if !upsertFileChangeProgressEntries(progress, event, false) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleFileChangeProgressCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Status = normalizeExecCommandProgressStatus(event.Status, true)
	if !upsertFileChangeProgressEntries(progress, event, true) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func cloneExecCommandProgressFileChange(record *state.ExecCommandProgressFileChangeRecord) *control.ExecCommandProgressFileChange {
	return execprogress.CloneFileChange(record)
}

func upsertFileChangeProgressEntries(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) bool {
	return execprogress.UpsertFileChangeProgressEntries(progress, event, final)
}
