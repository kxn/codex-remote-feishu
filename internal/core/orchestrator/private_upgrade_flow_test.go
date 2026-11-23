package orchestrator

import (
	"testing"
	"time"
)

func TestPrivateUpgradeFlowSurvivesCheckpointWithoutCrossOwner(t *testing.T) {
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	s.MaterializeSurface("s", "g", "c", "user")
	flow := &PrivateUpgradeFlow{ID: "flow", OwnerUserID: "user", Phase: "confirm", TargetVersion: "v2", StateFingerprint: "fingerprint", ExpiresAt: time.Now().Add(time.Hour)}
	if err := s.SetPrivateUpgradeFlow("s", flow); err != nil {
		t.Fatal(err)
	}
	cp, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(nil, Config{PrivateApplication: true}, nil)
	if err = restarted.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	got := restarted.PrivateUpgradeFlow("s")
	if got == nil || got.ID != flow.ID || got.StateFingerprint != flow.StateFingerprint {
		t.Fatalf("restored: %#v", got)
	}
	got.OwnerUserID = "other"
	if restarted.PrivateUpgradeFlow("s").OwnerUserID != "user" {
		t.Fatal("returned flow aliases core")
	}
	if err = restarted.SetPrivateUpgradeFlow("s", got); err == nil {
		t.Fatal("foreign flow accepted")
	}
}
