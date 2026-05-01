package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexProvidersMaterializeBuiltInAndDisambiguateDuplicateNames(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{
		{ID: "team-proxy", Name: "Team Proxy"},
		{ID: "team-proxy-2", Name: "Team Proxy"},
	})

	got := svc.CodexProviders()
	if len(got) != 3 {
		t.Fatalf("expected default + 2 custom providers, got %#v", got)
	}
	if got[0].ID != state.DefaultCodexProviderID || got[0].Name != state.DefaultCodexProviderName || !got[0].BuiltIn {
		t.Fatalf("unexpected built-in default provider: %#v", got[0])
	}
	if got[1].ID != "team-proxy" || got[1].Name != "Team Proxy" {
		t.Fatalf("unexpected first custom provider: %#v", got[1])
	}
	if got[2].ID != "team-proxy-2" || got[2].Name != "Team Proxy" {
		t.Fatalf("unexpected second custom provider: %#v", got[2])
	}
}
