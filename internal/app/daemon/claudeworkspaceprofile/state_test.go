package claudeworkspaceprofile

import (
	"encoding/json"
	"os"
	"strings"
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
		AccessMode:      " full ",
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
		AccessMode:      agentproto.AccessModeFullAccess,
	}) {
		t.Fatalf("unexpected stored snapshot: %#v", got)
	}
}

func TestStoreDropsLegacyPlanFieldsOnLoadButKeepsAccess(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := StatePath(stateDir)
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo", agentproto.BackendClaude, "devseek")
	raw, err := json.MarshalIndent(map[string]any{
		"version": 1,
		"entries": map[string]any{
			key: map[string]string{
				"ReasoningEffort": "max",
				"AccessMode":      "confirm",
				"PlanMode":        "on",
			},
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy state: %v", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if !store.Dirty() {
		t.Fatal("expected legacy plan field to mark store dirty")
	}
	got, ok := store.Get(key)
	if !ok {
		t.Fatal("expected stored claude workspace profile snapshot")
	}
	if got != (state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "max",
		AccessMode:      agentproto.AccessModeConfirm,
	}) {
		t.Fatalf("expected reasoning and access to survive legacy load, got %#v", got)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sanitized, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sanitized state: %v", err)
	}
	text := string(sanitized)
	if !strings.Contains(text, "AccessMode") || strings.Contains(text, "PlanMode") {
		t.Fatalf("expected access to be preserved and legacy plan field removed after save, got %s", text)
	}
}
