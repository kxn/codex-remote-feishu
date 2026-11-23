package ingressowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type scopeWitnessVerifier struct{ event Event }

func (v scopeWitnessVerifier) VerifyOwnerScopeBinding(_ context.Context, b OwnerScopeBinding, neutral *Record, proof []byte) error {
	hash, err := EventFactHash(v.event)
	if err != nil {
		return err
	}
	if string(proof) != "durable-source" || b.SourceFactHash != hash || b.SourceEventKey != v.event.Key || b.ScopeID != "scope" {
		return errors.New("source proof mismatch")
	}
	if neutral != nil {
		actual, err := EventFactHash(neutral.Event)
		if err != nil || actual != hash {
			return ErrConflict
		}
	}
	return nil
}

type coverageWitnessVerifier struct{}

type readyWitnessVerifier struct{}

func (readyWitnessVerifier) VerifyTargetReady(_ context.Context, _ ImportProof, proof []byte) error {
	if string(proof) != "verified-ready" {
		return errors.New("unverified target")
	}
	return nil
}

func (coverageWitnessVerifier) VerifyGatewayCoverage(_ context.Context, c GatewayCoverage, proof []byte) error {
	if c.CutoverID != "cutover" || string(proof) != "verified-coverage" {
		return errors.New("unverified coverage")
	}
	return nil
}
func TestHistoricalScopeImportCreatesOpaqueOwnerAndBoundDrain(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	defer r.Close()
	event := testEvent("historical", Message)
	event.ChatType = "p2p"
	event.ChatID = "chat"
	event.MessageID = "message"
	hash, err := EventFactHash(event)
	if err != nil {
		t.Fatal(err)
	}
	r.SetOwnerScopeBindingVerifier(scopeWitnessVerifier{event})
	b := OwnerScopeBinding{SourceEventKey: event.Key, SourceFactHash: hash, ScopeID: "scope", PrincipalID: "principal", RoleID: "role", GatewayID: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", OpenID: "user"}
	if _, err = r.ImportOwnerScopeBinding(ctx, b, []byte("forged")); err == nil {
		t.Fatal("forged source accepted")
	}
	bound, err := r.ImportOwnerScopeBinding(ctx, b, []byte("durable-source"))
	if err != nil {
		t.Fatal(err)
	}
	if bound.Owner.ID == "scope" || bound.Owner.ID == "" || bound.Owner.Application != Team {
		t.Fatal("owner was inferred")
	}
	again, err := r.ImportOwnerScopeBinding(ctx, b, []byte("durable-source"))
	if err != nil || again != bound {
		t.Fatalf("witness replay: %v", err)
	}
	resolved, err := r.LookupImportedOwner(ctx, "tenant", "app", "gateway", 1, "chat", "user")
	if err != nil || resolved != bound.Owner {
		t.Fatal("imported identity unavailable")
	}
	// A later copy of the original p2p event is frozen under the witnessed owner.
	record, _, err := r.Admit(ctx, event, resolverFunc(func(context.Context, Event, *PlatformMessage) (Owner, error) { return bound.Owner, nil }))
	if err != nil || record.Owner == nil || *record.Owner != bound.Owner {
		t.Fatalf("witness routing: %+v %v", record, err)
	}
	if _, err := r.BeginBoundOwnerMigration(ctx, bound, "migration"); err == nil {
		t.Fatal("missing coverage accepted")
	}
	r.SetGatewayCoverageVerifier(coverageWitnessVerifier{})
	coverage := GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: r.RouterID(), Epoch: 1, CutoverID: "cutover"}
	if err := r.RecordGatewayCoverage(ctx, coverage, []byte("verified-coverage")); err != nil {
		t.Fatal(err)
	}
	proof, err := r.BeginBoundOwnerMigration(ctx, bound, "migration")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", proof); err != nil {
		t.Fatal(err)
	}
	bad := bound
	bad.ScopeID = "another"
	if err = r.VerifyBoundOwnerMigrationDrain(ctx, bad, "migration", proof); err == nil {
		t.Fatal("wrong scope verified")
	}
	r.SetMigrationVerifier(importVerifier{})
	imported := ImportProof{Drain: proof, SourceSnapshotHash: "snapshot", TransactionID: "committed", Target: Owner{Application: Private, ID: "target", Epoch: bound.Owner.Epoch + 1}}
	if err = r.CommitOwnerMigration(ctx, imported); err != nil {
		t.Fatal(err)
	}
	if err = r.MapPendingDelivery(ctx, event.Key, "migration"); !errors.Is(err, ErrOwnerDraining) {
		t.Fatalf("mapped before target ready: %v", err)
	}
	if _, _, err = r.CommittedMigration(ctx, bound.Owner); !errors.Is(err, ErrOwnerDraining) {
		t.Fatalf("target exposed before ready: %v", err)
	}
	r.SetTargetReadyVerifier(readyWitnessVerifier{})
	if err = r.ActivateMigrationTarget(ctx, imported, []byte("forged")); err == nil {
		t.Fatal("forged ready accepted")
	}
	if err = r.ActivateMigrationTarget(ctx, imported, []byte("verified-ready")); err != nil {
		t.Fatal(err)
	}
	if err = r.ActivateMigrationTarget(ctx, imported, []byte("verified-ready")); err != nil {
		t.Fatal(err)
	}
	if err = r.MapPendingDelivery(ctx, event.Key, "migration"); err != nil {
		t.Fatal(err)
	}
}
