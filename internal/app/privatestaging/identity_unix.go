//go:build !windows

package privatestaging

import (
	"fmt"
	"os"
	"syscall"
)

func identity(f *os.File) (string, error) {
	info, e := f.Stat()
	if e != nil {
		return "", e
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported file identity")
	}
	return fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}
func privateDirectory(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.IsDir() && info.Mode().Perm() == 0700 && st.Uid == uint32(os.Geteuid())
}
