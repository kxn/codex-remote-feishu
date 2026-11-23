package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateCodexContractProjectionIsOwnerScopedAndHeld(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	for _, id := range []string{"a", "b"} {
		s.MaterializeSurface(id, "g", id, id)
	}
	a, b := s.root.Surfaces["a"], s.root.Surfaces["b"]
	a.Backend = agentproto.BackendCodex
	b.Backend = agentproto.BackendCodex
	profile := state.EffectiveSurfaceCodexProfileID(state.SurfaceDesiredBackendContract(a))
	ref := &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: profile, Revision: 2}, ContextPreferenceRef: state.CodexContextPreferenceRef{ProfileID: profile, Revision: 3}}
	s.privateRecoveryHolds["a"] = PrivateCheckpoint{Version: 1, SurfaceID: "a"}
	events, changed, err := s.ApplyPrivateCodexContracts("a", "v1", map[string]PrivateCodexContract{profile: {Admission: ref}})
	if err != nil || !changed || len(events) != 0 {
		t.Fatalf("held projection: %#v %v", events, err)
	}
	if a.CodexAdmissionRef == nil || a.CodexAdmissionRef.ProfileRef.Revision != 2 || !a.ContractRefreshPending {
		t.Fatal("projection or deferred refresh missing")
	}
	if b.CodexAdmissionRef != nil {
		t.Fatal("another owner changed")
	}
	cp, err := s.ExportPrivateCheckpoint("a")
	if err != nil {
		t.Fatal(err)
	}
	restored := NewService(nil, Config{PrivateApplication: true}, nil)
	if err = restored.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	if !restored.Surface("a").ContractRefreshPending {
		t.Fatal("deferred contract refresh not persisted")
	}
	_, changed, err = restored.ApplyPrivateCodexContracts("a", "v1", nil)
	if err != nil || changed {
		t.Fatalf("version replay changed core: %v", err)
	}
}
