//go:build !linux && !darwin && !windows

package hostinventory

import (
	"fmt"
	"os/exec"
)

func preparePlatform(*exec.Cmd) {}
func bindPlatform(*exec.Cmd) (*Scope, error) {
	return nil, fmt.Errorf("host native inventory unsupported")
}

func restorePlatform(Snapshot) (*Scope, error) {
	return nil, fmt.Errorf("host native inventory unsupported")
}
