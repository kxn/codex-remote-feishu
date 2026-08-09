package agentproto

import "testing"

func TestLegacyCodexDefaultCapabilitiesDoNotAssumeModelCatalog(t *testing.T) {
	caps := DefaultCapabilitiesForBackend(BackendCodex)
	if caps.ModelCatalog {
		t.Fatalf("legacy codex defaults must not assume model catalog support: %#v", caps)
	}
	if !caps.ThreadsRefresh || !caps.TurnSteer || !caps.RequestRespond || !caps.ResumeByThreadID || !caps.VSCodeMode {
		t.Fatalf("expected existing codex defaults to stay enabled, got %#v", caps)
	}
}

func TestExplicitModelCatalogCapabilityIsPreserved(t *testing.T) {
	caps := EffectiveCapabilitiesForBackend(BackendCodex, Capabilities{ModelCatalog: true})
	if !caps.ModelCatalog {
		t.Fatalf("expected explicit model catalog support to be preserved, got %#v", caps)
	}
}

func TestOpenCodeBackendIdentityAndCapabilities(t *testing.T) {
	if got := NormalizeBackend(BackendOpenCode); got != BackendOpenCode {
		t.Fatalf("NormalizeBackend(opencode) = %q, want %q", got, BackendOpenCode)
	}
	if got := BackendDisplayName(BackendOpenCode); got != "OpenCode" {
		t.Fatalf("BackendDisplayName(opencode) = %q", got)
	}
	caps := DefaultCapabilitiesForBackend(BackendOpenCode)
	if !caps.ThreadsRefresh || !caps.RequestRespond || !caps.SessionCatalog || !caps.ResumeByThreadID || !caps.RequiresCWDForResume {
		t.Fatalf("opencode required capabilities missing: %#v", caps)
	}
	if caps.TurnSteer || caps.VSCodeMode || caps.ModelCatalog {
		t.Fatalf("opencode default capabilities should not overstate unsupported features: %#v", caps)
	}
}

func TestParseBackendDistinguishesEmptyLegacyDefaultFromUnknown(t *testing.T) {
	if got, ok := ParseBackend(""); got != BackendCodex || !ok {
		t.Fatalf("ParseBackend(empty) = %q/%v, want codex/true", got, ok)
	}
	if got, ok := ParseBackend(" OPENCODE "); got != BackendOpenCode || !ok {
		t.Fatalf("ParseBackend(opencode) = %q/%v, want opencode/true", got, ok)
	}
	if got, ok := ParseBackend("mystery"); got != "" || ok {
		t.Fatalf("ParseBackend(unknown) = %q/%v, want empty/false", got, ok)
	}
}

func TestEffectiveHelloBackendPreservesUnknownBackend(t *testing.T) {
	hello := Hello{Instance: InstanceHello{Backend: Backend(" mystery "), CodexProviderID: "team-proxy"}}
	if got := EffectiveHelloBackend(hello); got != Backend("mystery") {
		t.Fatalf("EffectiveHelloBackend unknown = %q, want mystery", got)
	}
	caps := EffectiveHelloCapabilities(hello)
	if caps.VSCodeMode || caps.ThreadsRefresh || caps.TurnSteer {
		t.Fatalf("unknown hello backend inherited codex defaults: %#v", caps)
	}
}
