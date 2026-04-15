package daemon

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
)

const (
	cronStateSchemaVersion   = 1
	cronDefaultTimeoutMinute = 30
	cronScheduleScanEvery    = time.Second
	cronExitGrace            = 20 * time.Second
	cronBitableBootstrapTTL  = 2 * time.Minute
	cronBitableWorkspaceTTL  = 90 * time.Second
	cronBitablePermissionTTL = 30 * time.Second
	cronReloadWorkspaceTTL   = 45 * time.Second
	cronReloadTasksTTL       = 90 * time.Second
	cronWritebackRunsTTL     = 30 * time.Second
	cronWritebackTasksTTL    = 30 * time.Second
	cronInstancePrefix       = "inst-cron-"
	cronRunsTableName        = "运行记录"
	cronTasksTableName       = "任务配置"
	cronWorkspacesTableName  = "工作区清单"
	cronMetaTableName        = "元信息"
)

type cronStateFile struct {
	SchemaVersion       int               `json:"schema_version"`
	InstanceScopeKey    string            `json:"instance_scope_key,omitempty"`
	InstanceLabel       string            `json:"instance_label,omitempty"`
	GatewayID           string            `json:"gateway_id,omitempty"`
	Bitable             *cronBitableState `json:"bitable,omitempty"`
	Jobs                []cronJobState    `json:"jobs,omitempty"`
	LastWorkspaceSyncAt time.Time         `json:"last_workspace_sync_at,omitempty"`
	LastReloadAt        time.Time         `json:"last_reload_at,omitempty"`
	LastReloadSummary   string            `json:"last_reload_summary,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at,omitempty"`
}

type cronBitableState struct {
	AppToken     string       `json:"app_token,omitempty"`
	AppURL       string       `json:"app_url,omitempty"`
	DefaultTable string       `json:"default_table_id,omitempty"`
	MetaRecordID string       `json:"meta_record_id,omitempty"`
	Tables       cronTableIDs `json:"tables,omitempty"`
	CreatedAt    time.Time    `json:"created_at,omitempty"`
	LastVerified time.Time    `json:"last_verified,omitempty"`
}

type cronTableIDs struct {
	Tasks      string `json:"tasks,omitempty"`
	Runs       string `json:"runs,omitempty"`
	Workspaces string `json:"workspaces,omitempty"`
	Meta       string `json:"meta,omitempty"`
}

type cronJobState struct {
	RecordID          string    `json:"record_id,omitempty"`
	Name              string    `json:"name,omitempty"`
	ScheduleType      string    `json:"schedule_type,omitempty"`
	DailyHour         int       `json:"daily_hour,omitempty"`
	DailyMinute       int       `json:"daily_minute,omitempty"`
	IntervalMinutes   int       `json:"interval_minutes,omitempty"`
	WorkspaceKey      string    `json:"workspace_key,omitempty"`
	WorkspaceRecordID string    `json:"workspace_record_id,omitempty"`
	Prompt            string    `json:"prompt,omitempty"`
	TimeoutMinutes    int       `json:"timeout_minutes,omitempty"`
	NextRunAt         time.Time `json:"next_run_at,omitempty"`
}

type cronRunState struct {
	RunID            string
	InstanceID       string
	GatewayID        string
	JobRecordID      string
	JobName          string
	WorkspaceKey     string
	Prompt           string
	TimeoutMinutes   int
	TriggeredAt      time.Time
	StartedAt        time.Time
	CompletedAt      time.Time
	PID              int
	CommandID        string
	ThreadID         string
	TurnID           string
	Status           string
	ErrorMessage     string
	FinalMessage     string
	PendingFinalText string
	Buffers          map[string]*cronItemBuffer
}

type cronItemBuffer struct {
	ItemID   string
	ItemKind string
	Chunks   []string
}

type cronExitTarget struct {
	InstanceID string
	PID        int
	Deadline   time.Time
}

func (a *App) cronStatePath() string {
	if strings.TrimSpace(a.headlessRuntime.Paths.StateDir) != "" {
		return filepath.Join(a.headlessRuntime.Paths.StateDir, "cron-state.json")
	}
	return filepath.Join(".", "cron-state.json")
}

func (a *App) loadCronStateLocked(create bool) (*cronStateFile, error) {
	if a.cronLoaded {
		if a.cronState != nil {
			a.cronState = normalizeCronState(*a.cronState)
		}
		if a.cronState == nil && create {
			stateValue, err := a.newCronStateLocked()
			if err != nil {
				return nil, err
			}
			a.cronState = stateValue
			if err := a.writeCronStateLocked(); err != nil {
				return nil, err
			}
		}
		return a.cronState, nil
	}
	a.cronLoaded = true
	raw, err := os.ReadFile(a.cronStatePath())
	switch {
	case os.IsNotExist(err):
		if !create {
			return nil, nil
		}
		stateValue, createErr := a.newCronStateLocked()
		if createErr != nil {
			return nil, createErr
		}
		a.cronState = stateValue
		if err := a.writeCronStateLocked(); err != nil {
			return nil, err
		}
		return a.cronState, nil
	case err != nil:
		return nil, err
	}
	var stateValue cronStateFile
	if err := json.Unmarshal(raw, &stateValue); err != nil {
		return nil, err
	}
	stateValue = *normalizeCronState(stateValue)
	a.cronState = &stateValue
	return a.cronState, nil
}

