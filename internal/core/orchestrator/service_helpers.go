package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func isHeadlessInstance(inst *state.InstanceRecord) bool {
	return state.IsManagedHeadlessInstance(inst)
}

func isVSCodeInstance(inst *state.InstanceRecord) bool {
	return inst != nil && state.IsVSCodeOrDefaultSource(inst.Source)
}

func headlessThreadWorkspaceMustMatch(inst *state.InstanceRecord) bool {
	return isHeadlessInstance(inst) && state.EffectiveInstanceBackend(inst) == agentproto.BackendClaude
}
