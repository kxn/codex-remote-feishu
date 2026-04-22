package cronruntime

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	StateSchemaVersion    = 4
	DefaultTimeoutMinute  = 30
	DefaultConcurrencyCap = 1
	ScheduleScanEvery     = time.Second
	ExitGrace             = 20 * time.Second
	BitableBootstrapTTL   = 2 * time.Minute
	BitableWorkspaceTTL   = 90 * time.Second
	BitablePermissionTTL  = 30 * time.Second
	ReloadWorkspaceTTL    = 45 * time.Second
	ReloadTasksTTL        = 90 * time.Second
	WritebackRunsTTL      = 30 * time.Second
	WritebackTasksTTL     = 30 * time.Second
	InstancePrefix        = "inst-cron-"
	RunsTableName         = "运行记录"
	TasksTableName        = "任务配置"
	WorkspacesTableName   = "工作区清单"
	MetaTableName         = "元信息"
)

type StateFile struct {
	SchemaVersion       int           `json:"schema_version"`
	InstanceScopeKey    string        `json:"instance_scope_key,omitempty"`
	InstanceLabel       string        `json:"instance_label,omitempty"`
	GatewayID           string        `json:"gateway_id,omitempty"`
	OwnerGatewayID      string        `json:"owner_gateway_id,omitempty"`
	OwnerAppID          string        `json:"owner_app_id,omitempty"`
	OwnerBoundAt        time.Time     `json:"owner_bound_at,omitempty"`
	Bitable             *BitableState `json:"bitable,omitempty"`
	Jobs                []JobState    `json:"jobs,omitempty"`
	LastWorkspaceSyncAt time.Time     `json:"last_workspace_sync_at,omitempty"`
	LastReloadAt        time.Time     `json:"last_reload_at,omitempty"`
	LastReloadSummary   string        `json:"last_reload_summary,omitempty"`
	UpdatedAt           time.Time     `json:"updated_at,omitempty"`
}