func normalizeCronState(stateValue cronStateFile) *cronStateFile {
	stateValue.SchemaVersion = cronStateSchemaVersion
	if stateValue.Bitable == nil {
		stateValue.Bitable = &cronBitableState{}
	}
	if stateValue.Jobs == nil {
		stateValue.Jobs = []cronJobState{}
	}
	return &stateValue
}

func (a *App) newCronStateLocked() (*cronStateFile, error) {
	scopeKey, label, err := a.cronInstanceMetadataLocked()
	if err != nil {
		return nil, err
	}
	return &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: scopeKey,
		InstanceLabel:    label,
		Bitable:          &cronBitableState{},
		Jobs:             []cronJobState{},
		UpdatedAt:        time.Now().UTC(),
	}, nil
}

func (a *App) writeCronStateLocked() error {
	if a.cronState == nil {
		return nil
	}
	a.cronState.SchemaVersion = cronStateSchemaVersion
	a.cronState.UpdatedAt = time.Now().UTC()
	return writeJSONFileAtomic(a.cronStatePath(), a.cronState, 0o600)
}

func (a *App) cronInstanceMetadataLocked() (string, string, error) {
	stateValue, _, err := a.loadUpgradeStateLocked(true)
	if err != nil {
		return "", "", err
	}
	instanceID := strings.TrimSpace(stateValue.InstanceID)
	if instanceID == "" {
		instanceID = fallbackCronInstanceID(stateValue.ConfigPath, stateValue.StatePath)
	}
	return instanceID, instanceID, nil
}

func fallbackCronInstanceID(values ...string) string {
	hasher := sha1.New()
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		_, _ = hasher.Write([]byte(value))
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if len(sum) > 12 {
		sum = sum[:12]
	}
	if sum == "" {
		return "stable"
	}
	return "fallback-" + sum
}

func cronAppTitle(instanceLabel string) string {
	instanceLabel = strings.TrimSpace(instanceLabel)
	if instanceLabel == "" {
		instanceLabel = "stable"
	}
	return "Codex 定时任务（" + instanceLabel + "）"
}

func cronTimeZone() string {
	name := strings.TrimSpace(time.Now().Location().String())
	switch strings.ToLower(name) {
	case "", "local":
		return "Asia/Shanghai"
	default:
		return name
	}
}

func cronDefaultTimeoutMinutes(raw int) int {
	if raw > 0 {
		return raw
	}
	return cronDefaultTimeoutMinute
}

func cronStateHasBinding(stateValue *cronStateFile) bool {
	return stateValue != nil && stateValue.Bitable != nil && strings.TrimSpace(stateValue.Bitable.AppToken) != ""
}

func cronNextRunAt(job cronJobState, now time.Time) time.Time {
	now = cronSchedulerTime(now)
	switch job.ScheduleType {
	case cronScheduleTypeDaily:
		base := time.Date(now.Year(), now.Month(), now.Day(), job.DailyHour, job.DailyMinute, 0, 0, now.Location())
		if !base.After(now) {
			base = base.Add(24 * time.Hour)
		}
		return base
	case cronScheduleTypeInterval:
		interval := time.Duration(job.IntervalMinutes) * time.Minute
		if interval <= 0 {
			interval = time.Duration(cronIntervalChoices[0].Minutes) * time.Minute
		}
		return now.Add(interval)
	default:
		return time.Time{}
	}
}

func cronAdvanceRunAt(job cronJobState, current, now time.Time) time.Time {
	now = cronSchedulerTime(now)
	if !current.IsZero() {
		current = current.In(now.Location())
	}
	switch job.ScheduleType {
	case cronScheduleTypeDaily:
		if current.IsZero() {
			return cronNextRunAt(job, now)
		}
		next := current.Add(24 * time.Hour)
		for !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		return next
	case cronScheduleTypeInterval:
		interval := time.Duration(job.IntervalMinutes) * time.Minute
		if interval <= 0 {
			interval = time.Duration(cronIntervalChoices[0].Minutes) * time.Minute
		}
		if current.IsZero() {
			current = now
		}
		next := current.Add(interval)
		for !next.After(now) {
			next = next.Add(interval)
		}
		return next
	default:
		return time.Time{}
	}
}

func cronSchedulerTime(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	loc, err := time.LoadLocation(cronTimeZone())
	if err != nil || loc == nil {
		return now.In(time.Local)
	}
	return now.In(loc)
}

func cronInstanceIDForRun(jobRecordID string, triggeredAt time.Time) string {
	suffix := strings.NewReplacer("-", "", ":", "", " ", "", "/", "_", "\\", "_").Replace(triggeredAt.UTC().Format("20060102T150405"))
	jobRecordID = strings.TrimSpace(jobRecordID)
	if len(jobRecordID) > 16 {
		jobRecordID = jobRecordID[:16]
	}
	jobRecordID = strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(jobRecordID)
	return cronInstancePrefix + jobRecordID + "-" + suffix
}

func cronRunSummary(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := text
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = line[:idx]
	}
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

func cronStatusText(status string) string {
	switch strings.TrimSpace(status) {
	case "completed":
		return "成功"
	case "failed":
		return "失败"
	case "skipped":
		return "跳过"
	case "timeout":
		return "超时"
	default:
		return strings.TrimSpace(status)
	}
}

func cronMilliseconds(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().UnixMilli()
}

func cronElapsedSeconds(startedAt, completedAt time.Time) any {
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return nil
	}
	return int(completedAt.Sub(startedAt).Seconds())
}

func cronUpgradeStateInstanceID(stateValue install.InstallState) string {
	return strings.TrimSpace(stateValue.InstanceID)
}
