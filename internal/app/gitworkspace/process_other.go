//go:build !windows

package gitworkspace

import "os/exec"

func configureGitImportCommand(_ *exec.Cmd) {}
