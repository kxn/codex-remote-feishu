//go:build !windows

package relayruntime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

func terminateProcess(pid int, grace time.Duration) error {
	if pid <= 0 {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if reaped, err := reapExitedChild(pid); err != nil {
		return err
	} else if reaped {
		return nil
	}
	if processAlive(pid) {
		if err := process.Signal(syscall.SIGTERM); err != nil && !processSignalDone(err) {
			return err
		}
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) {
			if reaped, err := reapExitedChild(pid); err != nil {
				return err
			} else if reaped {
				return nil
			}
			if !processAlive(pid) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	if reaped, err := reapExitedChild(pid); err != nil {
		return err
	} else if reaped {
		return nil
	}
	if !processAlive(pid) {
		return nil
	}
	if err := process.Signal(syscall.SIGKILL); err != nil && !processSignalDone(err) {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reaped, err := reapExitedChild(pid); err != nil {
			return err
		} else if reaped {
			return nil
		}
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !processAlive(pid) {
		return nil
	}
	return fmt.Errorf("process %d still alive after SIGKILL timeout", pid)
}

func reapExitedChild(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	var status syscall.WaitStatus
	waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if errors.Is(err, syscall.ECHILD) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return waited == pid, nil
}

func processSignalDone(err error) bool {
	return err == nil || errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

func prepareDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
