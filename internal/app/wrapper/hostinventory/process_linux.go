//go:build linux

package hostinventory

import (
	"os"
	"strings"
)

func captureProcess(pid int) (string, bool, error) {
	_, start, state, err := linuxStat(pid)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(boot)) + ":" + start, state != "Z" && state != "X", nil
}
