//go:build darwin

package relayruntime

import (
	"time"

	"golang.org/x/sys/unix"
)

// processStartTime 返回 pid 对应进程的启动时间。
// 平台不支持或进程不存在时返回 ok=false。
func processStartTime(pid int) (time.Time, bool) {
	if pid <= 0 {
		return time.Time{}, false
	}
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil {
		return time.Time{}, false
	}
	start := info.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return time.Time{}, false
	}
	return time.Unix(start.Sec, int64(start.Usec)*1000), true
}
