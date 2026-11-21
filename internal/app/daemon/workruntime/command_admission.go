package workruntime

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

func safeReadOrStopCommand(kind agentproto.CommandKind) bool {
	switch kind {
	case agentproto.CommandRuntimeInspect, agentproto.CommandRuntimeMCPRead, agentproto.CommandModelList, agentproto.CommandThreadsRefresh, agentproto.CommandThreadRead, agentproto.CommandThreadHistoryRead, agentproto.CommandThreadGoalGet, agentproto.CommandThreadGoalClear, agentproto.CommandProcessExit, agentproto.CommandTurnInterrupt:
		return true
	}
	return false
}
