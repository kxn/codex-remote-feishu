package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateThreadCommandsRequireSessionScope(t *testing.T) {
	for _, kind := range []agentproto.CommandKind{agentproto.CommandThreadCompactStart, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalSet, agentproto.CommandThreadGoalClear, agentproto.CommandThreadRead, agentproto.CommandThreadHistoryRead} {
		if !privateEffectTargetsSession(kind) {
			t.Errorf("%s bypasses session scope", kind)
		}
	}
}
