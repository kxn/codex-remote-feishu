package install

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLaunchdStopWaitsUntilServiceIsUnloaded(t *testing.T) {
	defer withDarwinGOOS(t)()
	prints := 0
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		switch args[0] {
		case "bootout":
			return "", nil
		case "print":
			prints++
			switch prints {
			case 1:
				return "state = SIGTERM sent\npid = 55525\n", nil
			case 2:
				return "state = exited\n", nil
			default:
				return "", fmt.Errorf("Could not find service")
			}
		default:
			t.Fatalf("unexpected service operation: %v", args)
			return "", nil
		}
	})()

	if err := launchdUserStopAndWait(context.Background(), InstallState{}, time.Second, time.Microsecond); err != nil {
		t.Fatalf("launchdUserStopAndWait: %v", err)
	}
	if prints != 3 {
		t.Fatalf("must wait for launchd to remove the service, observed %d status checks, want 3", prints)
	}
}

func TestLaunchdStopTimesOutWhileServiceIsStillRegistered(t *testing.T) {
	defer withDarwinGOOS(t)()
	defer withMockLaunchctl(t, func(_ context.Context, args ...string) (string, error) {
		switch args[0] {
		case "bootout":
			return "", nil
		case "print":
			return "state = SIGTERM sent\npid = 55525\n", nil
		default:
			t.Fatalf("unexpected service operation: %v", args)
			return "", nil
		}
	})()

	err := launchdUserStopAndWait(context.Background(), InstallState{}, 0, time.Microsecond)
	if err == nil || !strings.Contains(err.Error(), "still registered") {
		t.Fatalf("launchdUserStopAndWait error = %v, want still registered timeout", err)
	}
}
