package daemon

import (
	"errors"
	"path/filepath"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestRuntimeLockRetainedWhilePriorApplicationCanStillWrite(t *testing.T) {
	for _, active := range []string{"sdk", "native", "private-worker", "team-worker"} {
		t.Run(active, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "daemon.lock")
			lock, err := relayruntime.AcquireLock(t.Context(), path, false)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Release()
			a := &App{applications: &applicationRuntime{}, teamRuntime: &teamRuntime{}}
			var done func()
			switch active {
			case "sdk":
				done, _ = a.applications.platformDeliveries.begin()
			case "native":
				done, _ = a.beginApplicationNativeDelivery()
			case "private-worker":
				a.applications.done = make(chan struct{})
				done = func() { close(a.applications.done) }
			case "team-worker":
				a.teamRuntime.runDone = make(chan struct{})
				done = func() { close(a.teamRuntime.runDone) }
			}
			if err = releaseApplicationRuntimeLock(lock, a); err == nil {
				t.Fatal("live writer lock released")
			}
			if err = lock.VerifyHeld(); err != nil {
				t.Fatal(err)
			}
			if replacement, err := relayruntime.AcquireLock(t.Context(), path, false); !errors.Is(err, relayruntime.ErrLockHeld) {
				if replacement != nil {
					_ = replacement.Release()
				}
				t.Fatalf("second daemon could claim exclusive coverage: %v", err)
			}
			done()
			if err = releaseApplicationRuntimeLock(lock, a); err != nil {
				t.Fatal(err)
			}
			replacement, err := relayruntime.AcquireLock(t.Context(), path, false)
			if err != nil {
				t.Fatal(err)
			}
			defer replacement.Release()
		})
	}
}
