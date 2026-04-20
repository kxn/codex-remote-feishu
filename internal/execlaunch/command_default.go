//go:build !windows

package execlaunch

import "os/exec"

func preparePlatform(_ *exec.Cmd) {}
