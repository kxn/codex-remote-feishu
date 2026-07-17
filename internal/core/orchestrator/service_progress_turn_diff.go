package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (r *serviceProgressRuntime) recordTurnDiffSnapshot(instanceID string, event agentproto.Event) {
	if r == nil {
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
		delete(r.turnDiffSnapshots, key)
		return
	}
	r.turnDiffSnapshots[key] = &control.TurnDiffSnapshot{
		ThreadID: threadID,
		TurnID:   turnID,
		Diff:     diff,
	}
}

func (r *serviceProgressRuntime) takeTurnDiffSnapshot(instanceID, threadID, turnID string) *control.TurnDiffSnapshot {
	if r == nil {
		return nil
	}
	key := turnRenderKey(instanceID, threadID, turnID)
	snapshot := r.turnDiffSnapshots[key]
	delete(r.turnDiffSnapshots, key)
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}
