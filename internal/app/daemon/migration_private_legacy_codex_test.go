package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyCodexHistoryRequiresUniqueExactMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "01")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "rollout-session-1.jsonl")
	body := []byte("{\"type\":\"session_meta\",\"payload\":{\"id\":\"session-1\",\"cwd\":\"/workspace\"}}\n")
	if err := os.WriteFile(name, body, 0600); err != nil {
		t.Fatal(err)
	}
	relative, hash, err := findLegacyCodexSession(t.Context(), root, "session-1", "/workspace")
	if err != nil || relative == "" || hash == "" {
		t.Fatalf("exact metadata: %v", err)
	}
	if _, _, err = findLegacyCodexSession(t.Context(), root, "session-1", "/different"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("different cwd accepted")
	}
	if err = os.WriteFile(filepath.Join(dir, "duplicate-session-1.jsonl"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = findLegacyCodexSession(t.Context(), root, "session-1", "/workspace"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("ambiguous history accepted")
	}
}
