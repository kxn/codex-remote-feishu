package install

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestSystemdUserStopAndWaitWaitsUntilInactive(t *testing.T) {
	originalRunner := systemctlUserRunner
	defer func() { systemctlUserRunner = originalRunner }()

	var calls []string
	showCount := 0
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "stop" {
			return "", nil
		}
		if len(args) > 0 && args[0] == "show" {
			showCount++
			if showCount == 1 {
				return "deactivating\n123\n", nil
			}
			return "inactive\n0\n", nil
		}
		return "", nil
	}

	err := systemdUserStopAndWait(context.Background(), InstallState{}, 50*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("systemdUserStopAndWait: %v", err)
	}
	if showCount < 2 {
		t.Fatalf("expected repeated show polling, got %d calls (%#v)", showCount, calls)
	}
	if len(calls) == 0 || calls[0] != "stop codex-remote.service" {
		t.Fatalf("unexpected systemctl calls: %#v", calls)
	}
}

func TestSystemdUserStopAndWaitReturnsTimeoutWhenServiceStaysActive(t *testing.T) {
	originalRunner := systemctlUserRunner
	defer func() { systemctlUserRunner = originalRunner }()

	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "stop" {
			return "", nil
		}
		return "active\n456\n", nil
	}

	err := systemdUserStopAndWait(context.Background(), InstallState{}, 0, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "still active") {
		t.Fatalf("systemdUserStopAndWait error = %v, want still active timeout", err)
	}
}

func TestSystemdUserStopAndWaitHandlesSwappedLegacyShowValues(t *testing.T) {
	originalRunner := systemctlUserRunner
	defer func() { systemctlUserRunner = originalRunner }()

	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "stop" {
			return "", nil
		}
		// Simulate value-only output where lines arrive as MainPID then ActiveState.
		return "0\ninactive\n", nil
	}

	err := systemdUserStopAndWait(context.Background(), InstallState{}, 50*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("systemdUserStopAndWait error = %v, want nil for swapped legacy values", err)
	}
}

func TestSystemdUserStopAndWaitParsesKeyedShowOutput(t *testing.T) {
	originalRunner := systemctlUserRunner
	defer func() { systemctlUserRunner = originalRunner }()

	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "stop" {
			return "", nil
		}
		return "MainPID=0\nActiveState=inactive\n", nil
	}

	err := systemdUserStopAndWait(context.Background(), InstallState{}, 50*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("systemdUserStopAndWait error = %v, want nil for keyed output", err)
	}
}

func TestStopCurrentDaemonDoesNotClearPIDFilesWhenTerminateFails(t *testing.T) {
	originalSleep := upgradeHelperSleepFunc
	originalReadPID := upgradeHelperReadPIDFunc
	originalTerminate := upgradeHelperTerminateProcessFunc
	originalRemove := upgradeHelperRemoveFileFunc
	defer func() {
		upgradeHelperSleepFunc = originalSleep
		upgradeHelperReadPIDFunc = originalReadPID
		upgradeHelperTerminateProcessFunc = originalTerminate
		upgradeHelperRemoveFileFunc = originalRemove
	}()

	upgradeHelperSleepFunc = func(time.Duration) {}
	upgradeHelperReadPIDFunc = func(string) (int, error) { return 123, nil }
	upgradeHelperTerminateProcessFunc = func(int, time.Duration) error {
		return errors.New("process still alive")
	}
	var removed []string
	upgradeHelperRemoveFileFunc = func(path string) error {
		removed = append(removed, path)
		return nil
	}

	err := stopCurrentDaemon(context.Background(), InstallState{}, relayruntime.Paths{
		PIDFile:      "/tmp/test.pid",
		IdentityFile: "/tmp/test.identity.json",
	})
	if err == nil || !strings.Contains(err.Error(), "still alive") {
		t.Fatalf("stopCurrentDaemon error = %v, want terminate failure", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no pid/identity removal on terminate failure, got %#v", removed)
	}
}
