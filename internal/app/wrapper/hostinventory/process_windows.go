//go:build windows

package hostinventory

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
)

func captureProcess(pid int) (string, bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer windows.CloseHandle(h)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return "", false, err
	}
	status, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%d:%d", creation.HighDateTime, creation.LowDateTime), status != windows.WAIT_OBJECT_0, nil
}
