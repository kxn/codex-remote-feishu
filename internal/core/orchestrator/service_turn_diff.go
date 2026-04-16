package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (s *Service) recordTurnDiffSnapshot(instanceID string, event agentproto.Event) {
	if s == nil {
		return
	}
	threadID := strings.TrimSpace(event.ThreadID)
	turnID := strings.TrimSpace(event.TurnID)
	if threadID == "" || turnID == "" {
		return
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	diff := event.TurnDiff
	if strings.TrimSpace(diff) == "" {
		delete(s.turnDiffSnapshots, key)
		return
	}
	s.turnDiffSnapshots[key] = &control.TurnDiffSnapshot{
		ThreadID: threadID,
		TurnID:   turnID,
		Diff:     diff,
	}
}

func (s *Service) takeTurnDiffSnapshot(instanceID, threadID, turnID string) *control.TurnDiffSnapshot {
	if s == nil {
		return nil
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	snapshot := s.turnDiffSnapshots[key]
	delete(s.turnDiffSnapshots, key)
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}
