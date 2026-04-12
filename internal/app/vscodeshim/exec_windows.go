//go:build windows

package vscodeshim

import (
	"os"
	"os/exec"
)

func execBinary(binaryPath string, args []string, env []string) error {
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}
