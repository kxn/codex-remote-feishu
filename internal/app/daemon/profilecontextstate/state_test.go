package profilecontextstate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStoreMaintainsIndependentImmutablePreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	store := NewStore(path)
	if err := store.EnsureCodexProfile("team-proxy", state.CodexContextModeDefault); err != nil {
		t.Fatalf("EnsureCodexProfile: %v", err)
	}
	if err := store.EnsureClaudeProfile("devseek", state.ClaudeContextModeDefault); err != nil {
		t.Fatalf("EnsureClaudeProfile: %v", err)
	}

	codex, ok := store.CodexCurrent("team-proxy")
	if !ok || codex.Revision != 1 || codex.ETag != state.CodexContextPreferenceETag("team-proxy", 1) {
		t.Fatalf("unexpected initial codex preference: %#v ok=%v", codex, ok)
	}
	claude, ok := store.ClaudeCurrent("devseek")
	if !ok || claude.Revision != 1 || claude.ETag != state.ClaudeContextPreferenceETag("devseek", 1) {
		t.Fatalf("unexpected initial claude preference: %#v ok=%v", claude, ok)
	}

	updated, changed, err := store.UpdateCodex("team-proxy", state.CodexContextModePrice272K, codex.ETag)
	if err != nil || !changed || updated.Revision != 2 {
		t.Fatalf("UpdateCodex() = %#v changed=%v err=%v", updated, changed, err)
	}
	if _, _, err := store.UpdateCodex("team-proxy", state.CodexContextModeExtended, codex.ETag); !errors.Is(err, ErrETagMismatch) {
		t.Fatalf("stale UpdateCodex error = %v, want ErrETagMismatch", err)
	}
	unchanged, changed, err := store.UpdateCodex("team-proxy", state.CodexContextModePrice272K, updated.ETag)
	if err != nil || changed || unchanged.Revision != 2 {
		t.Fatalf("no-op UpdateCodex() = %#v changed=%v err=%v", unchanged, changed, err)
	}
	if historical, ok := store.CodexRevision("team-proxy", 1); !ok || historical.Mode != state.CodexContextModeDefault {
		t.Fatalf("old codex preference not retained: %#v ok=%v", historical, ok)
	}

	if err := store.PruneCodexHistory("team-proxy", nil); err != nil {
		t.Fatalf("PruneCodexHistory: %v", err)
	}
	if _, ok := store.CodexRevision("team-proxy", 1); ok {
		t.Fatal("unretained old preference revision survived pruning")
	}
	if current, ok := store.CodexRevision("team-proxy", 2); !ok || current.Revision != 2 {
		t.Fatalf("current preference was pruned: %#v ok=%v", current, ok)
	}

	loaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if current, ok := loaded.CodexCurrent("team-proxy"); !ok || current.Revision != 2 {
		t.Fatalf("reloaded preference = %#v ok=%v", current, ok)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("preference store mode = %o, want 600", info.Mode().Perm())
		}
	}
}

func TestLoadStoreRejectsCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := LoadStore(path); err == nil {
		t.Fatal("LoadStore() accepted corrupt preference state")
	}
}

func TestLoadStoreRejectsStaleCurrentAndNonCanonicalPreferenceState(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"codex":{"team-proxy":{"profileID":"team-proxy","currentRevision":1,"revisions":[{"profileID":"team-proxy","revision":1,"mode":"codex_default"},{"profileID":"team-proxy","revision":2,"mode":"price_guard_272k"}]}}}`,
		`{"version":1,"codex":{"team-proxy":{"profileID":"team-proxy","currentRevision":1,"revisions":[{"profileID":"team-proxy","revision":1,"mode":" codex_default "}]}}}`,
		`{"version":1,"codex":{" team-proxy ":{"profileID":"team-proxy","currentRevision":1,"revisions":[{"profileID":"team-proxy","revision":1,"mode":"codex_default"}]}}}`,
	} {
		path := filepath.Join(t.TempDir(), StateFileName)
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if _, err := LoadStore(path); err == nil {
			t.Fatalf("LoadStore() accepted non-canonical preference state: %s", raw)
		}
	}
}
