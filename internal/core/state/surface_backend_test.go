package state

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestIsHeadlessProductMode(t *testing.T) {
	if !IsHeadlessProductMode(ProductModeNormal) {
		t.Fatal("expected ProductModeNormal to be treated as headless")
	}
	if IsHeadlessProductMode(ProductModeVSCode) {
		t.Fatal("expected ProductModeVSCode to be non-headless")
	}
}

func TestSurfaceModeAlias(t *testing.T) {
	tests := []struct {
		name    string
		mode    ProductMode
		backend agentproto.Backend
		want    string
	}{
		{name: "codex headless", mode: ProductModeNormal, backend: agentproto.BackendCodex, want: "codex"},
		{name: "claude headless", mode: ProductModeNormal, backend: agentproto.BackendClaude, want: "claude"},
		{name: "vscode forces codex alias", mode: ProductModeVSCode, backend: agentproto.BackendClaude, want: "vscode"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SurfaceModeAlias(tc.mode, tc.backend); got != tc.want {
				t.Fatalf("SurfaceModeAlias(%q, %q) = %q, want %q", tc.mode, tc.backend, got, tc.want)
			}
		})
	}
}

func TestSurfaceDesiredBackendContractPreservesCrossBackendStoredIDs(t *testing.T) {
	surface := &SurfaceConsoleRecord{
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		CodexProviderID: "team-proxy",
		ClaudeProfileID: "devseek",
	}
	contract := SurfaceDesiredBackendContract(surface)
	if contract.Backend != agentproto.BackendClaude {
		t.Fatalf("unexpected backend: %#v", contract)
	}
	if contract.CodexProviderID != "team-proxy" {
		t.Fatalf("expected codex provider storage to stay intact, got %#v", contract)
	}
	if contract.ClaudeProfileID != "devseek" {
		t.Fatalf("expected claude profile storage to stay intact, got %#v", contract)
	}
	if got := EffectiveSurfaceCodexProviderID(contract); got != "" {
		t.Fatalf("expected inactive codex provider projection to stay hidden, got %q", got)
	}
	if got := EffectiveSurfaceClaudeProfileID(contract); got != "devseek" {
		t.Fatalf("expected active claude profile projection, got %q", got)
	}
}
