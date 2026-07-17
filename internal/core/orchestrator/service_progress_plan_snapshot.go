package orchestrator

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func (s *Service) upsertTurnPlanSnapshot(surfaceID, instanceID, threadID, turnID string, snapshot *agentproto.TurnPlanSnapshot) bool {
	return s.progress.upsertTurnPlanSnapshot(&turnPlanSnapshotRecord{
		SurfaceSessionID: surfaceID,
		InstanceID:       instanceID,
		ThreadID:         threadID,
		TurnID:           turnID,
		Snapshot:         agentproto.CloneTurnPlanSnapshot(snapshot),
	})
}

func (s *Service) clearTurnPlanSnapshots(instanceID, threadID, turnID string) {
	s.progress.clearTurnPlanSnapshots(instanceID, threadID, turnID)
}
