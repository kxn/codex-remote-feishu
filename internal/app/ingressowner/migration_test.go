package ingressowner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

type importVerifier struct{}

func (importVerifier) VerifyImport(_ context.Context, p ImportProof) error {
	if p.TransactionID != "committed" || p.SourceSnapshotHash != "snapshot" {
		return errors.New("unverified import")
	}
	return nil
}

type targetAdmitter struct{ expected Owner }

func (a targetAdmitter) Admit(_ context.Context, r Record) (AdmissionReceipt, error) {
	if r.DeliveryTarget == nil || *r.DeliveryTarget != a.expected {
		return AdmissionReceipt{}, ErrConflict
	}
	hash, err := AdmissionHash(r)
	return AdmissionReceipt{Key: r.Event.Key, Owner: a.expected, AdmissionID: "imported", PayloadHash: hash}, err
}
func TestMigrationDrainsInFlightAndMapsOnlyPending(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	defer r.Close()
	source := teamOwner
	target := Owner{Application: Team, ID: "target", Epoch: 2}
	r.SetMigrationVerifier(importVerifier{})
	event := testEvent("first", Message)
	event.ChatType = "group"
	event.ChatID = "chat"
	event.MessageID = "message"
	if _, _, err := r.Admit(ctx, event, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := r.Deliver(ctx, event.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { close(started); <-release; return nil, nil }))
		done <- err
	}()
	<-started
	proofCh := make(chan DrainProof, 1)
	errCh := make(chan error, 1)
	go func() { p, err := r.BeginOwnerMigration(ctx, source, "migration"); proofCh <- p; errCh <- err }()
	// Wait for the durable fence, then ensure a new owner event remains pending.
	deadline := time.Now().Add(time.Second)
	for {
		var state string
		e := r.db.QueryRow(`SELECT state FROM owner_migrations WHERE owner=?`, ownerJSON(source)).Scan(&state)
		if e == nil && state == "draining" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("drain was not persisted")
		}
		time.Sleep(time.Millisecond)
	}
	second := testEvent("second", Message)
	second.ChatType = "group"
	second.ChatID = "chat"
	second.MessageID = "message2"
	if _, _, err := r.Admit(ctx, second, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-proofCh:
		t.Fatal("drain did not wait for source admission")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	proof := <-proofCh
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if proof.AcceptedWatermark == 0 {
		t.Fatal("missing source watermark")
	}
	if err := r.VerifyOwnerMigrationDrain(ctx, source, "migration", proof); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Deliver(ctx, second.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) {
		t.Fatal("delivered while draining")
		return nil, nil
	})); !errors.Is(err, ErrOwnerDraining) {
		t.Fatalf("gate: %v", err)
	}
	imp := ImportProof{Drain: proof, SourceSnapshotHash: "snapshot", Target: target, TransactionID: "committed"}
	if err := r.CommitOwnerMigration(ctx, imp); err != nil {
		t.Fatal(err)
	}
	if err := r.CommitOwnerMigration(ctx, imp); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := r.VerifyOwnerMigrationDrain(ctx, source, "migration", proof); err == nil {
		t.Fatal("old proof still valid")
	}
	if err := r.MapPendingDelivery(ctx, second.Key, "migration"); err != nil {
		t.Fatal(err)
	}
	result, err := r.Deliver(ctx, second.Key, targetAdmitter{target})
	if err != nil {
		t.Fatal(err)
	}
	if result.Owner == nil || *result.Owner != source || result.DeliveryTarget == nil || *result.DeliveryTarget != target {
		t.Fatal("original owner was rewritten")
	}
	if err := r.MapPendingDelivery(ctx, event.Key, "migration"); err == nil {
		t.Fatal("accepted event remapped")
	}
}
func TestMigrationUnknownAdmissionRequiresSourceReconcileAcrossRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "router.db")
	r := openTest(t, path)
	event := testEvent("event", Message)
	event.ChatType = "group"
	event.ChatID = "chat"
	event.MessageID = "message"
	if _, _, err := r.Admit(ctx, event, resolverFunc(classify)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Deliver(ctx, event.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { return nil, errors.New("lost response") })); err == nil {
		t.Fatal("expected unknown")
	}
	if _, err := r.BeginOwnerMigration(ctx, teamOwner, "migration"); !errors.Is(err, ErrAdmissionUncertain) {
		t.Fatalf("unknown ignored: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path)
	defer r.Close()
	if _, err := r.BeginOwnerMigration(ctx, teamOwner, "migration"); !errors.Is(err, ErrAdmissionUncertain) {
		t.Fatalf("unknown lost on restart: %v", err)
	}
	if _, err := r.ReconcileBeforeDrain(ctx, event.Key, admitFunc(func(context.Context, Record) (json.RawMessage, error) { return nil, nil })); err != nil {
		t.Fatal(err)
	}
	proof, err := r.BeginOwnerMigration(ctx, teamOwner, "migration")
	if err != nil || proof.AcceptedWatermark == 0 {
		t.Fatalf("reconciled drain: %+v %v", proof, err)
	}
}
