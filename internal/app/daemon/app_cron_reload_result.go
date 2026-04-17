package daemon

import (
	"fmt"
	"strings"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/app/cronrepo"
)

type cronReloadTaskChange string

const (
	cronReloadTaskChangeAdded cronReloadTaskChange = "added"
	cronReloadTaskChangeKept  cronReloadTaskChange = "kept"
)

type cronReloadTaskItem struct {
	RecordID        string
	Name            string
	ScheduleType    string
	DailyHour       int
	DailyMinute     int
	IntervalMinutes int
	MaxConcurrency  int
	SourceType      cronJobSourceType
	SourceSummary   string
	WorkspaceKey    string
	WorkspaceName   string
	GitRepoInput    string
	NextRunAt       time.Time
	ChangeKind      cronReloadTaskChange
	Reason          string
}

type cronReloadError struct {
	TableName string
	RowNumber int
	RecordID  string
	TaskName  string
	FieldName string
	Message   string
}

func (e *cronReloadError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

type cronReloadResult struct {
	Jobs             []cronJobState
	Loaded           []cronReloadTaskItem
	Disabled         []cronReloadTaskItem
	Stopped          []cronReloadTaskItem
	Errors           []cronReloadError
	OwnerBoundFilled bool
	TimeZone         string
}

func (r cronReloadResult) CompactSummary() string {
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

func cronReloadTaskItemFromJob(job cronJobState) cronReloadTaskItem {
	return cronReloadTaskItem{
		RecordID:        strings.TrimSpace(job.RecordID),
		Name:            strings.TrimSpace(job.Name),
		ScheduleType:    strings.TrimSpace(job.ScheduleType),
		DailyHour:       job.DailyHour,
		DailyMinute:     job.DailyMinute,
		IntervalMinutes: job.IntervalMinutes,
		MaxConcurrency:  cronDefaultMaxConcurrency(job.MaxConcurrency),
		SourceType:      job.SourceType,
		SourceSummary:   cronJobDisplaySource(job),
		WorkspaceKey:    strings.TrimSpace(job.WorkspaceKey),
		GitRepoInput:    strings.TrimSpace(job.GitRepoSourceInput),
		NextRunAt:       job.NextRunAt,
	}
}

func cronReloadTaskPreviewFromRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, timeZone string) cronReloadTaskItem {
	item := cronReloadTaskItem{}
	if record == nil {
		return item
	}
	item.RecordID = strings.TrimSpace(stringValue(record.RecordId))
	item.Name = strings.TrimSpace(cronValueString(record.Fields["任务名"]))
	if item.Name == "" {
		item.Name = item.RecordID
	}
	item.ScheduleType = strings.TrimSpace(cronValueString(record.Fields["调度类型"]))
	item.MaxConcurrency = cronDefaultMaxConcurrency(cronValueInt(record.Fields[cronTaskConcurrencyField]))
	workspaceLinks := cronValueStringSlice(record.Fields[cronTaskWorkspaceField])
	item.GitRepoInput = strings.TrimSpace(cronValueString(record.Fields[cronTaskGitRepoInputField]))
	item.SourceType = cronInferJobSourceType(cronValueString(record.Fields[cronTaskSourceTypeField]), item.GitRepoInput, workspaceLinks)
	if item.SourceType == cronJobSourceWorkspace && len(workspaceLinks) == 1 {
		if workspaceRow, ok := workspacesByRecord[workspaceLinks[0]]; ok {
			item.WorkspaceKey = strings.TrimSpace(workspaceRow.Key)
			item.WorkspaceName = strings.TrimSpace(workspaceRow.Name)
		}
	}
	switch item.ScheduleType {
	case cronScheduleTypeDaily:
		if hour, minute, ok := cronDailyTimeFromFields(record.Fields); ok {
			item.DailyHour = hour
			item.DailyMinute = minute
		}
	case cronScheduleTypeInterval:
		if minutes, ok := intervalMinutesForLabel(strings.TrimSpace(cronValueString(record.Fields["间隔"]))); ok {
			item.IntervalMinutes = minutes
		}
	}
	job := cronJobState{
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
	item.SourceSummary = cronJobDisplaySource(job)
	item.NextRunAt = cronNextRunAtIn(job, now, timeZone)
	return item
}

func cronNewReloadError(record *larkbitable.AppTableRecord, tableName string, rowNumber int, taskName, fieldName, message string) *cronReloadError {
	return &cronReloadError{
		TableName: strings.TrimSpace(tableName),
		RowNumber: rowNumber,
		RecordID:  strings.TrimSpace(stringValue(record.RecordId)),
		TaskName:  strings.TrimSpace(taskName),
		FieldName: strings.TrimSpace(fieldName),
		Message:   strings.TrimSpace(message),
	}
}

func cronJobFromReloadRecord(record *larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, timeZone, tableName string, rowNumber int) (cronJobState, bool, *cronReloadError) {
	if record == nil {
		return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, "", "", "empty task record")
	}
	name := strings.TrimSpace(cronValueString(record.Fields["任务名"]))
	if name == "" {
		name = strings.TrimSpace(stringValue(record.RecordId))
	}
	enabled, valid := cronValueBool(record.Fields["启用"])
	if !enabled && valid {
		return cronJobState{}, true, nil
	}
	if !valid {
		return cronJobState{}, false, cronNewReloadError(
			record,
			tableName,
			rowNumber,
			name,
			"启用",
			fmt.Sprintf("任务 `%s` 的启用值无效：%s", name, strings.TrimSpace(cronValueString(record.Fields["启用"]))),
		)
	}
	scheduleType := strings.TrimSpace(cronValueString(record.Fields["调度类型"]))
	prompt := strings.TrimSpace(cronValueString(record.Fields["提示词"]))
	if prompt == "" {
		return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, "提示词", fmt.Sprintf("任务 `%s` 缺少提示词", name))
	}
	workspaceLinks := cronValueStringSlice(record.Fields[cronTaskWorkspaceField])
	gitRepoInput := strings.TrimSpace(cronValueString(record.Fields[cronTaskGitRepoInputField]))
	sourceType := cronInferJobSourceType(cronValueString(record.Fields[cronTaskSourceTypeField]), gitRepoInput, workspaceLinks)
	maxConcurrency := cronDefaultMaxConcurrency(cronValueInt(record.Fields[cronTaskConcurrencyField]))
	timeoutMinutes := cronDefaultTimeoutMinutes(cronValueInt(record.Fields["超时（分钟）"]))
	job := cronJobState{
		RecordID:       strings.TrimSpace(stringValue(record.RecordId)),
		Name:           name,
		ScheduleType:   scheduleType,
		SourceType:     sourceType,
		Prompt:         prompt,
		MaxConcurrency: maxConcurrency,
		TimeoutMinutes: timeoutMinutes,
	}
	switch sourceType {
	case cronJobSourceWorkspace:
		if len(workspaceLinks) != 1 {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskWorkspaceField, fmt.Sprintf("任务 `%s` 需要且只能选择一个工作区", name))
		}
		workspaceRow, ok := workspacesByRecord[workspaceLinks[0]]
		if !ok || strings.TrimSpace(workspaceRow.Key) == "" {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskWorkspaceField, fmt.Sprintf("任务 `%s` 选择的工作区已不存在", name))
		}
		if strings.TrimSpace(workspaceRow.Status) == "已失效" {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskWorkspaceField, fmt.Sprintf("任务 `%s` 选择的工作区已失效", name))
		}
		job.WorkspaceKey = workspaceRow.Key
		job.WorkspaceRecordID = workspaceLinks[0]
	case cronJobSourceGitRepo:
		if gitRepoInput == "" {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskGitRepoInputField, fmt.Sprintf("任务 `%s` 缺少 Git 仓库引用", name))
		}
		spec, err := cronrepo.ParseSourceInput(gitRepoInput)
		if err != nil {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskGitRepoInputField, fmt.Sprintf("任务 `%s` 的 Git 仓库引用无效：%s", name, err.Error()))
		}
		job.GitRepoSourceInput = gitRepoInput
		job.GitRepoURL = spec.RepoURL
		job.GitRef = spec.Ref
	default:
		return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, cronTaskSourceTypeField, fmt.Sprintf("任务 `%s` 的来源类型无效：%s", name, cronValueString(record.Fields[cronTaskSourceTypeField])))
	}
	switch scheduleType {
	case cronScheduleTypeDaily:
		hour, minute, ok := cronDailyTimeFromFields(record.Fields)
		if !ok {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, "调度时间", fmt.Sprintf("任务 `%s` 的每天定时时间无效，应填写为 HH:mm", name))
		}
		job.DailyHour = hour
		job.DailyMinute = minute
	case cronScheduleTypeInterval:
		intervalLabel := strings.TrimSpace(cronValueString(record.Fields["间隔"]))
		minutes, ok := intervalMinutesForLabel(intervalLabel)
		if !ok {
			return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, "间隔", fmt.Sprintf("任务 `%s` 的间隔值无效：%s", name, intervalLabel))
		}
		job.IntervalMinutes = minutes
	default:
		return cronJobState{}, false, cronNewReloadError(record, tableName, rowNumber, name, "调度类型", fmt.Sprintf("任务 `%s` 的调度类型无效：%s", name, scheduleType))
	}
	job = cronNormalizeJobState(job)
	job.NextRunAt = cronNextRunAtIn(job, now, timeZone)
	return job, false, nil
}

