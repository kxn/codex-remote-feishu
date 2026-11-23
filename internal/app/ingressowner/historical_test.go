package ingressowner

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

type historyWitness struct{}

func (historyWitness) VerifyHistoricalSource(_ context.Context, _ OwnerScopeBinding, _ Event, proof []byte) error {
	if string(proof) != "sealed-source" {
		return errors.New("unverified history")
	}
	return nil
}
func (historyWitness) VerifyHistoricalPrivateStaging(_ context.Context, _ Record, _ Owner, _ json.RawMessage, proof []byte) error {
	if string(proof) != "verified-assets" {
		return errors.New("unverified assets")
	}
	return nil
}
func TestHistoricalSourceHasNoInventedReceiptAndReplayIsNotDelivered(t *testing.T) {
	ctx := t.Context()
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	source := testEvent("source", Message)
	source.ChatType = "p2p"
	source.ChatID = "chat"
	source.MessageID = "message"
	hash, _ := EventFactHash(source)
	r.SetOwnerScopeBindingVerifier(scopeWitnessVerifier{source})
	r.SetGatewayCoverageVerifier(coverageWitnessVerifier{})
	r.SetHistoricalSourceVerifier(historyWitness{})
	r.SetHistoricalStagingVerifier(historyWitness{})
	b := OwnerScopeBinding{SourceEventKey: source.Key, SourceFactHash: hash, ScopeID: "scope", PrincipalID: "principal", RoleID: "role", GatewayID: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", OpenID: "user"}
	bound, err := r.ImportOwnerScopeBinding(ctx, b, []byte("durable-source"))
	if err != nil {
		t.Fatal(err)
	}
	coverage := GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: r.RouterID(), Epoch: 1, CutoverID: "cutover"}
	if err = r.RecordGatewayCoverage(ctx, coverage, []byte("verified-coverage")); err != nil {
		t.Fatal(err)
	}
	if _, err = r.BeginBoundOwnerMigration(ctx, bound, "migration"); err != nil {
		t.Fatal(err)
	}
	historical := source
	historical.Key.EventID = "old-event"
	historical.MessageID = "old-message"
	record, err := r.ImportHistoricalSourceRecord(ctx, bound, historical, []byte("sealed-source"))
	if err != nil {
		t.Fatal(err)
	}
	if record.State != Historical || record.Sequence >= 0 || record.Receipt != nil {
		t.Fatal("invented admission")
	}
	target := Owner{Application: Private, ID: "private", Epoch: 2}
	if err = r.ImportStagedPrivateAction(ctx, historical.Key, target, json.RawMessage(`{"kind":"surface.message.image"}`), []byte("verified-assets")); err != nil {
		t.Fatal(err)
	}
	record.DeliveryTarget = &target
	if _, err = r.StagedPrivateAction(ctx, record, target); err != nil {
		t.Fatal(err)
	}
	again, duplicate, err := r.Admit(ctx, historical, nil)
	if err != nil || !duplicate || again.State != Historical {
		t.Fatal("historical event replayed as live")
	}
	changed := historical
	changed.Payload = json.RawMessage(`{"changed":true}`)
	if _, _, err = r.Admit(ctx, changed, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("changed historical facts accepted")
	}
}
