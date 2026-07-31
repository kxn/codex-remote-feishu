package surfaceresume

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestNormalizeEntryRepairsHeadlessWorkspaceOutsideThreadPath(t *testing.T) {
	entry, ok := NormalizeEntry(Entry{
		SurfaceSessionID:   "surface-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    "/data/projects/signal",
		ResumeWorkspaceKey: "/data/.local/state/codex-remote",
		ResumeHeadless:     true,
	})
	if !ok {
		t.Fatal("expected normalized entry")
	}
	if entry.ResumeWorkspaceKey != "/data/projects/signal" {
		t.Fatalf("expected stale workspace to fall back to thread CWD, got %#v", entry)
	}
}

func TestNormalizeEntryPreservesHeadlessWorkspaceContainingThreadPath(t *testing.T) {
	entry, ok := NormalizeEntry(Entry{
		SurfaceSessionID:   "surface-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    "/data/projects/droid/web",
		ResumeWorkspaceKey: "/data/projects/droid",
		ResumeHeadless:     true,
	})
	if !ok {
		t.Fatal("expected normalized entry")
	}
	if entry.ResumeWorkspaceKey != "/data/projects/droid" {
		t.Fatalf("expected containing workspace root to be retained, got %#v", entry)
	}
}

func TestNormalizeEntryResolvesHeadlessWorkspaceClaimKey(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	threadDir := filepath.Join(target, "pkg")
	if err := os.MkdirAll(threadDir, 0o755); err != nil {
		t.Fatalf("mkdir thread dir: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	threadCWD := filepath.Join(link, "pkg")

	entry, ok := NormalizeEntry(Entry{
		SurfaceSessionID:   "surface-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    threadCWD,
		ResumeWorkspaceKey: link,
		ResumeHeadless:     true,
	})
	if !ok {
		t.Fatal("expected normalized entry")
	}
	if want := state.ResolveWorkspaceClaimKey(threadCWD); entry.ResumeThreadCWD != want {
		t.Fatalf("thread cwd = %q, want claim key %q", entry.ResumeThreadCWD, want)
	}
	if want := state.ResolveHeadlessResumeWorkspaceKey(link, threadCWD); entry.ResumeWorkspaceKey != want {
		t.Fatalf("workspace key = %q, want headless resume key %q", entry.ResumeWorkspaceKey, want)
	}
}

func TestLoadStoreMarksRepairedHeadlessWorkspaceDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	raw := []byte(`{"version":1,"entries":{"surface-1":{"surfaceSessionID":"surface-1","productMode":"normal","resumeThreadID":"thread-1","resumeThreadCWD":"/data/projects/signal","resumeWorkspaceKey":"/data/.local/state/codex-remote","resumeHeadless":true}}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}
	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if !store.Dirty() {
		t.Fatal("expected repaired state to be marked dirty for persistence")
	}
}
