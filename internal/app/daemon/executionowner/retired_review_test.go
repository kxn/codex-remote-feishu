package executionowner

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRetiredReviewSessionIsInactiveAndRejectsForeignOwner(t *testing.T) {
	ctx := t.Context()
	r, err := Open(filepath.Join(t.TempDir(), "registry.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := Registration{Carrier: Carrier{InstanceID: "i", Incarnation: "old", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "one", Epoch: 1}}
	if err = r.Register(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(ctx, a, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = r.BindRetiredReviewSession(ctx, a, "proof", "child"); err != nil {
		t.Fatal(err)
	}
	if err = r.BindRetiredReviewSession(ctx, a, "proof", "child"); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ResolveSession(ctx, a.Carrier, a.Owner.Epoch, "child"); !errors.Is(err, ErrConflict) {
		t.Fatalf("retired carrier acquired active session: %v", err)
	}
	if err = r.WithRetiredReviewSession(ctx, a, "proof", "child", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	b := Registration{Carrier: Carrier{InstanceID: "j", Incarnation: "new", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Application: Private, ID: "other", Epoch: 1}}
	if err = r.Register(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, b, "child"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign carrier stole child: %v", err)
	}
	if err = r.WithRetiredReviewSession(ctx, a, "wrong", "child", func() error { return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong evidence: %v", err)
	}
}
