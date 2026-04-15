package daemon

import (
	"strconv"
	"strings"
)

func cronDailyTimeFromFields(fields map[string]any) (int, int, bool) {
	if len(fields) == 0 {
		return 0, 0, false
	}
	if text := strings.TrimSpace(cronValueString(fields["调度时间"])); text != "" {
		return parseCronClockText(text)
	}
	hourValue, hourExists := fields["每天-时"]
	minuteValue, minuteExists := fields["每天-分"]
	if !hourExists && !minuteExists {
		return 0, 0, false
	}
	hour := cronValueInt(hourValue)
	minute := cronValueInt(minuteValue)
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func parseCronClockText(value string) (int, int, bool) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "：", ":"))
	if value == "" {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, ":")
	if !ok {
		return 0, 0, false
	}
	if strings.Contains(strings.TrimSpace(right), ":") {
		return 0, 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return 0, 0, false
	}
	minute, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return 0, 0, false
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}
