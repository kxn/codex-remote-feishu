package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSurfaceInstanceCompatibilityPrefersExpectedProviderOverStaleCache(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider(
		"feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user",
		state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "team-proxy",
		state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	oldRef := &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: "default", Revision: 1}}
	oldContract := &state.CodexConnectionContract{ConnectionContractID: "old-contract"}
	surface.CodexAdmissionRef = oldRef
	surface.CodexConnectionContract = oldContract
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{}

	inst := &state.InstanceRecord{
		InstanceID:              "inst-old",
		Backend:                 agentproto.BackendCodex,
		CodexProviderID:         "default",
		CodexAdmissionRef:       oldRef,
		CodexConnectionContract: oldContract,
		Online:                  true,
	}
	if svc.surfaceInstanceCompatibleForAttach(surface, inst) {
		t.Fatal("stale codex contract cache must not make an old-provider instance compatible")
	}
}

func TestSurfaceInstanceCompatibilityKeepsSameProviderRevisionPrecision(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider(
		"feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user",
		state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "team-proxy",
		state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	newRef := &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: "team-proxy", Revision: 2}}
	newContract := &state.CodexConnectionContract{ConnectionContractID: "new-contract"}
	surface.CodexAdmissionRef = newRef
	surface.CodexConnectionContract = newContract

	inst := &state.InstanceRecord{
		InstanceID:              "inst-new",
		Backend:                 agentproto.BackendCodex,
		CodexProviderID:         "team-proxy",
		CodexAdmissionRef:       &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: "team-proxy", Revision: 1}},
		CodexConnectionContract: &state.CodexConnectionContract{ConnectionContractID: "old-contract"},
		Online:                  true,
	}
	if svc.surfaceInstanceCompatibleForAttach(surface, inst) {
		t.Fatal("same provider with different contract revision must stay incompatible")
	}
}

func TestSurfaceInstanceCompatibilityRejectsDifferentOpenCodeProfile(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract(
		"feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user",
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
		state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]

	inst := &state.InstanceRecord{
		InstanceID:        "inst-other",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_other",
		Online:            true,
	}
	if svc.surfaceInstanceCompatibleForAttach(surface, inst) {
		t.Fatal("different opencode profile must be incompatible")
	}
}

func TestSurfaceInstanceCompatibilityRejectsStaleOpenCodeAdmissionRevision(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract(
		"feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user",
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
		state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}

	stale := &state.InstanceRecord{
		InstanceID:           "inst-stale",
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    "op_team",
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 6}},
		Online:               true,
	}
	if svc.surfaceInstanceCompatibleForAttach(surface, stale) {
		t.Fatal("same opencode profile with stale admission revision must be incompatible")
	}

	current := &state.InstanceRecord{
		InstanceID:           "inst-current",
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    "op_team",
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}},
		Online:               true,
	}
	if !svc.surfaceInstanceCompatibleForAttach(surface, current) {
		t.Fatal("same opencode profile and admission revision should be compatible")
	}
}
