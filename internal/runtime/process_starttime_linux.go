//go:build linux

package relayruntime

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// linuxClockTicksPerSecond 是 /proc/<pid>/stat 中 starttime 的计数频率（USER_HZ）。
// Linux 上该值固定为 100，且 Go 在不引入 cgo 的前提下无法查询 sysconf。
const linuxClockTicksPerSecond = 100

// processStartTime 返回 pid 对应进程的启动时间。
// 平台不支持或进程不存在时返回 ok=false。
func processStartTime(pid int) (time.Time, bool) {
	if pid <= 0 {
		return time.Time{}, false
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	// comm 字段（字段 2）可能包含空格和括号，因此从最后一个 ')' 之后开始切分。
	text := string(raw)
	cut := strings.LastIndex(text, ")")
	if cut < 0 || cut+2 > len(text) {
		return time.Time{}, false
	}
	fields := strings.Fields(text[cut+2:])
	// ')' 之后第一个字段是 state（字段 3），starttime 是字段 22。
	const startTimeField = 22 - 3
	if len(fields) <= startTimeField {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(fields[startTimeField], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	boot, ok := linuxBootTime()
	if !ok {
		return time.Time{}, false
	}
	return boot.Add(time.Duration(ticks) * time.Second / linuxClockTicksPerSecond), true
}

// linuxBootTime 读取系统启动时刻（/proc/stat 的 btime 行）。
func linuxBootTime() (time.Time, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		value, found := strings.CutPrefix(line, "btime ")
		if !found {
			continue
		}
		seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		return time.Unix(seconds, 0), true
	}
	return time.Time{}, false
}
