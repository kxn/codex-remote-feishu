package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestScopedSessionBindingExpiresAndFencesOwner(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "owners.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	reg := Registration{Carrier: Carrier{InstanceID: "instance", Incarnation: "inc", Backend: "codex", Namespace: "history", Generation: 1}, Owner: Owner{Application: Private, ID: "owner", Epoch: 1}}
	if err = r.Register(ctx, reg); err != nil {
		t.Fatal(err)
	}
	var lease *Lease
	if err = r.WithScopedLease(ctx, reg, func(l *Lease) error {
		lease = l
		if err := l.BindSession(reg, "session"); err != nil {
			return err
		}
		wrong := reg
		wrong.Owner.ID = "foreign"
		if !errors.Is(l.BindSession(wrong, "other"), ErrConflict) {
			t.Fatal("foreign lease accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(lease.BindSession(reg, "other"), ErrConflict) {
		t.Fatal("expired lease accepted")
	}
	if _, err = r.ResolveSession(ctx, reg.Carrier, 1, "session"); err != nil {
		t.Fatal(err)
	}
}
