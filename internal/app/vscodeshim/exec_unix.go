//go:build !windows

package vscodeshim

import (
	"path/filepath"
	"syscall"
)

func execBinary(binaryPath string, args []string, env []string) error {
	argv := make([]string, 0, len(args)+1)
	argv = append(argv, filepath.Base(binaryPath))
	argv = append(argv, args...)
	return syscall.Exec(binaryPath, argv, env)
}
