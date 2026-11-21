//go:build !linux && !darwin && !windows

package hostinventory

import "fmt"

func captureProcess(pid int) (string, bool, error) {
	return "", false, fmt.Errorf("host process identity unavailable")
}
