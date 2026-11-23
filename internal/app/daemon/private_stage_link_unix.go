//go:build !windows

package daemon

import (
	"golang.org/x/sys/unix"
	"os"
)

func privateStageLink(dir *os.File, _ string, oldName, newName string) error {
	return unix.Linkat(int(dir.Fd()), oldName, int(dir.Fd()), newName, 0)
}
