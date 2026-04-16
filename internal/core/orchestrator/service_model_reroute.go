package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (s *Service) applyTurnModelReroute(instanceID string, event agentproto.Event) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	reroute := agentproto.NormalizeTurnModelReroute(event.ModelReroute)
	if reroute == nil {
		reroute = agentproto.NormalizeTurnModelReroute(&agentproto.TurnModelReroute{
			ThreadID: event.ThreadID,
			TurnID:   event.TurnID,
		})
	}
	if reroute == nil {
		return nil
	}
	if reroute.ThreadID == "" {
		reroute.ThreadID = strings.TrimSpace(event.ThreadID)
	}
	if reroute.TurnID == "" {
		reroute.TurnID = strings.TrimSpace(event.TurnID)
	}
	if reroute.ThreadID == "" || reroute.TurnID == "" {
		return nil
	}
	thread := s.ensureThread(inst, reroute.ThreadID)
	thread.LastModelReroute = agentproto.CloneTurnModelReroute(reroute)
	if reroute.ToModel != "" {
		thread.ExplicitModel = reroute.ToModel
	}
	if binding := s.lookupRemoteTurn(instanceID, reroute.ThreadID, reroute.TurnID); binding != nil {
		binding.ModelReroute = agentproto.CloneTurnModelReroute(reroute)
	}
	return nil
}
