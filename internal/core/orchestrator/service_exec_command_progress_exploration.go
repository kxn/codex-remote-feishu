package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execProgressExplorationBlockID = execprogress.ExplorationBlockID

type execProgressExplorationAction = execprogress.ExplorationAction

func execCommandProgressBlocks(progress *state.ExecCommandProgressRecord) []control.ExecCommandProgressBlock {
	return execprogress.Blocks(progress)
}

func upsertExplorationProgressForCommandExecution(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) (bool, bool) {
	return execprogress.UpsertExplorationProgressForCommandExecution(progress, event, final)
}

func upsertExplorationProgressForDynamicTool(progress *state.ExecCommandProgressRecord, event agentproto.Event, final bool) (bool, bool) {
	return execprogress.UpsertExplorationProgressForDynamicTool(progress, event, final)
}

func parseCommandExecutionExplorationAction(command string) (execProgressExplorationAction, bool) {
	return execprogress.ParseCommandExecutionExplorationAction(command)
}
