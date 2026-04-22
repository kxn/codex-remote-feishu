package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDaemonIdleHeadlessCleanupStopsOutsideAppLockAndKeepsStoppingState(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	base := time.Now().UTC()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Minute, KillGrace: time.Second})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-2",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           2468,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.managedHeadlessRuntime.Processes["inst-headless-2"] = &managedHeadlessProcess{
		InstanceID:       "inst-headless-2",
		PID:              2468,
		WorkspaceRoot:    "/data/dl/droid",
		DisplayName:      "headless",
		Status:           managedHeadlessStatusIdle,
		IdleSince:        base,
		RefreshInFlight:  true,
		RefreshCommandID: "cmd-refresh-1",
	}

	stopStarted := make(chan struct{})
	releaseStop := make(chan struct{})
	reapDone := make(chan struct{})
	app.stopProcess = func(pid int, _ time.Duration) error {
		if pid != 2468 {
			t.Errorf("stop pid = %d, want 2468", pid)
		}
		close(stopStarted)
		<-releaseStop
		return nil
	}

	go func() {
		app.mu.Lock()
		app.reapIdleHeadless(base.Add(2 * time.Minute))
		app.mu.Unlock()
		close(reapDone)
	}()

	waitForTestSignal(t, stopStarted, "idle headless stop start")
	unlock := lockAppForTest(t, app)
	managed := app.managedHeadlessRuntime.Processes["inst-headless-2"]
	if managed == nil {
		t.Fatalf("expected managed headless to remain tracked while stop is in flight")
	}
	if managed.Status != managedHeadlessStatusStopping {
		t.Fatalf("managed status = %q, want %q", managed.Status, managedHeadlessStatusStopping)
	}
	if managed.RefreshInFlight || managed.RefreshCommandID != "" {
		t.Fatalf("expected refresh tracking to be cleared while stopping, got %#v", managed)
	}
	app.syncManagedHeadlessLocked(base.Add(2 * time.Minute))
	if managed := app.managedHeadlessRuntime.Processes["inst-headless-2"]; managed == nil || managed.Status != managedHeadlessStatusStopping {
		t.Fatalf("sync must preserve stopping state, got %#v", managed)
	}
	summary, ok := app.adminManagedInstanceSummaryLocked("inst-headless-2")
	if !ok {
		t.Fatalf("expected admin summary while stop is in flight")
	}
	if summary.Status != managedHeadlessStatusStopping || summary.Online {
		t.Fatalf("unexpected in-flight admin summary: %#v", summary)
	}
	if app.service.Instance("inst-headless-2") == nil {
		t.Fatalf("service instance should remain until stop settles")
	}
	unlock()

	close(releaseStop)
	waitForTestSignal(t, reapDone, "idle headless reap completion")

	if app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected service instance removed after idle stop settles")
	}
	if _, ok := app.managedHeadlessRuntime.Processes["inst-headless-2"]; ok {
		t.Fatalf("expected managed headless entry removed after idle stop settles")
	}
}

func TestDaemonIdleHeadlessCleanupRetriesImmediatelyAfterStopFailure(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	base := time.Now().UTC()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Minute, KillGrace: time.Second})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-2",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           2468,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.managedHeadlessRuntime.Processes["inst-headless-2"] = &managedHeadlessProcess{
		InstanceID:    "inst-headless-2",
		PID:           2468,
		WorkspaceRoot: "/data/dl/droid",
		DisplayName:   "headless",
		Status:        managedHeadlessStatusIdle,
		IdleSince:     base,
	}

	attempts := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		attempts++
		if pid != 2468 {
			t.Errorf("stop pid = %d, want 2468", pid)
		}
		if attempts == 1 {
			return errors.New("boom")
		}
		return nil
	}

	app.mu.Lock()
	app.reapIdleHeadless(base.Add(2 * time.Minute))
	managed := app.managedHeadlessRuntime.Processes["inst-headless-2"]
	if attempts != 1 {
		t.Fatalf("attempts after first reap = %d, want 1", attempts)
	}
	if managed == nil || managed.Status != managedHeadlessStatusIdle {
		t.Fatalf("expected managed headless to return to idle after stop failure, got %#v", managed)
	}
	if managed.LastError == "" {
		t.Fatalf("expected stop failure to record last error")
	}
	if !managed.IdleSince.Equal(base) {
		t.Fatalf("idleSince = %s, want %s", managed.IdleSince, base)
	}
	app.reapIdleHeadless(base.Add(2*time.Minute + 100*time.Millisecond))
	app.mu.Unlock()

	if attempts != 2 {
		t.Fatalf("attempts after retry = %d, want 2", attempts)
	}
	if app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected service instance removed after retry succeeds")
	}
	if _, ok := app.managedHeadlessRuntime.Processes["inst-headless-2"]; ok {
		t.Fatalf("expected managed headless entry removed after retry succeeds")
	}
}

func waitForTestSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func lockAppForTest(t *testing.T, app *App) func() {
	t.Helper()
	locked := make(chan struct{})
	go func() {
		app.mu.Lock()
		close(locked)
	}()
	select {
	case <-locked:
		return func() { app.mu.Unlock() }
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for App.mu")
		return nil
	}
}
