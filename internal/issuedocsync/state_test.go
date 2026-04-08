package issuedocsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadStateMissingFileReturnsEmptyTrackedShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	state, err := LoadState(path, "kxn/codex-remote-feishu")
	if err != nil {
		t.Fatalf("LoadState missing file error = %v", err)
	}
	if state.Version != 1 || state.Repo != "kxn/codex-remote-feishu" {
		t.Fatalf("unexpected default state: %#v", state)
	}
	if len(state.Issues) != 0 {
		t.Fatalf("expected empty issue map, got %#v", state.Issues)
	}
}

func TestSaveStateRoundTripUsesTrackedIssueList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := NewState("kxn/codex-remote-feishu")
	UpsertRecord(&state, IssueRecord{
		Number:     22,
		Title:      "Headless instance 改用 pool 管理",
		UpdatedAt:  "2026-04-08T02:29:31Z",
		Decision:   "new-doc",
		Reason:     "current docs do not cover the managed headless pool lifecycle",
		TargetDocs: []string{"docs/implemented/managed-headless-pool-design.md"},
		SourceURL:  "https://github.com/kxn/codex-remote-feishu/issues/22",
		RecordedAt: "2026-04-08T04:20:00Z",
	})
	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState error = %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if !strings.Contains(string(payload), "\"issues\": [") {
		t.Fatalf("expected tracked issue list layout, got:\n%s", string(payload))
	}
	reloaded, err := LoadState(path, "kxn/codex-remote-feishu")
	if err != nil {
		t.Fatalf("LoadState round trip error = %v", err)
	}
	if got := reloaded.Issues["22"].Decision; got != "new-doc" {
		t.Fatalf("reloaded decision = %q, want new-doc", got)
	}
}
