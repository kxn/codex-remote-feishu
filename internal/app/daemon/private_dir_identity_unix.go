//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"syscall"
)

func privateDirectoryIdentity(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() {
		return "", fmt.Errorf("unsupported directory identity")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
