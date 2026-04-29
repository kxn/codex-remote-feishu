package claudeworkspaceprofile

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := LoadStore(StatePath(stateDir))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}

	key := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo", agentproto.BackendClaude, "devseek")
	if err := store.Put(key, state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: " HIGH ",
		AccessMode:      "confirm",
		PlanMode:        "on",
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	reloaded, err := LoadStore(StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, ok := reloaded.Get(key)
	if !ok {
		t.Fatal("expected stored claude workspace profile snapshot")
	}
	if got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
		PlanMode:        state.PlanModeSettingOn,
	}) {
		t.Fatalf("unexpected stored snapshot: %#v", got)
	}
}
