package daemon

import (
	"errors"
	"testing"
	"time"

	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestCronForcedStopRunsOutsideAppLockAndTracksInFlightState(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	now := time.Now().UTC()
	app.cronRuntime.exitTargets["inst-cron-1"] = &cronrt.ExitTarget{
		InstanceID: "inst-cron-1",
		PID:        4321,
		Deadline:   now.Add(-time.Second),
	}

	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	reapDone := make(chan struct{})
	app.stopProcess = func(pid int, _ time.Duration) error {
		if pid != 4321 {
			t.Errorf("stop pid = %d, want 4321", pid)
		}
		close(stopStarted)
		<-releaseStop
		return nil
	}

	go func() {
		app.mu.Lock()
		app.reapCronExitTargetsLocked(now)
		app.mu.Unlock()
		close(reapDone)
	}()

	waitForTestSignal(t, stopStarted, "cron forced stop start")
	unlock := lockAppForTest(t, app)
	target := app.cronRuntime.exitTargets["inst-cron-1"]
	if target == nil {
		t.Fatalf("expected cron exit target to remain while forced stop is in flight")
	}
	if !target.StopInFlight {
		t.Fatalf("expected in-flight forced-stop marker, got %#v", target)
	}
	if target.LastStopAttemptAt.IsZero() {
		t.Fatalf("expected forced-stop attempt timestamp, got %#v", target)
	}
	unlock()

	close(releaseStop)
	waitForTestSignal(t, reapDone, "cron forced stop completion")

	if _, ok := app.cronRuntime.exitTargets["inst-cron-1"]; ok {
		t.Fatalf("expected cron exit target removed after stop settles")
	}
}

func TestCronForcedStopFailureClearsInFlightAndRetries(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	now := time.Now().UTC()
	app.cronRuntime.exitTargets["inst-cron-1"] = &cronrt.ExitTarget{
		InstanceID: "inst-cron-1",
		PID:        4321,
		Deadline:   now.Add(-time.Second),
	}

	attempts := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		attempts++
		if pid != 4321 {
			t.Errorf("stop pid = %d, want 4321", pid)
		}
		if attempts == 1 {
			return errors.New("boom")
		}
		return nil
	}

	app.mu.Lock()
	app.reapCronExitTargetsLocked(now)
	target := app.cronRuntime.exitTargets["inst-cron-1"]
	if attempts != 1 {
		t.Fatalf("attempts after first reap = %d, want 1", attempts)
	}
	if target == nil || target.StopInFlight {
		t.Fatalf("expected failed forced stop to stay queued but not in flight, got %#v", target)
	}
	if target.LastStopAttemptAt.IsZero() {
		t.Fatalf("expected failed forced stop to keep last attempt time, got %#v", target)
	}
	app.reapCronExitTargetsLocked(now.Add(100 * time.Millisecond))
	app.mu.Unlock()

	if attempts != 2 {
		t.Fatalf("attempts after retry = %d, want 2", attempts)
	}
	if _, ok := app.cronRuntime.exitTargets["inst-cron-1"]; ok {
		t.Fatalf("expected cron exit target removed after retry succeeds")
	}
}
