package daemon

import (
	"fmt"
	"strings"
)

func (r cronReloadResult) DetailedText() string {
	lines := []string{r.CompactSummary()}
	if r.OwnerBoundFilled {
		lines = append(lines, "已回填正式 owner 绑定。")
	}
	appendTaskSection := func(title string, items []cronReloadTaskItem, plannedLabel string) {
		if len(items) == 0 {
			return
		}
		lines = append(lines, "", title)
		for _, item := range items {
			lines = append(lines, "- "+cronReloadTaskNoticeLine(item, plannedLabel, r.TimeZone))
		}
	}
	appendTaskSection("已加载：", r.Loaded, "下次")
	appendTaskSection("已停用：", r.Disabled, "原计划")
	appendTaskSection("本次停止：", r.Stopped, "原计划")
	if len(r.Errors) > 0 {
		lines = append(lines, "", "配置错误：")
		for _, item := range r.Errors {
			lines = append(lines, "- "+cronReloadErrorNoticeLine(item))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func cronReloadTaskNoticeLine(item cronReloadTaskItem, plannedLabel, timeZone string) string {
	segments := []string{}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = strings.TrimSpace(item.RecordID)
	}
	if name != "" {
		switch item.ChangeKind {
		case cronReloadTaskChangeAdded:
			name += "（新增）"
		case cronReloadTaskChangeKept:
			name += "（保留）"
		}
		segments = append(segments, fmt.Sprintf("`%s`", name))
	}
	if schedule := cronReloadTaskScheduleText(item); schedule != "" {
		segments = append(segments, schedule)
	}
	segments = append(segments, cronJobConcurrencyText(item.MaxConcurrency))
	if next := cronReloadTaskNextRunText(item, plannedLabel, timeZone); next != "" {
		segments = append(segments, next)
	}
	if reason := strings.TrimSpace(item.Reason); reason != "" {
		segments = append(segments, "原因："+reason)
	}
	return strings.Join(segments, "｜")
}

func cronReloadTaskScheduleText(item cronReloadTaskItem) string {
	switch strings.TrimSpace(item.ScheduleType) {
	case cronScheduleTypeDaily:
		if item.DailyHour >= 0 && item.DailyHour <= 23 && item.DailyMinute >= 0 && item.DailyMinute <= 59 {
			return fmt.Sprintf("%s｜%02d:%02d", cronScheduleTypeDaily, item.DailyHour, item.DailyMinute)
		}
		return cronScheduleTypeDaily
	case cronScheduleTypeInterval:
		if item.IntervalMinutes > 0 {
			return fmt.Sprintf("%s｜每%d分钟", cronScheduleTypeInterval, item.IntervalMinutes)
		}
		return cronScheduleTypeInterval
	default:
		return firstNonEmpty(strings.TrimSpace(item.ScheduleType), "调度方式未识别")
	}
}

func cronReloadTaskNextRunText(item cronReloadTaskItem, label, timeZone string) string {
	if item.NextRunAt.IsZero() {
		return ""
	}
	label = firstNonEmpty(strings.TrimSpace(label), "下次")
	return fmt.Sprintf("%s %s", label, cronSchedulerTimeIn(item.NextRunAt, timeZone).Format("01-02 15:04"))
}

func cronReloadErrorNoticeLine(item cronReloadError) string {
	parts := []string{}
	name := strings.TrimSpace(item.TaskName)
	if name != "" {
		parts = append(parts, fmt.Sprintf("`%s`", name))
	}
	location := cronReloadTableLabel(item.TableName)
	if item.RowNumber > 0 {
		location = fmt.Sprintf("%s 第 %d 行", firstNonEmpty(location, "任务配置表"), item.RowNumber)
	}
	if location != "" {
		parts = append(parts, location)
	}
	if field := strings.TrimSpace(item.FieldName); field != "" {
		parts = append(parts, "字段："+field)
	}
	if recordID := strings.TrimSpace(item.RecordID); recordID != "" {
		parts = append(parts, "记录："+recordID)
	}
	if message := strings.TrimSpace(item.Message); message != "" {
		parts = append(parts, message)
	}
	return strings.Join(parts, "｜")
}

func cronReloadTableLabel(name string) string {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return ""
	case strings.HasSuffix(name, "表"):
		return name
	default:
		return name + "表"
	}
}
