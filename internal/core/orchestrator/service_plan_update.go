package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func turnPlanSnapshotKey(surfaceID, instanceID, threadID, turnID string) string {
	return strings.Join([]string{surfaceID, instanceID, threadID, turnID}, "::")
}

func deleteMatchingTurnPlanSnapshots(records map[string]*turnPlanSnapshotRecord, instanceID, threadID, turnID string) {
	for key, record := range records {
		if record == nil {
			continue
		}
		if record.InstanceID != instanceID {
			continue
		}
		if threadID != "" && record.ThreadID != threadID {
			continue
		}
		if turnID != "" && record.TurnID != turnID {
			continue
		}
		delete(records, key)
	}
}

func equalTurnPlanSnapshot(left, right *agentproto.TurnPlanSnapshot) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	if left.Explanation != right.Explanation {
		return false
	}
	if len(left.Steps) != len(right.Steps) {
		return false
	}
	for index := range left.Steps {
		if left.Steps[index] != right.Steps[index] {
			return false
		}
	}
	return true
}

func planUpdateFromSnapshot(threadID, turnID string, snapshot *agentproto.TurnPlanSnapshot) *control.PlanUpdate {
	if snapshot == nil {
		return nil
	}
	update := &control.PlanUpdate{
		ThreadID:    threadID,
		TurnID:      turnID,
		Explanation: snapshot.Explanation,
	}
	if len(snapshot.Steps) > 0 {
		update.Steps = make([]control.PlanUpdateStep, 0, len(snapshot.Steps))
		for _, step := range snapshot.Steps {
			update.Steps = append(update.Steps, control.PlanUpdateStep{
				Step:   step.Step,
				Status: step.Status,
			})
		}
	}
	return update
}

func (s *Service) applyTurnPlanUpdate(instanceID string, event agentproto.Event) []control.UIEvent {
	if event.PlanSnapshot == nil || strings.TrimSpace(event.ThreadID) == "" || strings.TrimSpace(event.TurnID) == "" {
		return nil
	}
	surface := s.surfaceForInitiator(instanceID, event)
	if surface == nil {
		return nil
	}
	key := turnPlanSnapshotKey(surface.SurfaceSessionID, instanceID, event.ThreadID, event.TurnID)
	if existing := s.turnPlanSnapshots[key]; existing != nil && equalTurnPlanSnapshot(existing.Snapshot, event.PlanSnapshot) {
		return nil
	}
	s.turnPlanSnapshots[key] = &turnPlanSnapshotRecord{
		SurfaceSessionID: surface.SurfaceSessionID,
		InstanceID:       instanceID,
		ThreadID:         event.ThreadID,
		TurnID:           event.TurnID,
		Snapshot:         agentproto.CloneTurnPlanSnapshot(event.PlanSnapshot),
	}
	update := planUpdateFromSnapshot(event.ThreadID, event.TurnID, event.PlanSnapshot)
	if update == nil {
		return nil
	}
	sourceMessageID := ""
	if binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID); binding != nil {
		sourceMessageID = binding.ReplyToMessageID
	}
	return []control.UIEvent{{
		Kind:             control.UIEventPlanUpdated,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  sourceMessageID,
		PlanUpdate:       update,
	}}
}
