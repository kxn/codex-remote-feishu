package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func turnPlanSnapshotKey(surfaceID, instanceID, threadID, turnID string) string {
	return strings.Join([]string{surfaceID, instanceID, threadID, turnID}, "::")
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
		ThreadID:              threadID,
		TurnID:                turnID,
		TemporarySessionLabel: "",
		Explanation:           snapshot.Explanation,
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

func (s *Service) applyTurnPlanUpdate(instanceID string, event agentproto.Event) []eventcontract.Event {
	if event.PlanSnapshot == nil || strings.TrimSpace(event.ThreadID) == "" || strings.TrimSpace(event.TurnID) == "" {
		return nil
	}
	surface := s.surfaceForInitiator(instanceID, event)
	if surface == nil {
		return nil
	}
	if !s.upsertTurnPlanSnapshot(surface.SurfaceSessionID, instanceID, event.ThreadID, event.TurnID, event.PlanSnapshot) {
		return nil
	}
	update := planUpdateFromSnapshot(event.ThreadID, event.TurnID, event.PlanSnapshot)
	if update == nil {
		return nil
	}
	update.TemporarySessionLabel = s.temporarySessionLabel(surface, instanceID, event.ThreadID, event.TurnID)
	sourceMessageID, _ := s.replyAnchorForTurn(instanceID, event.ThreadID, event.TurnID)
	outbound := eventcontract.Event{
		Kind:             eventcontract.KindPlanUpdate,
		GatewayID:        surface.GatewayID,
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  sourceMessageID,
		PlanUpdate:       update,
	}
	if strings.TrimSpace(sourceMessageID) != "" {
		outbound.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return []eventcontract.Event{outbound}
}
