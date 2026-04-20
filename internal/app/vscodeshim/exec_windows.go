//go:build windows

package vscodeshim

import (
	"os"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func execBinary(binaryPath string, args []string, env []string) error {
	cmd := execlaunch.Command(binaryPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}
