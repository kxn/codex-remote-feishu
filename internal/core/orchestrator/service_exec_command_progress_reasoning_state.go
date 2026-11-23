package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func surfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.SurfaceReasoningProgressRecord {
	if surface == nil || surface.ActiveReasoning == nil {
		return nil
	}
	record := surface.ActiveReasoning
	if record.InstanceID != strings.TrimSpace(instanceID) || record.ThreadID != strings.TrimSpace(threadID) || record.TurnID != strings.TrimSpace(turnID) {
		return nil
	}
	return record
}

func ensureSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.SurfaceReasoningProgressRecord {
	if surface == nil {
		return nil
	}
	if record := surfaceReasoningProgress(surface, instanceID, threadID, turnID); record != nil {
		return record
	}
	surface.ActiveReasoning = &state.SurfaceReasoningProgressRecord{
		InstanceID: strings.TrimSpace(instanceID),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	}
	return surface.ActiveReasoning
}

func clearSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) {
	if surface == nil {
		return
	}
	if surfaceReasoningProgress(surface, instanceID, threadID, turnID) != nil {
		surface.ActiveReasoning = nil
	}
}

func (s *Service) upsertSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string, event agentproto.Event, backend agentproto.Backend, now time.Time) bool {
	record := ensureSurfaceReasoningProgress(surface, instanceID, threadID, turnID)
	if record == nil {
		return false
	}
	return execprogress.UpdateReasoningCarrier(&record.Reasoning, event, backend, now)
}
