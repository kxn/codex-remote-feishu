package cronruntime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
)

type WorkspaceRow struct {
	RecordID string
	Key      string
	Name     string
	Status   string
}

type ReloadTaskChange string

const (
	ReloadTaskChangeAdded ReloadTaskChange = "added"
	ReloadTaskChangeKept  ReloadTaskChange = "kept"
)

type ReloadTaskItem struct {
	RecordID        string
	Name            string
	ScheduleType    string
	DailyHour       int
	DailyMinute     int
	IntervalMinutes int
	MaxConcurrency  int
	SourceType      JobSourceType
	SourceSummary   string
	WorkspaceKey    string
	WorkspaceName   string
	GitRepoInput    string
	NextRunAt       time.Time
	ChangeKind      ReloadTaskChange
	Reason          string
}

type ReloadError struct {
	TableName string
	RowNumber int
	RecordID  string
	TaskName  string
	FieldName string
	Message   string
}

func (e *ReloadError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

type ReloadResult struct {
	Jobs     []JobState
	Loaded   []ReloadTaskItem
	Disabled []ReloadTaskItem
	Stopped  []ReloadTaskItem
	Errors   []ReloadError
	TimeZone string
}

func (r ReloadResult) CompactSummary() string {
	parts := []string{
		fmt.Sprintf("已加载 %d 条任务", len(r.Loaded)),
		fmt.Sprintf("停用 %d 条", len(r.Disabled)),
	}
	if len(r.Stopped) > 0 {
		parts = append(parts, fmt.Sprintf("停止 %d 条", len(r.Stopped)))
	}
	summary := strings.Join(parts, "，") + "。"
	if len(r.Errors) > 0 {
		summary += fmt.Sprintf("\n发现 %d 条配置错误。", len(r.Errors))
	}
	return summary
}

func ReloadTaskItemFromJob(job JobState) ReloadTaskItem {
	return ReloadTaskItem{
		RecordID:        strings.TrimSpace(job.RecordID),
		Name:            strings.TrimSpace(job.Name),
		ScheduleType:    strings.TrimSpace(job.ScheduleType),
		DailyHour:       job.DailyHour,
		DailyMinute:     job.DailyMinute,
		IntervalMinutes: job.IntervalMinutes,
		MaxConcurrency:  DefaultMaxConcurrency(job.MaxConcurrency),
		SourceType:      job.SourceType,
		SourceSummary:   JobDisplaySource(job),
		WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
		GitRepoInput:    strings.TrimSpace(job.GitRepoSourceInput),
		NextRunAt:       job.NextRunAt,
	}
}

func ReloadTaskPreviewFromRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]WorkspaceRow, now time.Time, timeZone string) ReloadTaskItem {
	item := ReloadTaskItem{}
	if record == nil {
		return item
	}
	item.RecordID = strings.TrimSpace(stringValue(record.RecordId))
	item.Name = strings.TrimSpace(valueString(record.Fields["任务名"]))
	if item.Name == "" {
		item.Name = item.RecordID
	}
	item.ScheduleType = strings.TrimSpace(valueString(record.Fields["调度类型"]))
	item.MaxConcurrency = DefaultMaxConcurrency(valueInt(record.Fields[TaskConcurrencyField]))
	workspaceLinks := valueStringSlice(record.Fields[TaskWorkspaceField])
	item.GitRepoInput = strings.TrimSpace(valueString(record.Fields[TaskGitRepoInputField]))
	item.SourceType = InferJobSourceType(valueString(record.Fields[TaskSourceTypeField]), item.GitRepoInput, workspaceLinks)
	if item.SourceType == JobSourceWorkspace && len(workspaceLinks) == 1 {
		if workspaceRow, ok := workspacesByRecord[workspaceLinks[0]]; ok {
			item.WorkspaceKey = strings.TrimSpace(workspaceRow.Key)
			item.WorkspaceName = strings.TrimSpace(workspaceRow.Name)
		}
	}
	switch item.ScheduleType {
	case ScheduleTypeDaily:
		if hour, minute, ok := DailyTimeFromFields(record.Fields); ok {
			item.DailyHour = hour
			item.DailyMinute = minute
		}
	case ScheduleTypeInterval:
		if minutes, ok := intervalMinutesForLabel(strings.TrimSpace(valueString(record.Fields["间隔"]))); ok {
			item.IntervalMinutes = minutes
		}
	}
	job := JobState{
		RecordID:           item.RecordID,
		Name:               item.Name,
		ScheduleType:       item.ScheduleType,
		SourceType:         item.SourceType,
		DailyHour:          item.DailyHour,
		DailyMinute:        item.DailyMinute,
		IntervalMinutes:    item.IntervalMinutes,
		MaxConcurrency:     item.MaxConcurrency,
		WorkspaceKey:       item.WorkspaceKey,
		GitRepoSourceInput: item.GitRepoInput,
	}
	item.SourceSummary = JobDisplaySource(job)
	item.NextRunAt = NextRunAtIn(job, now, timeZone)
	return item
}

