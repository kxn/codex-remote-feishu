package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type verifier struct{ valid bool }

func (v verifier) VerifyStop(_ context.Context, _ Registration, ref string) error {
	if !v.valid || ref != "proof" {
		return errors.New("unverified")
	}
	return nil
}
func TestCarrierSessionTransferAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	old := Registration{Carrier: Carrier{InstanceID: "i1", Incarnation: "c1", Backend: "codex", Namespace: "account", Generation: 1}, Owner: Owner{Private, "opaque", 1}}
	if err = r.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	if got, err := r.Resolve(ctx, old.Carrier, 1); err != nil || got != old.Owner {
		t.Fatalf("launch: %v %v", got, err)
	}
	other := old
	other.Owner = Owner{Team, "other", 1}
	if err = r.Register(ctx, other); !errors.Is(err, ErrConflict) {
		t.Fatalf("dual owner: %v", err)
	}
	if err = r.BindSession(ctx, old, "session"); err != nil {
		t.Fatal(err)
	}
	if known, allowed, e := r.SessionAccess(ctx, "codex", "account", "local-unmanaged", old.Owner); e != nil || known || allowed {
		t.Fatalf("unmanaged session misclassified: %v %v %v", known, allowed, e)
	}
	if known, allowed, e := r.SessionAccess(ctx, "codex", "account", "session", Owner{Team, "other", 2}); e != nil || !known || allowed {
		t.Fatalf("foreign session exposed: %v %v %v", known, allowed, e)
	}
	replacement := Registration{Carrier: Carrier{InstanceID: "i2", Incarnation: "c2", Backend: "codex", Namespace: "account", Generation: 2}, Owner: Owner{Team, "other", 2}}
	if err = r.Register(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, replacement, "session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("live overlap: %v", err)
	}
	if err = r.Transfer(ctx, old, replacement.Owner, "bad"); err == nil {
		t.Fatal("accepted unverified stop")
	}
	if err = r.Transfer(ctx, old, replacement.Owner, "proof"); err != nil {
		t.Fatal(err)
	}
	if known, allowed, e := r.SessionAccess(ctx, "codex", "account", "session", replacement.Owner); e != nil || !known || !allowed {
		t.Fatalf("explicit transfer not visible: %v %v %v", known, allowed, e)
	}
	if known, allowed, e := r.SessionAccess(ctx, "codex", "account", "session", old.Owner); e != nil || !known || allowed {
		t.Fatalf("transferred source retained access: %v %v %v", known, allowed, e)
	}
	if _, err = r.ResolveSession(ctx, old.Carrier, 1, "session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("old event: %v", err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.BindSession(ctx, replacement, "session"); err != nil {
		t.Fatal(err)
	}
	if transferred, e := r.SessionTransferred(ctx, "codex", "account", "session", replacement.Owner); e != nil || !transferred {
		t.Fatalf("transfer provenance lost after bind/restart: %v %v", transferred, e)
	}
	if transferred, e := r.SessionTransferred(ctx, "codex", "account", "session", old.Owner); e != nil || transferred {
		t.Fatalf("foreign transfer accepted: %v %v", transferred, e)
	}
	if got, err := r.ResolveSession(ctx, replacement.Carrier, 2, "session"); err != nil || got != replacement.Owner {
		t.Fatalf("resume: %v %v", got, err)
	}
	if _, err = r.Resolve(ctx, replacement.Carrier, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale epoch: %v", err)
	}
	changed := replacement.Carrier
	changed.Namespace = "wrong"
	if _, err = r.Resolve(ctx, changed, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("namespace: %v", err)
	}
}
func TestRetireAllowsSameOwnerResumeOnly(t *testing.T) {
	ctx := context.Background()
	r, err := Open(filepath.Join(t.TempDir(), "owners.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := Registration{Carrier: Carrier{"i1", "c1", "claude", "acct", 1}, Owner: Owner{Private, "owner", 1}}
	if err = r.Register(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, a, "s"); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(ctx, a, "proof"); err != nil {
		t.Fatal(err)
	}
	b := Registration{Carrier: Carrier{"i2", "c2", "claude", "acct", 2}, Owner: Owner{Team, "other", 1}}
	if err = r.Register(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, b, "s"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stolen session: %v", err)
	}
	b.Owner = a.Owner
	b.Carrier.InstanceID = "i3"
	b.Carrier.Incarnation = "c3"
	if err = r.Register(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err = r.BindSession(ctx, b, "s"); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentSessionClaimAndReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	a, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	first := Registration{Carrier: Carrier{"i1", "c1", "codex", "account", 1}, Owner: Owner{Private, "one", 1}}
	second := Registration{Carrier: Carrier{"i2", "c2", "codex", "account", 2}, Owner: Owner{Team, "two", 2}}
	if err = a.Register(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err = b.Register(ctx, second); err != nil {
		t.Fatal(err)
	}
	type claim struct {
		first bool
		err   error
	}
	results := make(chan claim, 2)
	go func() { results <- claim{true, a.BindSession(ctx, first, "s")} }()
	go func() { results <- claim{false, b.BindSession(ctx, second, "s")} }()
	c1, c2 := <-results, <-results
	e1, e2 := c1.err, c2.err
	if (e1 == nil) == (e2 == nil) {
		t.Fatalf("exactly one claimant should win: %v %v", e1, e2)
	}
	winner := second
	if (c1.first && c1.err == nil) || (c2.first && c2.err == nil) {
		winner = first
	}
	next := Owner{Team, "target", winner.Owner.Epoch + 1}
	if err = a.Transfer(ctx, winner, next, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = b.Transfer(ctx, winner, next, "proof"); err != nil {
		t.Fatalf("replay: %v", err)
	}
	wrong := next
	wrong.ID = "wrong"
	if err = b.Transfer(ctx, winner, wrong, "proof"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different target: %v", err)
	}
	if err = b.Retire(ctx, winner, "proof"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different operation: %v", err)
	}
	resume := Registration{Carrier: Carrier{"i3", "c3", "codex", "account", 3}, Owner: next}
	if err = b.Register(ctx, resume); err != nil {
		t.Fatal(err)
	}
	if err = b.BindSession(ctx, resume, "s"); err != nil {
		t.Fatalf("resume: %v", err)
	}
}
func TestRetireReplay(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	r, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	v := Registration{Carrier: Carrier{"i", "c", "codex", "ns", 1}, Owner: Owner{Private, "id", 1}}
	if err = r.Register(ctx, v); err != nil {
		t.Fatal(err)
	}
	if err = r.Retire(ctx, v, "proof"); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = Open(path, verifier{false})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.Retire(ctx, v, "proof"); err != nil {
		t.Fatalf("durable replay: %v", err)
	}
	if err = r.Retire(ctx, v, "other"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different proof: %v", err)
	}
}

func TestConcurrentRetireAndRebind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	a, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	old := Registration{Carrier: Carrier{"i1", "c1", "codex", "ns", 1}, Owner: Owner{Private, "owner", 1}}
	next := Registration{Carrier: Carrier{"i2", "c2", "codex", "ns", 2}, Owner: old.Owner}
	if err = a.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err = b.Register(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err = a.BindSession(ctx, old, "session"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- a.Retire(ctx, old, "proof") }()
	go func() {
		err := b.BindSession(ctx, next, "session")
		if err != nil && !errors.Is(err, ErrConflict) {
			results <- err
			return
		}
		results <- nil
	}()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err = b.BindSession(ctx, next, "session"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResolveSession(ctx, old.Carrier, 1, "session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("old carrier: %v", err)
	}
}

func TestLeaseSerializesLocalCommitAndTransferAcrossConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	a, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	old := Registration{Carrier: Carrier{"instance", "incarnation", "codex", "namespace", 1}, Owner: Owner{Private, "owner", 1}}
	if err = a.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	operation := make(chan error, 1)
	transferred := make(chan error, 1)
	go func() { operation <- a.WithLease(ctx, old, func() error { close(entered); <-release; return nil }) }()
	<-entered
	go func() { transferred <- b.Transfer(ctx, old, Owner{Team, "new", 2}, "proof") }()
	select {
	case err := <-transferred:
		t.Fatalf("transfer crossed active lease: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err = <-operation; err != nil {
		t.Fatal(err)
	}
	if err = <-transferred; err != nil {
		t.Fatal(err)
	}
	wrote := false
	if err = a.WithLease(ctx, old, func() error { wrote = true; return nil }); !errors.Is(err, ErrConflict) || wrote {
		t.Fatalf("old lease after transfer: %v, wrote=%v", err, wrote)
	}
}

type singleUseVerifier struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (v *singleUseVerifier) VerifyStop(_ context.Context, _ Registration, _ string) error {
	if v.calls.Add(1) != 1 {
		return errors.New("proof no longer valid")
	}
	close(v.entered)
	<-v.release
	return nil
}
func TestConcurrentTransferReplayDoesNotReverify(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owners.db")
	proof := &singleUseVerifier{entered: make(chan struct{}), release: make(chan struct{})}
	first, err := Open(path, proof)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path, proof)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	old := Registration{Carrier: Carrier{"instance", "carrier", "codex", "namespace", 1}, Owner: Owner{Private, "owner", 1}}
	next := Owner{Team, "next", 2}
	if err = first.Register(ctx, old); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- first.Transfer(ctx, old, next, "proof") }()
	<-proof.entered
	started := make(chan struct{})
	go func() { close(started); results <- second.Transfer(ctx, old, next, "proof") }()
	<-started
	close(proof.release)
	for i := 0; i < 2; i++ {
		if err = <-results; err != nil {
			t.Fatalf("identical transfer failed: %v", err)
		}
	}
	if calls := proof.calls.Load(); calls != 1 {
		t.Fatalf("verifier called %d times", calls)
	}
	different := next
	different.ID = "other"
	if err = second.Transfer(ctx, old, different, "proof"); !errors.Is(err, ErrConflict) {
		t.Fatalf("different target: %v", err)
	}
	if calls := proof.calls.Load(); calls != 1 {
		t.Fatalf("conflicting transfer called verifier: %d", calls)
	}
}

func TestResumeReservationPreventsConcurrentOwnerBeforeNativeBinding(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "owners.db")
	a, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	one := Registration{Carrier: Carrier{InstanceID: "one", Incarnation: "inc-one", Backend: "codex", Namespace: "home", Generation: 1}, Owner: Owner{Private, "first", 1}}
	two := Registration{Carrier: Carrier{InstanceID: "two", Incarnation: "inc-two", Backend: "codex", Namespace: "home", Generation: 1}, Owner: Owner{Team, "second", 2}}
	if err = a.Register(ctx, one); err != nil {
		t.Fatal(err)
	}
	if err = b.Register(ctx, two); err != nil {
		t.Fatal(err)
	}
	if err = a.ReserveSession(ctx, one, "session"); err != nil {
		t.Fatal(err)
	}
	if err = b.ReserveSession(ctx, two, "session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign reservation: %v", err)
	}
	if err = b.BindSession(ctx, two, "session"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign native bind bypassed reservation: %v", err)
	}
	if err = a.BindSession(ctx, one, "session"); err != nil {
		t.Fatal(err)
	}
	if err = a.Transfer(ctx, one, two.Owner, "proof"); err != nil {
		t.Fatal(err)
	}
	if err = b.ReserveSession(ctx, two, "session"); err != nil {
		t.Fatal(err)
	}
	if err = b.BindSession(ctx, two, "session"); err != nil {
		t.Fatal(err)
	}
}
