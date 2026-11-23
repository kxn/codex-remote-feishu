package ingressowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type liveCoverageFixture struct{ current string }

func (v *liveCoverageFixture) VerifyGatewayCoverage(_ context.Context, c GatewayCoverage, proof []byte) error {
	if c.CutoverID != v.current || string(proof) != v.current {
		return errors.New("gateway hook or exclusive lock is not current")
	}
	return nil
}

func TestBoundDrainCoverageSurvivesVerifiedRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "neutral.db")
	r := openTest(t, path)
	event := testEvent("historical-restart", Message)
	event.ChatType = "p2p"
	event.ChatID = "chat"
	event.MessageID = "message"
	hash, err := EventFactHash(event)
	if err != nil {
		t.Fatal(err)
	}
	witness := scopeWitnessVerifier{event}
	r.SetOwnerScopeBindingVerifier(witness)
	b := OwnerScopeBinding{SourceEventKey: event.Key, SourceFactHash: hash, ScopeID: "scope", PrincipalID: "principal", RoleID: "role", GatewayID: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", OpenID: "user"}
	bound, err := r.ImportOwnerScopeBinding(ctx, b, []byte("durable-source"))
	if err != nil {
		t.Fatal(err)
	}
	live := &liveCoverageFixture{current: "startup-one"}
	r.SetGatewayCoverageVerifier(live)
	epoch, err := r.NextGatewayCoverageEpoch(ctx, "tenant", "app", "gateway", 1)
	if err != nil || epoch != 1 {
		t.Fatal("initial epoch")
	}
	coverage := GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: r.RouterID(), Epoch: epoch, CutoverID: live.current}
	if err = r.RecordGatewayCoverage(ctx, coverage, []byte(live.current)); err != nil {
		t.Fatal(err)
	}
	proof, err := r.BeginBoundOwnerMigration(ctx, bound, "migration")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	r.SetOwnerScopeBindingVerifier(witness)
	live.current = "startup-two"
	r.SetGatewayCoverageVerifier(live)
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", proof); err == nil {
		t.Fatal("dead startup proof remained valid")
	}
	epoch, err = r.NextGatewayCoverageEpoch(ctx, "tenant", "app", "gateway", 1)
	if err != nil || epoch != 2 {
		t.Fatal("restart epoch")
	}
	coverage.Epoch = epoch
	coverage.CutoverID = live.current
	if err = r.RecordGatewayCoverage(ctx, coverage, []byte("forged")); err == nil {
		t.Fatal("unverified successor accepted")
	}
	if err = r.RecordGatewayCoverage(ctx, coverage, []byte(live.current)); err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", proof); err != nil {
		t.Fatalf("verified continuation: %v", err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	r.SetOwnerScopeBindingVerifier(witness)
	live.current = "startup-three"
	r.SetGatewayCoverageVerifier(live)
	epoch, err = r.NextGatewayCoverageEpoch(ctx, "tenant", "app", "gateway", 1)
	if err != nil || epoch != 3 {
		t.Fatal("second restart epoch")
	}
	coverage.Epoch = epoch
	coverage.CutoverID = live.current
	if err = r.RecordGatewayCoverage(ctx, coverage, []byte(live.current)); err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", proof); err != nil {
		t.Fatalf("multi-restart continuation: %v", err)
	}
	if _, err = r.db.ExecContext(ctx, `UPDATE gateway_coverage_lineage SET next_body='{}' WHERE previous_digest=?`, proof.CoverageDigest); err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", proof); err == nil {
		t.Fatal("tampered lineage accepted")
	}
}