func NewReloadError(record *larkbitable.AppTableRecord, tableName string, rowNumber int, taskName, fieldName, message string) *ReloadError {
	return &ReloadError{
		TableName: strings.TrimSpace(tableName),
		RowNumber: rowNumber,
		RecordID:  strings.TrimSpace(stringValue(record.RecordId)),
		TaskName:  strings.TrimSpace(taskName),
		FieldName: strings.TrimSpace(fieldName),
		Message:   strings.TrimSpace(message),
	}
}

func JobFromReloadRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]WorkspaceRow, now time.Time, timeZone, tableName string, rowNumber int) (JobState, bool, *ReloadError) {
	if record == nil {
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, "", "", "empty task record")
	}
	name := strings.TrimSpace(valueString(record.Fields["任务名"]))
	if name == "" {
		name = strings.TrimSpace(stringValue(record.RecordId))
	}
	enabled, valid := valueBool(record.Fields["启用"])
	if !enabled && valid {
		return JobState{}, true, nil
	}
	if !valid {
		return JobState{}, false, NewReloadError(
			record,
			tableName,
			rowNumber,
			name,
			"启用",
			fmt.Sprintf("任务 `%s` 的启用值无效：%s", name, strings.TrimSpace(valueString(record.Fields["启用"]))),
		)
	}
	scheduleType := strings.TrimSpace(valueString(record.Fields["调度类型"]))
	prompt := strings.TrimSpace(valueString(record.Fields["提示词"]))
	if prompt == "" {
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, "提示词", fmt.Sprintf("任务 `%s` 缺少提示词", name))
	}
	workspaceLinks := valueStringSlice(record.Fields[TaskWorkspaceField])
	gitRepoInput := strings.TrimSpace(valueString(record.Fields[TaskGitRepoInputField]))
	sourceType := InferJobSourceType(valueString(record.Fields[TaskSourceTypeField]), gitRepoInput, workspaceLinks)
	maxConcurrency := DefaultMaxConcurrency(valueInt(record.Fields[TaskConcurrencyField]))
	timeoutMinutes := DefaultTimeoutMinutes(valueInt(record.Fields["超时（分钟）"]))
	job := JobState{
		RecordID:       strings.TrimSpace(stringValue(record.RecordId)),
		Name:           name,
		ScheduleType:   scheduleType,
		SourceType:     sourceType,
		Prompt:         prompt,
		MaxConcurrency: maxConcurrency,
		TimeoutMinutes: timeoutMinutes,
	}
	switch sourceType {
	case JobSourceWorkspace:
		if len(workspaceLinks) != 1 {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskWorkspaceField, fmt.Sprintf("任务 `%s` 需要且只能选择一个工作区", name))
		}
		workspaceRow, ok := workspacesByRecord[workspaceLinks[0]]
		if !ok || strings.TrimSpace(workspaceRow.Key) == "" {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskWorkspaceField, fmt.Sprintf("任务 `%s` 选择的工作区已不存在", name))
		}
		if strings.TrimSpace(workspaceRow.Status) == "已失效" {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskWorkspaceField, fmt.Sprintf("任务 `%s` 选择的工作区已失效", name))
		}
		job.WorkspaceKey = workspaceRow.Key
		job.WorkspaceRecordID = workspaceLinks[0]
	case JobSourceGitRepo:
		if gitRepoInput == "" {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskGitRepoInputField, fmt.Sprintf("任务 `%s` 缺少 Git 仓库引用", name))
		}
		spec, err := cronrepo.ParseSourceInput(gitRepoInput)
		if err != nil {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskGitRepoInputField, fmt.Sprintf("任务 `%s` 的 Git 仓库引用无效：%s", name, err.Error()))
		}
		job.GitRepoSourceInput = gitRepoInput
		job.GitRepoURL = spec.RepoURL
		job.GitRef = spec.Ref
	default:
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskSourceTypeField, fmt.Sprintf("任务 `%s` 的来源类型无效：%s", name, valueString(record.Fields[TaskSourceTypeField])))
	}
	switch scheduleType {
	case ScheduleTypeDaily:
		hour, minute, ok := DailyTimeFromFields(record.Fields)
		if !ok {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, "调度时间", fmt.Sprintf("任务 `%s` 的每天定时时间无效，应填写为 HH:mm", name))
		}
		job.DailyHour = hour
		job.DailyMinute = minute
	case ScheduleTypeInterval:
		intervalLabel := strings.TrimSpace(valueString(record.Fields["间隔"]))
		minutes, ok := intervalMinutesForLabel(intervalLabel)
		if !ok {
			return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, "间隔", fmt.Sprintf("任务 `%s` 的间隔值无效：%s", name, intervalLabel))
		}
		job.IntervalMinutes = minutes
	default:
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, "调度类型", fmt.Sprintf("任务 `%s` 的调度类型无效：%s", name, scheduleType))
	}
	job = NormalizeJobState(job)
	job.NextRunAt = NextRunAtIn(job, now, timeZone)
	return job, false, nil
}

