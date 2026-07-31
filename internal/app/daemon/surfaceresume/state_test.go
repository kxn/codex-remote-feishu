package surfaceresume

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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

func TestCanonicalizeEntriesRetainsConflictingCodexSelectionDiagnostic(t *testing.T) {
	entries, changed := CanonicalizeEntries(map[string]Entry{
		"feishu:main:user:ou_old": {
			SurfaceSessionID: "feishu:main:user:ou_old", GatewayID: "main", ChatID: "oc_chat", ActorUserID: "ou_old",
			ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex), CodexProviderID: "proxy-a",
			UpdatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		"feishu:main:user:ou_new": {
			SurfaceSessionID: "feishu:main:user:ou_new", GatewayID: "main", ChatID: "oc_chat", ActorUserID: "ou_new",
			ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex), CodexProviderID: "proxy-b",
			UpdatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		},
	})
	if !changed || len(entries) != 1 {
		t.Fatalf("canonical entries = %#v changed=%v, want one merged diagnostic entry", entries, changed)
	}
	for _, entry := range entries {
		raw, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal canonical entry: %v", err)
		}
		if !strings.Contains(string(raw), `"codexProfileSelectionStatus":"profile_selection_conflict"`) {
			t.Fatalf("canonicalization discarded conflicting provider evidence: %s", raw)
		}
	}
}

func TestCanonicalizeEntryProfileSelectionUsesProfileAsCanonicalOwner(t *testing.T) {
	entry := CanonicalizeEntryProfileSelection(Entry{
		ProductMode:     string(state.ProductModeNormal),
		Backend:         string(agentproto.BackendCodex),
		CodexProviderID: "legacy-new",
		CodexProfileID:  "canonical-old",
	})
	if entry.CodexProfileID != "canonical-old" || entry.CodexProviderID != "canonical-old" {
		t.Fatalf("canonical profile selection drifted: %#v", entry)
	}
}

func TestNormalizeEntryClearsCodexProfileStateOutsideCodexBackend(t *testing.T) {
	entry, ok := NormalizeEntry(Entry{
		SurfaceSessionID:            "surface-1",
		ProductMode:                 string(state.ProductModeNormal),
		Backend:                     string(agentproto.BackendClaude),
		CodexProviderID:             "team-proxy",
		CodexProfileID:              "team-proxy",
		CodexProfileSelectionStatus: CodexProfileSelectionStatusConflict,
		CodexAdmissionRef: &state.CodexAdmissionRef{
			ProfileRef:           state.CodexProfileRef{ID: "team-proxy", Revision: 1},
			ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: "team-proxy", Revision: 1},
		},
	})
	if !ok {
		t.Fatal("expected normalized entry")
	}
	if entry.CodexProviderID != "" || entry.CodexProfileID != "" || entry.CodexProfileSelectionStatus != "" || entry.CodexAdmissionRef != nil {
		t.Fatalf("non-Codex entry retained Codex profile state: %#v", entry)
	}
}
