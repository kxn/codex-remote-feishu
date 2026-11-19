//go:build !darwin && !linux && !windows

package relayruntime

import "time"

// processStartTime 在缺少实现的平台上不可用；此时锁判定退化为只检查 PID 是否存活。
func processStartTime(int) (time.Time, bool) {
	return time.Time{}, false
}
