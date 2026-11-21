//go:build darwin

package hostinventory

import (
	"errors"
	"golang.org/x/sys/unix"
	"syscall"
)

func captureProcess(pid int) (string, bool, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if errors.Is(err, syscall.ESRCH) || (err == nil && (info == nil || info.Proc.P_pid == 0)) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return darwinIdentity(info), info.Proc.P_stat != 5, nil
}
