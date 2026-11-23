package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func outboundFixture(t *testing.T) (*Registry, *Outbound, Registration) {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "owners.db"), verifier{true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	reg := Registration{Carrier{"instance", "incarnation", "codex", "account", 1}, Owner{Private, "opaque", 1}}
	if err = r.Register(context.Background(), reg); err != nil {
		t.Fatal(err)
	}
	b, err := NewOutbound(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, b, reg
}

func TestOutboundDrainClosesLeaseSendGap(t *testing.T) {
	r, b, reg := outboundFixture(t)
	ctx := context.Background()
	entered, release := make(chan struct{}), make(chan struct{})
	sent := make(chan error, 1)
	go func() {
		sent <- b.Send(ctx, reg, func() error { return nil }, func() error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	// No SQLite transaction spans the stalled socket write; inbound evidence
	// and another carrier must remain responsive.
	if err := r.WithLease(ctx, reg, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	other := Registration{Carrier{"other", "another", "claude", "other-account", 1}, Owner{Team, "member", 1}}
	if err := r.Register(ctx, other); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, other, func() error { return nil }, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	timeout, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := b.Transfer(timeout, reg, Owner{Team, "successor", 2}, "proof"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("transfer must wait for socket: %v", err)
	}
	if got, err := r.Resolve(ctx, reg.Carrier, 1); err != nil || got != reg.Owner {
		t.Fatalf("owner changed before sender drained: %v %v", got, err)
	}
	if err := b.Send(ctx, reg, func() error { t.Error("prepared after drain"); return nil }, func() error { t.Error("sent after drain"); return nil }); !errors.Is(err, ErrDraining) {
		t.Fatalf("timeout reopened carrier: %v", err)
	}
	close(release)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err := b.Transfer(ctx, reg, Owner{Team, "successor", 2}, "proof"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve(ctx, reg.Carrier, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("old lease survived: %v", err)
	}
	if err := b.Transfer(ctx, reg, Owner{Team, "successor", 2}, "proof"); err != nil {
		t.Fatalf("transfer replay: %v", err)
	}
}

func TestOutboundPreservesUnknownTransportResult(t *testing.T) {
	_, b, reg := outboundFixture(t)
	ctx := context.Background()
	transportFailure := errors.New("socket closed after possible write")
	err := b.Send(ctx, reg, func() error { return nil }, func() error { return transportFailure })
	var notSent *BeforeSendError
	if err != transportFailure || errors.As(err, &notSent) {
		t.Fatalf("uncertainty turned into safe retry: %v", err)
	}
	if err := b.Retire(ctx, reg, "invalid-proof"); err == nil {
		t.Fatal("drain incorrectly proved execution stopped")
	}
	err = b.Send(ctx, reg, func() error { return nil }, func() error { t.Error("sent after failed retirement"); return nil })
	if !errors.Is(err, ErrDraining) || !errors.As(err, &notSent) {
		t.Fatalf("failed verification reopened gate: %v", err)
	}
}

func TestOutboundPreparationFailureAndCanceledSend(t *testing.T) {
	_, b, reg := outboundFixture(t)
	ctx := context.Background()
	failed := errors.New("could not commit command")
	err := b.Send(ctx, reg, func() error { return failed }, func() error { t.Error("sent uncommitted command"); return nil })
	var notSent *BeforeSendError
	if !errors.Is(err, failed) || !errors.As(err, &notSent) {
		t.Fatalf("prepare result: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	err = b.Send(canceled, reg, func() error { cancel(); return nil }, func() error { t.Error("sent after cancel"); return nil })
	if !errors.Is(err, context.Canceled) || !errors.As(err, &notSent) {
		t.Fatalf("cancel result: %v", err)
	}
	if err := b.Drain(ctx, reg); err != nil {
		t.Fatalf("failed sends leaked an in-flight handle: %v", err)
	}
	changed := reg
	changed.Owner.Epoch++
	if err := b.Drain(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("rebound same incarnation: %v", err)
	}
}

func TestOutboundWrongFirstOwnerCannotPoisonCarrier(t *testing.T) {
	_, b, reg := outboundFixture(t)
	ctx := context.Background()
	wrong := reg
	wrong.Owner.ID = "another-private-chat"
	if err := b.Drain(ctx, wrong); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign drain: %v", err)
	}
	if err := b.Send(ctx, wrong, func() error { t.Error("prepared foreign command"); return nil }, func() error { t.Error("sent foreign command"); return nil }); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign send: %v", err)
	}
	if err := b.Send(ctx, reg, func() error { return nil }, func() error { return nil }); err != nil {
		t.Fatalf("foreign call poisoned valid owner: %v", err)
	}
}
