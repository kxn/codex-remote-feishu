package ingressowner

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestPrivateViewIssueIsDurableAndCannotChangeRender(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "ingress.db")
	r, err := Open(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{Application: Private, ID: "owner", Epoch: 1}
	first, err := r.IssuePrivateView(ctx, owner, "effect", 0, []byte(`{"card":"original"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	again, err := r.IssuePrivateView(ctx, owner, "effect", 0, []byte(`{"card":"original"}`))
	if err != nil || again != first {
		t.Fatalf("issue not stable: %v", err)
	}
	if _, err = r.IssuePrivateView(ctx, owner, "effect", 0, []byte(`{"card":"changed"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("render changed: %v", err)
	}
	other, err := r.IssuePrivateView(ctx, owner, "effect", 1, []byte(`{"card":"original"}`))
	if err != nil || other.Token == first.Token {
		t.Fatal("distinct operation reused token")
	}
}
