package executionowner

import (
	"context"
	"path/filepath"
	"testing"
)

func TestUnretiredPrivateCarrierWithoutSessionDoesNotBlockFreshCarrier(t *testing.T) {
	ctx := context.Background()
	r, e := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	old := Registration{Carrier: Carrier{InstanceID: "old", Incarnation: "old-carrier", Backend: "codex", Namespace: "same-account", Generation: 1}, Owner: Owner{Private, "owner", 1}}
	next := Registration{Carrier: Carrier{InstanceID: "new", Incarnation: "new-carrier", Backend: "codex", Namespace: "same-account", Generation: 1}, Owner: old.Owner}
	if e = r.Register(ctx, old); e != nil {
		t.Fatal(e)
	}
	if e = r.Register(ctx, next); e != nil {
		t.Fatalf("fresh carrier blocked by old without session: %v", e)
	}
	if e = r.BindSession(ctx, next, "new-session"); e != nil {
		t.Fatal(e)
	}
	if got, e := r.Resolve(ctx, old.Carrier, old.Owner.Epoch); e != nil || got != old.Owner {
		t.Fatal("old unresolved carrier was falsely retired")
	}
}
