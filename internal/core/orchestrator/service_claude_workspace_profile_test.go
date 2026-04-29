package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeWorkspaceProfileSnapshotPartitionsAndClearsUnsupportedModelOverride(t *testing.T) {
	now := time.Date(2026, 4, 29, 8, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	surface.ProductMode = state.ProductModeNormal
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "profile-a"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	surface.PlanMode = state.PlanModeSettingOn
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "should-not-persist",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	svc.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	surface.ClaudeProfileID = "profile-b"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-b"
	surface.PlanMode = state.PlanModeSettingOff
	surface.PromptOverride = state.ModelConfigRecord{
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeFullAccess,
	}
	svc.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	keyA := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo-a", agentproto.BackendClaude, "profile-a")
	keyB := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo-b", agentproto.BackendClaude, "profile-b")
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[keyA]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
		PlanMode:        state.PlanModeSettingOn,
	}) {
		t.Fatalf("unexpected profile A snapshot: %#v", got)
	}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[keyB]; got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeFullAccess,
		PlanMode:        state.PlanModeSettingOff,
	}) {
		t.Fatalf("unexpected profile B snapshot: %#v", got)
	}

	surface.ClaudeProfileID = "profile-a"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	surface.PlanMode = state.PlanModeSettingOff
	surface.PromptOverride = state.ModelConfigRecord{Model: "clear-me"}
	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	if surface.PlanMode != state.PlanModeSettingOn {
		t.Fatalf("expected plan mode restored from snapshot, got %q", surface.PlanMode)
	}
	if surface.PromptOverride.Model != "" || surface.PromptOverride.ReasoningEffort != "high" || surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected restored snapshot to clear model and restore reasoning/access, got %#v", surface.PromptOverride)
	}

	surface.ClaudeProfileID = "profile-c"
	surface.ClaimedWorkspaceKey = "/data/dl/repo-c"
	surface.PlanMode = state.PlanModeSettingOn
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "clear-me-too",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	if surface.PlanMode != state.PlanModeSettingOff {
		t.Fatalf("expected missing snapshot to reset plan mode, got %q", surface.PlanMode)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected missing snapshot to clear prompt override, got %#v", surface.PromptOverride)
	}
}
