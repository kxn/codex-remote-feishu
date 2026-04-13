//go:build windows

package launcher

import (
	"syscall"

	"golang.org/x/sys/windows"
)

type consoleCtrlHandlerFunc func(ctrlType uint32) bool

var setConsoleCtrlHandler = func(handler uintptr, add bool) error {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	proc := kernel32.NewProc("SetConsoleCtrlHandler")
	ret, _, callErr := proc.Call(handler, boolToUintptr(add))
	if ret != 0 {
		return nil
	}
	if callErr != windows.ERROR_SUCCESS {
		return callErr
	}
	return syscall.EINVAL
}

func registerPlatformConsoleCloseBridge(cancel func()) (func(), error) {
	if cancel == nil {
		return nil, nil
	}
	return registerConsoleCtrlHandler(func(ctrlType uint32) bool {
		switch ctrlType {
		case windows.CTRL_CLOSE_EVENT, windows.CTRL_LOGOFF_EVENT, windows.CTRL_SHUTDOWN_EVENT:
			cancel()
			return true
		default:
			return false
		}
	})
}

func registerConsoleCtrlHandler(handler consoleCtrlHandlerFunc) (func(), error) {
	callback := windows.NewCallback(func(ctrlType uint32) uintptr {
		if handler(ctrlType) {
			return 1
		}
		return 0
	})
	if err := setConsoleCtrlHandler(callback, true); err != nil {
		return nil, err
	}
	return func() {
		_ = setConsoleCtrlHandler(callback, false)
	}, nil
}

func boolToUintptr(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}
