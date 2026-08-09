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
		{name: "opencode headless", mode: ProductModeNormal, backend: agentproto.BackendOpenCode, want: "opencode"},
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

func TestSurfaceDesiredBackendContractProjectsOpenCodeBinding(t *testing.T) {
	surface := &SurfaceConsoleRecord{
		ProductMode:       ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		CodexProviderID:   "team-proxy",
		ClaudeProfileID:   "devseek",
		OpenCodeProfileID: "op_team",
	}
	contract := SurfaceDesiredBackendContract(surface)
	if contract.Backend != agentproto.BackendOpenCode {
		t.Fatalf("unexpected backend: %#v", contract)
	}
	if contract.CodexProviderID != "" || contract.ClaudeProfileID != "" {
		t.Fatalf("expected inactive backend bindings to stay hidden, got %#v", contract)
	}
	if contract.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected opencode profile to survive projection, got %#v", contract)
	}
	if got := EffectiveSurfaceOpenCodeProfileID(contract); got != "op_team" {
		t.Fatalf("expected active opencode profile projection, got %q", got)
	}
}

func TestHeadlessLaunchContractCarriesOpenCodeAdmissionRef(t *testing.T) {
	ref := &OpenCodeAdmissionRef{ProfileRef: OpenCodeProfileRef{ID: "op_team", Revision: 7}}
	surface := &SurfaceConsoleRecord{
		ProductMode:            ProductModeNormal,
		Backend:                agentproto.BackendOpenCode,
		OpenCodeProfileID:      "op_team",
		OpenCodeAdmissionRef:   ref,
		CodexAdmissionRef:      &CodexAdmissionRef{ProfileRef: CodexProfileRef{ID: "cp_team", Revision: 1}, ContextPreferenceRef: CodexContextPreferenceRef{ProfileID: "cp_team", Revision: 1}},
		CodexProviderID:        "team-proxy",
		ClaudeProfileID:        "devseek",
		ContractRefreshPending: true,
	}
	contract := HeadlessLaunchContractFromSurface(surface)
	if contract.Backend != agentproto.BackendOpenCode {
		t.Fatalf("unexpected backend: %#v", contract)
	}
	if contract.OpenCodeProfileID != "op_team" {
		t.Fatalf("unexpected opencode profile: %#v", contract)
	}
	if contract.OpenCodeAdmissionRef == nil || contract.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected opencode admission ref to be cloned into launch contract, got %#v", contract)
	}
	if contract.CodexAdmissionRef != nil || contract.CodexProviderID != "" || contract.ClaudeProfileID != "" {
		t.Fatalf("expected inactive backend launch fields to be hidden, got %#v", contract)
	}
}

func TestWorkspaceDefaultsStorageKeyPartitionsOpenCodeProfiles(t *testing.T) {
	left := WorkspaceDefaultsStorageKey("/repo", OpenCodeInstanceBackendContract("op_left"))
	right := WorkspaceDefaultsStorageKey("/repo", OpenCodeInstanceBackendContract("op_right"))
	if left == "" || right == "" {
		t.Fatalf("expected opencode workspace default keys, got %q %q", left, right)
	}
	if left == right {
		t.Fatalf("expected opencode workspace defaults to partition by profile, got %q", left)
	}
	if got := WorkspaceDefaultsIdentity(OpenCodeInstanceBackendContract("")); got != DefaultOpenCodeProfileID {
		t.Fatalf("default opencode workspace identity = %q, want %q", got, DefaultOpenCodeProfileID)
	}
}

func TestSurfaceDesiredBackendContractProjectsOnlyActiveBackendBinding(t *testing.T) {
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
	if contract.CodexProviderID != "" {
		t.Fatalf("expected inactive codex provider to stay hidden in active contract, got %#v", contract)
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
	if surface.CodexProviderID != "team-proxy" || surface.ClaudeProfileID != "devseek" {
		t.Fatalf("expected source surface storage to remain intact, got %#v", surface)
	}
}

func TestPersistedSurfaceBackendContractCanonicalizesLegacyClaudeProfileProjection(t *testing.T) {
	contract := PersistedSurfaceBackendContract(ProductModeNormal, agentproto.BackendCodex, "", "devseek", "")
	if contract.Backend != agentproto.BackendClaude {
		t.Fatalf("expected legacy persisted claude profile to canonicalize back to claude, got %#v", contract)
	}
	if contract.ClaudeProfileID != "devseek" {
		t.Fatalf("expected claude profile to survive canonicalization, got %#v", contract)
	}
	if contract.CodexProviderID != "" {
		t.Fatalf("expected inactive codex provider projection to stay hidden, got %#v", contract)
	}
}

func TestHeadlessLaunchContractCarriesClaudeReasoningEffort(t *testing.T) {
	surface := &SurfaceConsoleRecord{
		ProductMode:     ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		PromptOverride:  ModelConfigRecord{ReasoningEffort: " HIGH "},
	}
	contract := HeadlessLaunchContractFromSurface(surface)
	if contract.Backend != agentproto.BackendClaude {
		t.Fatalf("unexpected backend: %#v", contract)
	}
	if contract.ClaudeProfileID != "devseek" {
		t.Fatalf("unexpected claude profile: %#v", contract)
	}
	if contract.ClaudeReasoningEffort != "high" {
		t.Fatalf("unexpected reasoning effort: %#v", contract)
	}

	inst := &InstanceRecord{
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       "devseek",
		ClaudeReasoningEffort: " HIGH ",
	}
	observed := HeadlessLaunchContractFromInstance(inst)
	if observed.ClaudeReasoningEffort != "high" {
		t.Fatalf("unexpected observed reasoning effort: %#v", observed)
	}
}
