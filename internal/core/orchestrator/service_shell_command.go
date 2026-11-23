package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func queuedItemShellPayload(item *state.QueueItemRecord) (string, bool, error) {
	if item == nil {
		return "", false, nil
	}
	return agentproto.QueuedInputShellPayload(item.Inputs)
}
