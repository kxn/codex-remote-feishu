package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handleAssistantMessageProgressStart(_ string, _ agentproto.Event) []eventcontract.Event {
	return nil
}

func (s *Service) handleReasoningSummaryProgressDelta(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	if !s.upsertSurfaceReasoningProgress(surface, instanceID, event.ThreadID, event.TurnID, event, s.surfaceBackend(surface), s.now()) {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress != nil {
		syncExecCommandProgressReasoning(progress, surfaceReasoningProgress(surface, instanceID, event.ThreadID, event.TurnID))
	}
	if !surfaceShowsVisibleReasoning(surface.Verbosity) {
		return nil
	}
	if progress == nil {
		progress = s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
		if progress == nil {
			return nil
		}
		syncExecCommandProgressReasoning(progress, surfaceReasoningProgress(surface, instanceID, event.ThreadID, event.TurnID))
	}
	progress.ItemID = strings.TrimSpace(event.ItemID)
	if !s.execCommandProgressReasoningFlushDue(progress, s.now()) || !execCommandProgressReasoningCanEmit(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) flushExecCommandProgressReasoning(instanceID, threadID, turnID string) []eventcontract.Event {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil || !surfaceShowsVisibleReasoning(surface.Verbosity) {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, threadID, turnID)
	if progress == nil || !execCommandProgressReasoningDirty(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, threadID, turnID, false)
}

func (s *Service) tickExecCommandProgressReasoning(surface *state.SurfaceConsoleRecord, now time.Time) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if !surfaceShowsVisibleReasoning(surface.Verbosity) {
		return nil
	}
	progress := surface.ActiveExecProgress
	if progress == nil || !execCommandProgressReasoningDirty(progress) || !s.execCommandProgressReasoningFlushDue(progress, now) {
		return nil
	}
	if !execCommandProgressReasoningCanEmit(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, progress.ThreadID, progress.TurnID, false)
}

func execCommandProgressReasoningDirty(progress *state.ExecCommandProgressRecord) bool {
	if progress == nil || progress.Reasoning == nil || progress.Reasoning.LastUpdatedAt.IsZero() {
		return false
	}
	return progress.LastEmittedAt.IsZero() || progress.Reasoning.Revision > progress.Reasoning.LastEmittedRevision
}

func (s *Service) execCommandProgressReasoningFlushDue(progress *state.ExecCommandProgressRecord, now time.Time) bool {
	if progress == nil || !execCommandProgressReasoningDirty(progress) {
		return false
	}
	if progress.LastEmittedAt.IsZero() {
		return true
	}
	return !now.Before(progress.LastEmittedAt.Add(execCommandProgressReasoningFlushInterval))
}

func execCommandProgressReasoningCanEmit(progress *state.ExecCommandProgressRecord) bool {
	if progress == nil {
		return false
	}
	return progress.LastEmittedAt.IsZero() || activeExecCommandProgressSegmentMessageID(progress) != ""
}

func (s *Service) handleReasoningSummaryProgressCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	defer clearSurfaceReasoningProgress(surface, instanceID, event.ThreadID, event.TurnID)
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	syncExecCommandProgressReasoning(progress, surfaceReasoningProgress(surface, instanceID, event.ThreadID, event.TurnID))
	if !finalizeExecCommandProgressReasoning(progress, execprogress.NormalizeStatus(event.Status, true)) && !surfaceShowsVisibleReasoning(surface.Verbosity) {
		return nil
	}
	if !surfaceShowsVisibleReasoning(surface.Verbosity) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}
