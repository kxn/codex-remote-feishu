package cronruntime

import (
	"fmt"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/app/bitablevalue"
	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
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
	item.RecordID = strings.TrimSpace(xutil.StringValue(record.RecordId))
	item.Name = strings.TrimSpace(bitablevalue.String(record.Fields["任务名"]))
	if item.Name == "" {
		item.Name = item.RecordID
	}
	item.ScheduleType = strings.TrimSpace(bitablevalue.String(record.Fields["调度类型"]))
	item.MaxConcurrency = DefaultMaxConcurrency(bitablevalue.Int(record.Fields[TaskConcurrencyField]))
	workspaceLinks := bitablevalue.StringSlice(record.Fields[TaskWorkspaceField])
	item.GitRepoInput = strings.TrimSpace(bitablevalue.String(record.Fields[TaskGitRepoInputField]))
	item.SourceType = InferJobSourceType(bitablevalue.String(record.Fields[TaskSourceTypeField]), item.GitRepoInput, workspaceLinks)
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
		if minutes, ok := intervalMinutesForLabel(strings.TrimSpace(bitablevalue.String(record.Fields["间隔"]))); ok {
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
		RecordID:  strings.TrimSpace(xutil.StringValue(record.RecordId)),
		TaskName:  strings.TrimSpace(taskName),
		FieldName: strings.TrimSpace(fieldName),
		Message:   strings.TrimSpace(message),
	}
}

func JobFromReloadRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]WorkspaceRow, now time.Time, timeZone, tableName string, rowNumber int) (JobState, bool, *ReloadError) {
	if record == nil {
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, "", "", "empty task record")
	}
	name := strings.TrimSpace(bitablevalue.String(record.Fields["任务名"]))
	if name == "" {
		name = strings.TrimSpace(xutil.StringValue(record.RecordId))
	}
	enabled, valid := bitablevalue.Bool(record.Fields["启用"])
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
			fmt.Sprintf("任务 `%s` 的启用值无效：%s", name, strings.TrimSpace(bitablevalue.String(record.Fields["启用"]))),
		)
	}
	scheduleType := strings.TrimSpace(bitablevalue.String(record.Fields["调度类型"]))
	prompt := strings.TrimSpace(bitablevalue.String(record.Fields["提示词"]))
	if prompt == "" {
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, "提示词", fmt.Sprintf("任务 `%s` 缺少提示词", name))
	}
	workspaceLinks := bitablevalue.StringSlice(record.Fields[TaskWorkspaceField])
	gitRepoInput := strings.TrimSpace(bitablevalue.String(record.Fields[TaskGitRepoInputField]))
	sourceType := InferJobSourceType(bitablevalue.String(record.Fields[TaskSourceTypeField]), gitRepoInput, workspaceLinks)
	maxConcurrency := DefaultMaxConcurrency(bitablevalue.Int(record.Fields[TaskConcurrencyField]))
	timeoutMinutes := DefaultTimeoutMinutes(bitablevalue.Int(record.Fields["超时（分钟）"]))
	job := JobState{
		RecordID:       strings.TrimSpace(xutil.StringValue(record.RecordId)),
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
		return JobState{}, false, NewReloadError(record, tableName, rowNumber, name, TaskSourceTypeField, fmt.Sprintf("任务 `%s` 的来源类型无效：%s", name, bitablevalue.String(record.Fields[TaskSourceTypeField])))
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
		intervalLabel := strings.TrimSpace(bitablevalue.String(record.Fields["间隔"]))
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
