package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func isHeadlessInstance(inst *state.InstanceRecord) bool {
	return inst != nil && strings.EqualFold(strings.TrimSpace(inst.Source), "headless") && inst.Managed
}

func isVSCodeInstance(inst *state.InstanceRecord) bool {
	if inst == nil {
		return false
	}
	return strings.EqualFold(xutil.FirstNonEmpty(inst.Source, "vscode"), "vscode")
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
