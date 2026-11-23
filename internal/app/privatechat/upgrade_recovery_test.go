package privatechat

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestUpgradeRecoveryPlanIsAtomicWithCheckpointAndReplaysAfterReopen(t *testing.T) {
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	plan := UpgradeRecoveryPlan{MaintenanceID: "upgrade", MaintenanceEpoch: 1, Owner: owner, Generation: 1, OldOwner: agentproto.NativeOwner{CarrierIncarnation: "old"}}
	effects := []Effect{{ID: "launch", Lane: "background", Body: []byte(`{"kind":"notice"}`)}}
	cp := Checkpoint{Owner: owner, Generation: 1, Revision: 1, Core: core}
	ref := migrationResumeRef{Upgrade: &upgradeRecoveryRef{Plan: plan, EffectID: "launch"}}
	if err = j.commitCheckpointExtended(t.Context(), cp, 0, nil, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err == nil {
		t.Fatal("plan committed without its effect")
	}
	if _, ok, err := j.UpgradeRecoverySelection(t.Context(), owner, 1, "launch"); err != nil || ok {
		t.Fatalf("rolled back plan visible: %v %v", ok, err)
	}
	if err = j.commitCheckpointExtended(t.Context(), cp, 0, effects, 0, nil, nil, nil, nil, nil, nil, nil, &ref); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	got, ok, err := j.UpgradeRecoverySelection(t.Context(), owner, 1, "launch")
	if err != nil || !ok || got != plan {
		t.Fatalf("recovered plan: %+v %v %v", got, ok, err)
	}
	foreign := owner
	foreign.UserID = "other"
	if _, ok, err = j.UpgradeRecoverySelection(t.Context(), foreign, 1, "launch"); err != nil || ok {
		t.Fatalf("foreign owner saw plan: %v %v", ok, err)
	}
}
