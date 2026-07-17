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
