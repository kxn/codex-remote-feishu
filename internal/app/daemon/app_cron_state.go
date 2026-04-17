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
	cronStateSchemaVersion    = 4
	cronDefaultTimeoutMinute  = 30
	cronDefaultConcurrencyCap = 1
	cronScheduleScanEvery     = time.Second
	cronExitGrace             = 20 * time.Second
	cronBitableBootstrapTTL   = 2 * time.Minute
	cronBitableWorkspaceTTL   = 90 * time.Second
	cronBitablePermissionTTL  = 30 * time.Second
	cronReloadWorkspaceTTL    = 45 * time.Second
	cronReloadTasksTTL        = 90 * time.Second
	cronWritebackRunsTTL      = 30 * time.Second
	cronWritebackTasksTTL     = 30 * time.Second
	cronInstancePrefix        = "inst-cron-"
	cronRunsTableName         = "运行记录"
	cronTasksTableName        = "任务配置"
	cronWorkspacesTableName   = "工作区清单"
	cronMetaTableName         = "元信息"
)

type cronStateFile struct {
	SchemaVersion       int               `json:"schema_version"`
	InstanceScopeKey    string            `json:"instance_scope_key,omitempty"`
	InstanceLabel       string            `json:"instance_label,omitempty"`
	GatewayID           string            `json:"gateway_id,omitempty"`
	OwnerGatewayID      string            `json:"owner_gateway_id,omitempty"`
	OwnerAppID          string            `json:"owner_app_id,omitempty"`
	OwnerBoundAt        time.Time         `json:"owner_bound_at,omitempty"`
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
	TimeZone     string       `json:"time_zone,omitempty"`
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
	RecordID           string            `json:"record_id,omitempty"`
	Name               string            `json:"name,omitempty"`
	ScheduleType       string            `json:"schedule_type,omitempty"`
	DailyHour          int               `json:"daily_hour,omitempty"`
	DailyMinute        int               `json:"daily_minute,omitempty"`
	IntervalMinutes    int               `json:"interval_minutes,omitempty"`
	SourceType         cronJobSourceType `json:"source_type,omitempty"`
	WorkspaceKey       string            `json:"workspace_key,omitempty"`
	WorkspaceRecordID  string            `json:"workspace_record_id,omitempty"`
	GitRepoSourceInput string            `json:"git_repo_source_input,omitempty"`
	GitRepoURL         string            `json:"git_repo_url,omitempty"`
	GitRef             string            `json:"git_ref,omitempty"`
	Prompt             string            `json:"prompt,omitempty"`
	MaxConcurrency     int               `json:"max_concurrency,omitempty"`
	TimeoutMinutes     int               `json:"timeout_minutes,omitempty"`
	NextRunAt          time.Time         `json:"next_run_at,omitempty"`
}

