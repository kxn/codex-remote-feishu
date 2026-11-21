//go:build !linux && !darwin && !windows

package hostinventory

import "fmt"

func BootIdentity() (string, error) { return "", fmt.Errorf("host boot identity unavailable") }