func cronBuildReloadResult(records []*larkbitable.AppTableRecord, workspacesByRecord map[string]cronWorkspaceRow, now time.Time, previousJobs []cronJobState, timeZone string) cronReloadResult {
	result := cronReloadResult{TimeZone: strings.TrimSpace(timeZone)}
	loadedByRecord := map[string]cronReloadTaskItem{}
	disabledByRecord := map[string]cronReloadTaskItem{}
	errorByRecord := map[string]cronReloadError{}
	previousByRecord := map[string]cronJobState{}
	for _, job := range previousJobs {
		recordID := strings.TrimSpace(job.RecordID)
		if recordID == "" {
			continue
		}
		previousByRecord[recordID] = job
	}
	for index, record := range records {
		rowNumber := index + 1
		preview := cronReloadTaskPreviewFromRecord(record, workspacesByRecord, now, timeZone)
		job, disabled, reloadErr := cronJobFromReloadRecord(record, workspacesByRecord, now, timeZone, cronTasksTableName, rowNumber)
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
			item := cronReloadTaskItemFromJob(job)
			if _, exists := previousByRecord[item.RecordID]; exists {
				item.ChangeKind = cronReloadTaskChangeKept
			} else {
				item.ChangeKind = cronReloadTaskChangeAdded
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
		stopped := cronReloadTaskItemFromJob(previous)
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