type cronRunState struct {
	RunID            string
	InstanceID       string
	GatewayID        string
	WritebackTarget  cronWritebackTarget
	JobRecordID      string
	JobName          string
	SourceType       cronJobSourceType
	SourceLabel      string
	WorkspaceKey     string
	RunRoot          string
	RunDirectory     string
	GitSourceKey     string
	GitRepoURL       string
	GitRef           string
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
	InstanceID        string
	PID               int
	Deadline          time.Time
	StopInFlight      bool
	LastStopAttemptAt time.Time
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
	path := a.cronStatePath()
	a.cronStateIOMu.Lock()
	a.mu.Unlock()
	raw, err := os.ReadFile(path)
	a.cronStateIOMu.Unlock()
	a.mu.Lock()
	if a.cronLoaded {
		if a.cronState != nil {
			a.cronState = normalizeCronState(*a.cronState)
		}
		if a.cronState == nil && create {
			stateValue, createErr := a.newCronStateLocked()
			if createErr != nil {
				return nil, createErr
			}
			a.cronState = stateValue
			if err := a.writeCronStateLocked(); err != nil {
				return nil, err
			}
		}
		return a.cronState, nil
	}
	switch {
	case os.IsNotExist(err):
		a.cronLoaded = true
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
	a.cronLoaded = true
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
	for index := range stateValue.Jobs {
		stateValue.Jobs[index] = cronNormalizeJobState(stateValue.Jobs[index])
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
	updatedAt := time.Now().UTC()
	a.cronState.SchemaVersion = cronStateSchemaVersion
	a.cronState.UpdatedAt = updatedAt
	snapshot := cloneCronState(a.cronState)
	if snapshot == nil {
		return nil
	}
	path := a.cronStatePath()
	a.cronStateIOMu.Lock()
	a.mu.Unlock()
	err := writeJSONFileAtomic(path, snapshot, 0o600)
	a.cronStateIOMu.Unlock()
	a.mu.Lock()
	return err
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
	return cronSystemTimeZone()
}

func cronDefaultTimeoutMinutes(raw int) int {
	if raw > 0 {
		return raw
	}
	return cronDefaultTimeoutMinute
}

func cronDefaultMaxConcurrency(raw int) int {
	if raw > 0 {
		return raw
	}
	return cronDefaultConcurrencyCap
}

func cronStateHasBinding(stateValue *cronStateFile) bool {
	return stateValue != nil && stateValue.Bitable != nil && strings.TrimSpace(stateValue.Bitable.AppToken) != ""
}

func cronNextRunAt(job cronJobState, now time.Time) time.Time {
	return cronNextRunAtIn(job, now, cronSystemTimeZone())
}

func cronNextRunAtIn(job cronJobState, now time.Time, timeZone string) time.Time {
	now = cronSchedulerTimeIn(now, timeZone)
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
	return cronAdvanceRunAtIn(job, current, now, cronSystemTimeZone())
}

func cronAdvanceRunAtIn(job cronJobState, current, now time.Time, timeZone string) time.Time {
	now = cronSchedulerTimeIn(now, timeZone)
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
	return cronSchedulerTimeIn(now, cronSystemTimeZone())
}

func cronSchedulerTimeIn(now time.Time, timeZone string) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return now.In(cronResolveLocation(timeZone))
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

func cronConfiguredTimeZone(stateValue *cronStateFile) string {
	if stateValue != nil && stateValue.Bitable != nil {
		if value := cronNormalizeTimeZone(stateValue.Bitable.TimeZone); value != "" {
			return value
		}
	}
	return cronSystemTimeZone()
}

func cronFormatDisplayTime(value time.Time, timeZone string) string {
	if value.IsZero() {
		return ""
	}
	return cronSchedulerTimeIn(value, timeZone).Format("2006-01-02 15:04")
}

func cronResolveLocation(timeZone string) *time.Location {
	if value := cronNormalizeTimeZone(timeZone); value != "" {
		if loc, err := time.LoadLocation(value); err == nil && loc != nil {
			return loc
		}
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

func cronSystemTimeZone() string {
	candidates := []string{
		os.Getenv("TZ"),
		cronReadTimeZoneFile("/etc/timezone"),
		cronTimeZoneFromLocaltime("/etc/localtime"),
		time.Now().Location().String(),
	}
	for _, candidate := range candidates {
		if value := cronNormalizeTimeZone(candidate); value != "" {
			return value
		}
	}
	return "UTC"
}

func cronNormalizeTimeZone(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, ":"))
	switch strings.ToLower(value) {
	case "", "local":
		return ""
	}
	if loc, err := time.LoadLocation(value); err == nil && loc != nil {
		return loc.String()
	}
	return ""
}

func cronReadTimeZoneFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func cronTimeZoneFromLocaltime(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	normalized := filepath.ToSlash(strings.TrimSpace(target))
	if normalized == "" {
		return ""
	}
	const marker = "/zoneinfo/"
	if idx := strings.Index(normalized, marker); idx >= 0 {
		return strings.TrimPrefix(normalized[idx+len(marker):], "/")
	}
	return ""
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
