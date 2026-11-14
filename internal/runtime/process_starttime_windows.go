//go:build windows

package relayruntime

import (
	"time"

	"golang.org/x/sys/windows"
)

// processStartTime 返回 pid 对应进程的启动时间。
// 平台不支持或进程不存在时返回 ok=false。
func processStartTime(pid int) (time.Time, bool) {
	if pid <= 0 {
		return time.Time{}, false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return time.Time{}, false
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, false
	}
	if creation == (windows.Filetime{}) {
		return time.Time{}, false
	}
	// Filetime.Nanoseconds 已经换算为 Unix 纪元起的纳秒。
	return time.Unix(0, creation.Nanoseconds()), true
}
