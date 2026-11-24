package ingressowner

import (
	"path/filepath"
	"testing"
)

func TestOwnerScopeBindingForVerifiesIdentityAndProof(t *testing.T) {
	ctx := t.Context()
	r := openTest(t, filepath.Join(t.TempDir(), "router.db"))
	defer r.Close()
	event := testEvent("source", Message)
	event.ChatType, event.ChatID, event.MessageID = "p2p", "chat", "message"
	hash, err := EventFactHash(event)
	if err != nil {
		t.Fatal(err)
	}
	r.SetOwnerScopeBindingVerifier(scopeWitnessVerifier{event})
	b := OwnerScopeBinding{SourceEventKey: event.Key, SourceFactHash: hash, ScopeID: "scope", PrincipalID: "principal", RoleID: "role", GatewayID: "gateway", AppID: "app", BotGeneration: 1, Tenant: "tenant", ChatID: "chat", OpenID: "user"}
	bound, err := r.ImportOwnerScopeBinding(ctx, b, []byte("durable-source"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.OwnerScopeBindingFor(ctx, bound.Owner)
	if err != nil || got != bound {
		t.Fatalf("got %+v: %v", got, err)
	}
	other := bound.Owner
	other.Epoch++
	if _, err = r.OwnerScopeBindingFor(ctx, other); err == nil {
		t.Fatal("other epoch accepted")
	}
	if _, err = r.db.ExecContext(ctx, `UPDATE owner_scope_bindings SET proof=? WHERE owner=?`, []byte("forged"), ownerJSON(bound.Owner)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.OwnerScopeBindingFor(ctx, bound.Owner); err == nil {
		t.Fatal("forged source proof accepted")
	}
}
