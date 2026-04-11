//go:build !windows

package externalaccess

import "os/exec"

func configureTryCloudflareLaunch(cmd *exec.Cmd) {
	_ = cmd
}
