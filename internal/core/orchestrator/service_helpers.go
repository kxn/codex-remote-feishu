package orchestrator

import (
	"strings"

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

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
