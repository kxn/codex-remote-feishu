//go:build linux

package hostinventory

import (
	"os"
	"strings"
)

func BootIdentity() (string, error) {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	return strings.TrimSpace(string(raw)), err
}
