package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) applyCapabilityStateUpdate(instanceID string, event agentproto.Event) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	update := agentproto.NormalizeCapabilityStateUpdate(event.CapabilityState)
	if update == nil {
		return nil
	}
	if update.ThreadID == "" {
		update.ThreadID = strings.TrimSpace(event.ThreadID)
	}
	inst.LastCapabilityState = agentproto.CloneCapabilityStateUpdate(update)
	if update.ThreadID != "" {
		thread := s.ensureThread(inst, update.ThreadID)
		thread.LastCapabilityState = agentproto.CloneCapabilityStateUpdate(update)
	}
	return s.projectCapabilityStateUpdate(instanceID, *update)
}
