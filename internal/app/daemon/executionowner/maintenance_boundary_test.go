package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceFencesNewSendsAndWaitsInflightWithoutBlockingStop(t *testing.T) {
	ctx := context.Background()
	r, e := Open(filepath.Join(t.TempDir(), "owners.db"), verifier{true})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	reg := Registration{Carrier: Carrier{InstanceID: "i", Incarnation: "inc", Backend: "codex", Namespace: "ns", Generation: 1}, Owner: Owner{Private, "owner", 1}}
	if e = r.Register(ctx, reg); e != nil {
		t.Fatal(e)
	}
	out, e := NewOutbound(r)
	if e != nil {
		t.Fatal(e)
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- out.SendLeased(ctx, reg, func(l *Lease) error { return l.CheckBusiness(reg) }, func() error { close(entered); <-release; return nil })
	}()
	<-entered
	if _, e = r.BeginMaintenance(ctx, "codex", "maint", "2"); e != nil {
		t.Fatal(e)
	}
	other := reg
	other.Carrier.InstanceID = "i2"
	other.Carrier.Incarnation = "inc2"
	if e = r.Register(ctx, other); !errors.Is(e, ErrMaintenanceActive) {
		t.Fatalf("register during maintenance: %v", e)
	}
	if e = r.ReserveSession(ctx, reg, "session"); !errors.Is(e, ErrMaintenanceActive) {
		t.Fatalf("reserve during maintenance: %v", e)
	}
	sent := false
	e = out.SendLeased(ctx, reg, func(l *Lease) error { return l.CheckBusiness(reg) }, func() error { sent = true; return nil })
	if !errors.Is(e, ErrMaintenanceActive) || sent {
		t.Fatalf("business sent: %v %v", sent, e)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if e = out.WaitIdle(waitCtx, reg); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("missed in-flight socket: %v", e)
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if e = out.WaitIdle(ctx, reg); e != nil {
		t.Fatal(e)
	}
	if e = out.SendLeased(ctx, reg, func(*Lease) error { return nil }, func() error { sent = true; return nil }); e != nil || !sent {
		t.Fatalf("stop blocked: %v", e)
	}
}
