package orchestrator

import (
	"fmt"
	"strings"
	"time"
)

func threadLastUsedAt(view *mergedThreadView) time.Time {
	if view == nil || view.Thread == nil {
		return time.Time{}
	}
	return view.Thread.LastUsedAt
}

func threadViewsLatestUsedAt(views []*mergedThreadView) time.Time {
	latest := time.Time{}
	for _, view := range views {
		if usedAt := threadLastUsedAt(view); usedAt.After(latest) {
			latest = usedAt
		}
	}
	return latest
}

func humanizeRelativeTime(now, then time.Time) string {
	if then.IsZero() {
		return "时间未知"
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if then.After(now) {
		then = now
	}
	delta := now.Sub(then)
	if delta < time.Second {
		return "刚刚"
	}
	type unit struct {
		name string
		secs int64
	}
	units := []unit{
		{name: "年", secs: 365 * 24 * 60 * 60},
		{name: "个月", secs: 30 * 24 * 60 * 60},
		{name: "天", secs: 24 * 60 * 60},
		{name: "小时", secs: 60 * 60},
		{name: "分", secs: 60},
		{name: "秒", secs: 1},
	}
	remaining := int64(delta / time.Second)
	parts := make([]string, 0, 2)
	for _, item := range units {
		if remaining < item.secs {
			continue
		}
		value := remaining / item.secs
		remaining -= value * item.secs
		parts = append(parts, fmt.Sprintf("%d%s", value, item.name))
		if len(parts) == 2 {
			break
		}
	}
	if len(parts) == 0 {
		return "刚刚"
	}
	return strings.Join(parts, "") + "前"
}
