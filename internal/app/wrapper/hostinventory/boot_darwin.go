//go:build darwin

package hostinventory

import (
	"encoding/hex"
	"golang.org/x/sys/unix"
)

func BootIdentity() (string, error) {
	v, err := unix.SysctlRaw("kern.boottime")
	return hex.EncodeToString(v), err
}
