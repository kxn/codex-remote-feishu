package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexProfilesMaterializeNativeAndSortCustomProfiles(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: "team-proxy", Name: "Team Proxy"},
		{ID: "team-proxy-2", Name: "Team Proxy"},
	})

	got := svc.CodexProfiles()
	if len(got) != 3 {
		t.Fatalf("expected native + 2 custom profiles, got %#v", got)
	}
	if got[0].ID != state.NativeCodexProfileID || got[0].Name != state.DefaultCodexProfileName || got[0].Kind != state.CodexProfileKindNative {
		t.Fatalf("unexpected native profile: %#v", got[0])
	}
	if got[1].ID != "team-proxy" || got[1].Name != "Team Proxy" {
		t.Fatalf("unexpected first custom profile: %#v", got[1])
	}
	if got[2].ID != "team-proxy-2" || got[2].Name != "Team Proxy" {
		t.Fatalf("unexpected second custom profile: %#v", got[2])
	}
}

func TestCodexProfilesAlwaysIncludeNativeDefault(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles(nil)

	profiles := svc.CodexProfiles()
	if len(profiles) != 1 || profiles[0].ID != state.NativeCodexProfileID {
		t.Fatalf("native profile default missing: %#v", profiles)
	}
}
