package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressTransientAnimationInterval = 1500 * time.Millisecond

func (s *Service) handleAssistantMessageProgressStart(instanceID string, event agentproto.Event) []eventcontract.Event {
	return s.clearExecCommandProgressReasoning(instanceID, event.ThreadID, event.TurnID)
}

func (s *Service) handleReasoningSummaryProgressDelta(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || state.NormalizeSurfaceVerbosity(surface.Verbosity) != state.SurfaceVerbosityVerbose {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	if !execprogress.UpsertReasoning(progress, event, s.now()) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) clearExecCommandProgressReasoning(instanceID, threadID, turnID string) []eventcontract.Event {
	surface := s.turnSurface(instanceID, threadID, turnID)
	progress := activeExecCommandProgress(surface, instanceID, threadID, turnID)
	if !execprogress.ClearReasoningRecord(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, threadID, turnID, false)
}
