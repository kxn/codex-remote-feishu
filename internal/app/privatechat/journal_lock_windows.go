//go:build windows

package privatechat

import (
	"golang.org/x/sys/windows"
	"os"
)

func lockJournal(path string) (*os.File, error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var o windows.Overlapped
	if err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &o); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlockJournal(f *os.File) error {
	var o windows.Overlapped
	err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &o)
	e := f.Close()
	if err != nil {
		return err
	}
	return e
}