func BuildReloadResult(records []*larkbitable.AppTableRecord, workspacesByRecord map[string]WorkspaceRow, now time.Time, previousJobs []JobState, timeZone string) ReloadResult {
	result := ReloadResult{TimeZone: strings.TrimSpace(timeZone)}
	loadedByRecord := map[string]ReloadTaskItem{}
	disabledByRecord := map[string]ReloadTaskItem{}
	errorByRecord := map[string]ReloadError{}
	previousByRecord := map[string]JobState{}
	for _, job := range previousJobs {
		recordID := strings.TrimSpace(job.RecordID)
		if recordID == "" {
			continue
		}
		previousByRecord[recordID] = job
	}
	for index, record := range records {
		rowNumber := index + 1
		preview := ReloadTaskPreviewFromRecord(record, workspacesByRecord, now, timeZone)
		job, disabled, reloadErr := JobFromReloadRecord(record, workspacesByRecord, now, timeZone, TasksTableName, rowNumber)
		switch {
		case disabled:
			preview.Reason = "表格中已停用"
			result.Disabled = append(result.Disabled, preview)
			if preview.RecordID != "" {
				disabledByRecord[preview.RecordID] = preview
			}
		case reloadErr != nil:
			result.Errors = append(result.Errors, *reloadErr)
			if reloadErr.RecordID != "" {
				errorByRecord[reloadErr.RecordID] = *reloadErr
			}
		default:
			item := ReloadTaskItemFromJob(job)
			if _, exists := previousByRecord[item.RecordID]; exists {
				item.ChangeKind = ReloadTaskChangeKept
			} else {
				item.ChangeKind = ReloadTaskChangeAdded
			}
			result.Jobs = append(result.Jobs, job)
			result.Loaded = append(result.Loaded, item)
			if item.RecordID != "" {
				loadedByRecord[item.RecordID] = item
			}
		}
	}
	for _, previous := range previousJobs {
		recordID := strings.TrimSpace(previous.RecordID)
		if recordID == "" {
			continue
		}
		if _, stillLoaded := loadedByRecord[recordID]; stillLoaded {
			continue
		}
		stopped := ReloadTaskItemFromJob(previous)
		switch {
		case disabledByRecord[recordID].RecordID != "":
			stopped.Reason = "表格中已停用"
		case errorByRecord[recordID].RecordID != "":
			stopped.Reason = "配置错误，未继续生效"
		default:
			stopped.Reason = "表格中已删除"
		}
		result.Stopped = append(result.Stopped, stopped)
	}
	return result
}

func valueString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"text", "name", "label", "title", "value", "id", "record_id", "recordId"} {
			if text := valueString(typed[key]); text != "" {
				return text
			}
		}
		if values := valueStringSlice(typed); len(values) > 0 {
			return strings.Join(values, "\n")
		}
		return ""
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(valueString(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case []string:
		return strings.Join(typed, "\n")
	default:
		return fmt.Sprint(value)
	}
}

func valueBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case nil:
		return false, true
	case bool:
		return typed, true
	case int:
		return typed != 0, true
	case int32:
		return typed != 0, true
	case int64:
		return typed != 0, true
	case float32:
		return typed != 0, true
	case float64:
		return typed != 0, true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed != 0, true
		}
		return false, false
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "0", "false", "off", "no", "unchecked", "停用":
			return false, true
		case "1", "true", "on", "yes", "checked", "启用":
			return true, true
		default:
			return false, false
		}
	case map[string]any:
		for _, key := range []string{"checked", "value", "text", "name", "label"} {
			if nested, ok := typed[key]; ok {
				if enabled, valid := valueBool(nested); valid {
					return enabled, true
				}
			}
		}
		return false, false
	case []any:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return valueBool(typed[0])
		}
		return false, false
	case []string:
		if len(typed) == 0 {
			return false, true
		}
		if len(typed) == 1 {
			return valueBool(typed[0])
		}
		return false, false
	default:
		return valueBool(fmt.Sprint(value))
	}
}

func valueStringSlice(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), typed...)
	case map[string]any:
		for _, key := range []string{"record_ids", "recordIds", "ids", "values"} {
			if values := valueStringSlice(typed[key]); len(values) > 0 {
				return values
			}
		}
		for _, key := range []string{"record_id", "recordId", "id", "value", "text", "name", "label"} {
			if text := strings.TrimSpace(valueString(typed[key])); text != "" {
				return []string{text}
			}
		}
		return nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if nested := valueStringSlice(item); len(nested) > 0 {
				values = append(values, nested...)
				continue
			}
			if text := strings.TrimSpace(valueString(item)); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		if text := strings.TrimSpace(valueString(value)); text != "" {
			return []string{text}
		}
		return nil
	}
}

func valueInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case map[string]any:
		for _, key := range []string{"value", "number", "text"} {
			if keyValue, ok := typed[key]; ok {
				return valueInt(keyValue)
			}
		}
		return 0
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}
