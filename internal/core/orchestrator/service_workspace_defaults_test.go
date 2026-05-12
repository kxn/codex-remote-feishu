package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestResolveWorkspaceDefaultsPartitionsByProviderAndProfile(t *testing.T) {
	now := time.Date(2026, 5, 3, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendCodex,
		CodexProviderID: state.DefaultCodexProviderID,
	})] = state.ModelConfigRecord{Model: "gpt-default"}
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "team-proxy",
	})] = state.ModelConfigRecord{Model: "gpt-team"}
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
	})] = state.ModelConfigRecord{ReasoningEffort: "low"}
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	})] = state.ModelConfigRecord{ReasoningEffort: "max"}

	inst := &state.InstanceRecord{
		InstanceID:    "inst",
		WorkspaceRoot: workspaceKey,
		WorkspaceKey:  workspaceKey,
		Backend:       agentproto.BackendCodex,
	}
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "", "", state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]
	defaults, ok := svc.resolveWorkspaceDefaults(inst, surface, workspaceKey)
	if !ok || defaults.Model != "gpt-team" {
		t.Fatalf("expected team-proxy codex defaults, ok=%t defaults=%#v", ok, defaults)
	}
	surface.CodexProviderID = state.DefaultCodexProviderID
	defaults, ok = svc.resolveWorkspaceDefaults(inst, surface, workspaceKey)
	if !ok || defaults.Model != "gpt-default" {
		t.Fatalf("expected default codex defaults, ok=%t defaults=%#v", ok, defaults)
	}

	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "devseek"
	defaults, ok = svc.resolveWorkspaceDefaults(&state.InstanceRecord{
		InstanceID:      "inst-claude",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}, surface, workspaceKey)
	if !ok || defaults.ReasoningEffort != "max" {
		t.Fatalf("expected devseek claude defaults, ok=%t defaults=%#v", ok, defaults)
	}
	surface.ClaudeProfileID = state.DefaultClaudeProfileID
	defaults, ok = svc.resolveWorkspaceDefaults(&state.InstanceRecord{
		InstanceID:      "inst-claude-default",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
	}, surface, workspaceKey)
	if !ok || defaults.ReasoningEffort != "low" {
		t.Fatalf("expected default claude defaults, ok=%t defaults=%#v", ok, defaults)
	}
}
