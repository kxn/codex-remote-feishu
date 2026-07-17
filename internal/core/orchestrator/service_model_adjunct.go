package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) applyTurnModelVerification(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	verification := agentproto.NormalizeTurnModelVerification(event.ModelVerification)
	if verification == nil {
		verification = agentproto.NormalizeTurnModelVerification(&agentproto.TurnModelVerification{
			ThreadID: event.ThreadID,
			TurnID:   event.TurnID,
		})
	}
	if verification == nil {
		return nil
	}
	if verification.ThreadID == "" {
		verification.ThreadID = strings.TrimSpace(event.ThreadID)
	}
	if verification.TurnID == "" {
		verification.TurnID = strings.TrimSpace(event.TurnID)
	}
	if verification.ThreadID == "" || verification.TurnID == "" {
		return nil
	}
	thread := s.ensureThread(inst, verification.ThreadID)
	thread.LastModelVerification = agentproto.CloneTurnModelVerification(verification)
	return nil
}

func (s *Service) applyTurnModelSafetyBuffering(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	buffering := agentproto.NormalizeTurnModelSafetyBuffering(event.ModelSafetyBuffering)
	if buffering == nil {
		buffering = agentproto.NormalizeTurnModelSafetyBuffering(&agentproto.TurnModelSafetyBuffering{
			ThreadID: event.ThreadID,
			TurnID:   event.TurnID,
		})
	}
	if buffering == nil {
		return nil
	}
	if buffering.ThreadID == "" {
		buffering.ThreadID = strings.TrimSpace(event.ThreadID)
	}
	if buffering.TurnID == "" {
		buffering.TurnID = strings.TrimSpace(event.TurnID)
	}
	if buffering.ThreadID == "" || buffering.TurnID == "" {
		return nil
	}
	thread := s.ensureThread(inst, buffering.ThreadID)
	thread.LastModelSafetyBuffering = agentproto.CloneTurnModelSafetyBuffering(buffering)
	return nil
}
