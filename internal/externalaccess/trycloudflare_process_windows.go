//go:build windows

package externalaccess

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func configureTryCloudflareLaunch(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}
