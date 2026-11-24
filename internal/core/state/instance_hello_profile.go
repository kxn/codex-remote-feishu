package state

import "github.com/kxn/codex-remote-feishu/internal/core/agentproto"

// ApplyInstanceHelloProfile projects the backend-specific identity reported by
// an authenticated runtime hello into the shared instance model.
func ApplyInstanceHelloProfile(inst *InstanceRecord, backend agentproto.Backend, hello agentproto.InstanceHello) {
	inst.CodexProfileID = ""
	inst.ClaudeProfileID, inst.ClaudeReasoningEffort = "", ""
	inst.OpenCodeProfileID, inst.OpenCodeRuntimeAccessMode = "", ""
	switch backend {
	case agentproto.BackendCodex:
		inst.CodexProfileID = NormalizeCodexProfileID(hello.CodexProfileID)
	case agentproto.BackendClaude:
		inst.ClaudeProfileID = NormalizeClaudeProfileID(hello.ClaudeProfileID)
		inst.ClaudeReasoningEffort = NormalizeReasoningEffort(hello.ClaudeReasoningEffort)
	case agentproto.BackendOpenCode:
		inst.OpenCodeProfileID = NormalizeOpenCodeProfileID(hello.OpenCodeProfileID)
		inst.OpenCodeRuntimeAccessMode = NormalizeOpenCodeRuntimeAccessMode(hello.OpenCodeRuntimeAccessMode)
	default:
		inst.OpenCodeAdmissionRef = nil
	}
	if backend != agentproto.BackendOpenCode {
		inst.OpenCodeAdmissionRef = nil
	}
}