type BitableState struct {
	AppToken     string    `json:"app_token,omitempty"`
	AppURL       string    `json:"app_url,omitempty"`
	TimeZone     string    `json:"time_zone,omitempty"`
	DefaultTable string    `json:"default_table_id,omitempty"`
	MetaRecordID string    `json:"meta_record_id,omitempty"`
	Tables       TableIDs  `json:"tables,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
	LastVerified time.Time `json:"last_verified,omitempty"`
}

type TableIDs struct {
	Tasks      string `json:"tasks,omitempty"`
	Runs       string `json:"runs,omitempty"`
	Workspaces string `json:"workspaces,omitempty"`
	Meta       string `json:"meta,omitempty"`
}

type JobState struct {
	RecordID           string        `json:"record_id,omitempty"`
	Name               string        `json:"name,omitempty"`
	ScheduleType       string        `json:"schedule_type,omitempty"`
	DailyHour          int           `json:"daily_hour,omitempty"`
	DailyMinute        int           `json:"daily_minute,omitempty"`
	IntervalMinutes    int           `json:"interval_minutes,omitempty"`
	SourceType         JobSourceType `json:"source_type,omitempty"`
	WorkspaceKey       string        `json:"workspace_key,omitempty"`
	WorkspaceRecordID  string        `json:"workspace_record_id,omitempty"`
	GitRepoSourceInput string        `json:"git_repo_source_input,omitempty"`
	GitRepoURL         string        `json:"git_repo_url,omitempty"`
	GitRef             string        `json:"git_ref,omitempty"`
	Prompt             string        `json:"prompt,omitempty"`
	MaxConcurrency     int           `json:"max_concurrency,omitempty"`
	TimeoutMinutes     int           `json:"timeout_minutes,omitempty"`
	NextRunAt          time.Time     `json:"next_run_at,omitempty"`
}

type WritebackTarget struct {
	GatewayID string
	Bitable   BitableState
}

type RunState struct {
	RunID            string
	InstanceID       string
	GatewayID        string
	WritebackTarget  WritebackTarget
	JobRecordID      string
	JobName          string
	SourceType       JobSourceType
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
	Buffers          map[string]*ItemBuffer
}

type ItemBuffer struct {
	ItemID   string
	ItemKind string
	Chunks   []string
}

type ExitTarget struct {
	InstanceID        string
	PID               int
	Deadline          time.Time
	StopInFlight      bool
	LastStopAttemptAt time.Time
}

type RuntimeState struct {
	Loaded                 bool
	SyncInFlight           bool
	State                  *StateFile
	Runs                   map[string]*RunState
	JobActiveRuns          map[string]map[string]struct{}
	ExitTargets            map[string]*ExitTarget
	NextScheduleScan       time.Time
	LastStateNormalization time.Time
}

func NewRuntimeState() RuntimeState {
	return RuntimeState{
		Runs:          map[string]*RunState{},
		JobActiveRuns: map[string]map[string]struct{}{},
		ExitTargets:   map[string]*ExitTarget{},
	}
}

func NormalizeState(stateValue StateFile) *StateFile {
	stateValue.SchemaVersion = StateSchemaVersion
	if stateValue.Bitable == nil {
		stateValue.Bitable = &BitableState{}
	}
	if stateValue.Jobs == nil {
		stateValue.Jobs = []JobState{}
	}
	for index := range stateValue.Jobs {
		stateValue.Jobs[index] = NormalizeJobState(stateValue.Jobs[index])
	}
	return &stateValue
}

func CloneState(stateValue *StateFile) *StateFile {
	if stateValue == nil {
		return nil
	}
	cloned := *stateValue
	if stateValue.Bitable != nil {
		bitable := *stateValue.Bitable
		cloned.Bitable = &bitable
	}
	if stateValue.Jobs != nil {
		cloned.Jobs = append([]JobState(nil), stateValue.Jobs...)
	}
	return &cloned
}

func NewState(scopeKey, label string) *StateFile {
	return &StateFile{
		SchemaVersion:    StateSchemaVersion,
		InstanceScopeKey: strings.TrimSpace(scopeKey),
		InstanceLabel:    strings.TrimSpace(label),
		Bitable:          &BitableState{},
		Jobs:             []JobState{},
		UpdatedAt:        time.Now().UTC(),
	}
}

func AppTitle(instanceLabel string) string {
	instanceLabel = strings.TrimSpace(instanceLabel)
	if instanceLabel == "" {
		instanceLabel = "stable"
	}
	return "Codex 定时任务（" + instanceLabel + "）"
}

func FallbackInstanceID(values ...string) string {
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

func DefaultTimeoutMinutes(raw int) int {
	if raw > 0 {
		return raw
	}
	return DefaultTimeoutMinute
}

func DefaultMaxConcurrency(raw int) int {
	if raw > 0 {
		return raw
	}
	return DefaultConcurrencyCap
}

func StateHasBinding(stateValue *StateFile) bool {
	return stateValue != nil && stateValue.Bitable != nil && strings.TrimSpace(stateValue.Bitable.AppToken) != ""
}

func NextRunAt(job JobState, now time.Time) time.Time {
	return NextRunAtIn(job, now, SystemTimeZone())
}

func NextRunAtIn(job JobState, now time.Time, timeZone string) time.Time {
	now = SchedulerTimeIn(now, timeZone)
	switch job.ScheduleType {
	case ScheduleTypeDaily:
		base := time.Date(now.Year(), now.Month(), now.Day(), job.DailyHour, job.DailyMinute, 0, 0, now.Location())
		if !base.After(now) {
			base = base.Add(24 * time.Hour)
		}
		return base
	case ScheduleTypeInterval:
		interval := time.Duration(job.IntervalMinutes) * time.Minute
		if interval <= 0 {
			interval = time.Duration(IntervalChoices[0].Minutes) * time.Minute
		}
		return now.Add(interval)
	default:
		return time.Time{}
	}
}

func AdvanceRunAt(job JobState, current, now time.Time) time.Time {
	return AdvanceRunAtIn(job, current, now, SystemTimeZone())
}

func AdvanceRunAtIn(job JobState, current, now time.Time, timeZone string) time.Time {
	now = SchedulerTimeIn(now, timeZone)
	if !current.IsZero() {
		current = current.In(now.Location())
	}
	switch job.ScheduleType {
	case ScheduleTypeDaily:
		if current.IsZero() {
			return NextRunAt(job, now)
		}
		next := current.Add(24 * time.Hour)
		for !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		return next
	case ScheduleTypeInterval:
		interval := time.Duration(job.IntervalMinutes) * time.Minute
		if interval <= 0 {
			interval = time.Duration(IntervalChoices[0].Minutes) * time.Minute
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

func SchedulerTimeIn(now time.Time, timeZone string) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	return now.In(ResolveLocation(timeZone))
}

func InstanceIDForRun(jobRecordID string, triggeredAt time.Time) string {
	suffix := strings.NewReplacer("-", "", ":", "", " ", "", "/", "_", "\\", "_").Replace(triggeredAt.UTC().Format("20060102T150405"))
	jobRecordID = strings.TrimSpace(jobRecordID)
	if len(jobRecordID) > 16 {
		jobRecordID = jobRecordID[:16]
	}
	jobRecordID = strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(jobRecordID)
	return InstancePrefix + jobRecordID + "-" + suffix
}

func RunSummary(text string) string {
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

func StatusText(status string) string {
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

func Milliseconds(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().UnixMilli()
}

func ConfiguredTimeZone(stateValue *StateFile) string {
	if stateValue != nil && stateValue.Bitable != nil {
		if value := NormalizeTimeZone(stateValue.Bitable.TimeZone); value != "" {
			return value
		}
	}
	return SystemTimeZone()
}

func FormatDisplayTime(value time.Time, timeZone string) string {
	if value.IsZero() {
		return ""
	}
	return SchedulerTimeIn(value, timeZone).Format("2006-01-02 15:04")
}

func ResolveLocation(timeZone string) *time.Location {
	if value := NormalizeTimeZone(timeZone); value != "" {
		if loc, err := time.LoadLocation(value); err == nil && loc != nil {
			return loc
		}
	}
	if time.Local != nil {
		return time.Local
	}
	return time.UTC
}

func SystemTimeZone() string {
	candidates := []string{
		os.Getenv("TZ"),
		ReadTimeZoneFile("/etc/timezone"),
		TimeZoneFromLocaltime("/etc/localtime"),
		time.Now().Location().String(),
	}
	for _, candidate := range candidates {
		if value := NormalizeTimeZone(candidate); value != "" {
			return value
		}
	}
	return "UTC"
}

func NormalizeTimeZone(value string) string {
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

func ReadTimeZoneFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func TimeZoneFromLocaltime(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.TrimSpace(filepath.ToSlash(target)), "/")
	for idx := range parts {
		if parts[idx] != "zoneinfo" {
			continue
		}
		if idx+1 >= len(parts) {
			return ""
		}
		return strings.Join(parts[idx+1:], "/")
	}
	return ""
}

func ElapsedSeconds(startedAt, completedAt time.Time) any {
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return nil
	}
	return int(completedAt.Sub(startedAt).Round(time.Second) / time.Second)
}

func (t WritebackTarget) Valid() bool {
	return strings.TrimSpace(t.GatewayID) != "" &&
		strings.TrimSpace(t.Bitable.AppToken) != "" &&
		strings.TrimSpace(t.Bitable.Tables.Runs) != "" &&
		strings.TrimSpace(t.Bitable.Tables.Tasks) != ""
}

func ItemBufferKey(itemID string) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "__default__"
	}
	return itemID
}

func EnsureItemBuffer(run *RunState, itemID, itemKind string) *ItemBuffer {
	if run == nil {
		return &ItemBuffer{}
	}
	if run.Buffers == nil {
		run.Buffers = map[string]*ItemBuffer{}
	}
	key := ItemBufferKey(itemID)
	if existing := run.Buffers[key]; existing != nil {
		if existing.ItemKind == "" {
			existing.ItemKind = itemKind
		}
		return existing
	}
	buf := &ItemBuffer{
		ItemID:   strings.TrimSpace(itemID),
		ItemKind: strings.TrimSpace(itemKind),
	}
	run.Buffers[key] = buf
	return buf
}

func JobActiveKey(jobRecordID, jobName string) string {
	jobRecordID = strings.TrimSpace(jobRecordID)
	if jobRecordID != "" {
		return jobRecordID
	}
	jobName = strings.TrimSpace(jobName)
	if jobName != "" {
		return "name:" + jobName
	}
	return ""
}
