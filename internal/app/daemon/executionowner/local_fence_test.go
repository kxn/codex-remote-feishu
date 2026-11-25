package executionowner

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLeaseAcquisitionCancellationNeverEntersCallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.db")
	a, e := Open(path, verifier{true})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := Open(path, verifier{true})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	reg := Registration{Carrier{"instance", "inc", "codex", "home", 1}, Owner{Private, "owner", 1}}
	if e = a.Register(context.Background(), reg); e != nil {
		t.Fatal(e)
	}
	hold, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.WithLease(context.Background(), reg, func() error { close(hold); <-release; return nil })
	}()
	<-hold
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- b.WithLease(ctx, reg, func() error { t.Error("cancelled acquisition ran callback"); return nil })
	}()
	select {
	case e = <-result:
		if !errors.Is(e, context.DeadlineExceeded) {
			t.Fatalf("wait error %v", e)
		}
	case <-time.After(time.Second):
		close(release)
		<-done
		t.Fatal("registry lock wait did not honor deadline")
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}

func TestLeaseCancellationDuringLocalCommitKeepsFenceAndScopedBindings(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "scoped"}[scoped], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.db")
			a, e := Open(path, verifier{true})
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			b, e := Open(path, verifier{true})
			if e != nil {
				t.Fatal(e)
			}
			defer b.Close()
			reg := Registration{Carrier{"instance", "inc", "codex", "home", 1}, Owner{Private, "owner", 1}}
			if e = a.Register(context.Background(), reg); e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			entered, release := make(chan struct{}), make(chan struct{})
			local := make(chan error, 1)
			go func() {
				if scoped {
					local <- a.WithScopedLease(ctx, reg, func(l *Lease) error {
						close(entered)
						<-release
						if err := l.ReserveSession(reg, "reserved"); err != nil {
							return err
						}
						if err := l.BindSession(reg, "bound"); err != nil {
							return err
						}
						return l.Run(reg, func() error { return nil })
					})
				} else {
					local <- a.WithLease(ctx, reg, func() error { close(entered); <-release; return nil })
				}
			}()
			<-entered
			cancel()
			transferred := make(chan error, 1)
			go func() { transferred <- b.Transfer(context.Background(), reg, Owner{Team, "next", 2}, "proof") }()
			select {
			case err := <-transferred:
				t.Fatalf("transfer passed live local transaction after caller cancel: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			if err := <-local; err != nil {
				t.Fatalf("local commit after cancellation: %v", err)
			}
			if err := <-transferred; err != nil {
				t.Fatalf("transfer after commit: %v", err)
			}
		})
	}
}
