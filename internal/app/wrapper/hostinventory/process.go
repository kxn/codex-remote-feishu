package hostinventory

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

type ProcessIdentity struct {
	Platform      string `json:"platform"`
	PID           int    `json:"pid"`
	StartIdentity string `json:"start_identity"`
}

func CaptureProcess(pid int) (ProcessIdentity, error) {
	if pid <= 0 {
		return ProcessIdentity{}, fmt.Errorf("positive process identity required")
	}
	id, alive, err := captureProcess(pid)
	if err != nil {
		return ProcessIdentity{}, err
	}
	if !alive {
		return ProcessIdentity{}, fmt.Errorf("process has already exited")
	}
	return ProcessIdentity{Platform: runtime.GOOS, PID: pid, StartIdentity: id}, nil
}

// This proves only the precise carrier process exited. It does not prove any
// descendant or remote resource stopped; native quiescence is a prerequisite.
func WaitProcessExit(ctx context.Context, identity ProcessIdentity) error {
	if identity.Platform != runtime.GOOS || identity.PID <= 0 || identity.StartIdentity == "" {
		return fmt.Errorf("host process identity mismatch")
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		current, alive, err := captureProcess(identity.PID)
		if err != nil {
			return err
		}
		if !alive || current != identity.StartIdentity {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
