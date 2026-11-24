package state

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestApplyInstanceHelloProfileUsesBackendIdentity(t *testing.T) {
	h := agentproto.InstanceHello{CodexProfileID: "cp_oauth", ClaudeProfileID: "claude-profile", ClaudeReasoningEffort: "high", OpenCodeProfileID: "open-profile", OpenCodeRuntimeAccessMode: "full_access"}
	inst := &InstanceRecord{}
	ApplyInstanceHelloProfile(inst, agentproto.BackendCodex, h)
	if inst.CodexProfileID != "cp_oauth" || inst.ClaudeProfileID != "" || inst.OpenCodeProfileID != "" {
		t.Fatalf("codex: %+v", inst)
	}
	ApplyInstanceHelloProfile(inst, agentproto.BackendClaude, h)
	if inst.CodexProfileID != "" || inst.ClaudeProfileID != NormalizeClaudeProfileID(h.ClaudeProfileID) || inst.ClaudeReasoningEffort != NormalizeReasoningEffort(h.ClaudeReasoningEffort) {
		t.Fatalf("claude: %+v", inst)
	}
	ApplyInstanceHelloProfile(inst, agentproto.BackendOpenCode, h)
	if inst.ClaudeProfileID != "" || inst.OpenCodeProfileID != NormalizeOpenCodeProfileID(h.OpenCodeProfileID) || inst.OpenCodeRuntimeAccessMode != NormalizeOpenCodeRuntimeAccessMode(h.OpenCodeRuntimeAccessMode) {
		t.Fatalf("opencode: %+v", inst)
	}
}
