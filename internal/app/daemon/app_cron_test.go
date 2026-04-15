package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type fakeCronBitableAPI struct {
	mu            sync.Mutex
	createRecords []fakeCronRecordWrite
	updateRecords []fakeCronRecordWrite
	grantCalls    []fakeCronPermissionGrant
	permissions   map[string]feishu.BitablePermissionMember
}

type fakeCronRecordWrite struct {
	AppToken string
	TableID  string
	RecordID string
	Fields   map[string]any
}

type fakeCronPermissionGrant struct {
	Token         string
	DocType       string
	MemberType    string
	MemberID      string
	PrincipalType string
	Perm          string
	PermType      string
}

func (f *fakeCronBitableAPI) GetApp(context.Context, string) (*larkbitable.App, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) CreateApp(context.Context, string, string) (*larkbitable.App, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) ListTables(context.Context, string) ([]*larkbitable.AppTable, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) CreateTable(context.Context, string, *larkbitable.ReqTable) (*larkbitable.AppTable, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) RenameTable(context.Context, string, string, string) error {
	return nil
}

func (f *fakeCronBitableAPI) ListFields(context.Context, string, string) ([]*larkbitable.AppTableField, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) CreateField(context.Context, string, string, *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) UpdateField(context.Context, string, string, string, *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) ListRecords(context.Context, string, string, []string) ([]*larkbitable.AppTableRecord, error) {
	return nil, nil
}

func (f *fakeCronBitableAPI) CreateRecord(_ context.Context, appToken, tableID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createRecords = append(f.createRecords, fakeCronRecordWrite{
		AppToken: appToken,
		TableID:  tableID,
		Fields:   cloneAnyMap(fields),
	})
	return &larkbitable.AppTableRecord{RecordId: stringPtr("rec-created")}, nil
}

func (f *fakeCronBitableAPI) UpdateRecord(_ context.Context, appToken, tableID, recordID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateRecords = append(f.updateRecords, fakeCronRecordWrite{
		AppToken: appToken,
		TableID:  tableID,
		RecordID: recordID,
		Fields:   cloneAnyMap(fields),
	})
	return &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)}, nil
}

func (f *fakeCronBitableAPI) BatchCreateRecords(_ context.Context, appToken, tableID string, values []map[string]any) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	base := len(f.createRecords)
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for i, fields := range values {
		recordID := fmt.Sprintf("rec-created-%d", base+i)
		f.createRecords = append(f.createRecords, fakeCronRecordWrite{
			AppToken: appToken,
			TableID:  tableID,
			RecordID: recordID,
			Fields:   cloneAnyMap(fields),
		})
		records = append(records, &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)})
	}
	return records, nil
}

func (f *fakeCronBitableAPI) BatchUpdateRecords(_ context.Context, appToken, tableID string, values []feishu.BitableRecordUpdate) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for _, update := range values {
		recordID := strings.TrimSpace(update.RecordID)
		f.updateRecords = append(f.updateRecords, fakeCronRecordWrite{
			AppToken: appToken,
			TableID:  tableID,
			RecordID: recordID,
			Fields:   cloneAnyMap(update.Fields),
		})
		records = append(records, &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)})
	}
	return records, nil
}

func (f *fakeCronBitableAPI) ListPermissionMembers(context.Context, string, string) (map[string]feishu.BitablePermissionMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.permissions) == 0 {
		return map[string]feishu.BitablePermissionMember{}, nil
	}
	values := make(map[string]feishu.BitablePermissionMember, len(f.permissions))
	for key, member := range f.permissions {
		values[key] = member
	}
	return values, nil
}

func (f *fakeCronBitableAPI) GrantPermission(_ context.Context, token, docType, memberType, memberID, principalType, perm string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grantCalls = append(f.grantCalls, fakeCronPermissionGrant{
		Token:         token,
		DocType:       docType,
		MemberType:    memberType,
		MemberID:      memberID,
		PrincipalType: principalType,
		Perm:          perm,
	})
	if f.permissions == nil {
		f.permissions = map[string]feishu.BitablePermissionMember{}
	}
	f.permissions[memberType+":"+memberID] = feishu.BitablePermissionMember{Perm: perm, PermType: "container"}
	return nil
}

func (f *fakeCronBitableAPI) UpdatePermission(_ context.Context, token, docType, memberType, memberID, principalType, perm, permType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grantCalls = append(f.grantCalls, fakeCronPermissionGrant{
		Token:         token,
		DocType:       docType,
		MemberType:    memberType,
		MemberID:      memberID,
		PrincipalType: principalType,
		Perm:          perm,
		PermType:      permType,
	})
	if f.permissions == nil {
		f.permissions = map[string]feishu.BitablePermissionMember{}
	}
	f.permissions[memberType+":"+memberID] = feishu.BitablePermissionMember{Perm: perm, PermType: permType}
	return nil
}

func (f *fakeCronBitableAPI) waitForWrites(t *testing.T, wantCreates, wantUpdates int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		gotCreates := len(f.createRecords)
		gotUpdates := len(f.updateRecords)
		f.mu.Unlock()
		if gotCreates >= wantCreates && gotUpdates >= wantUpdates {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t.Fatalf("timed out waiting for writes: got creates=%d updates=%d want creates=%d updates=%d", len(f.createRecords), len(f.updateRecords), wantCreates, wantUpdates)
}

func TestCronSchedulerLaunchesFreshHiddenRun(t *testing.T) {
	workspace := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID: "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronScheduleTypeInterval,
			IntervalMinutes: 5,
			WorkspaceKey:    workspace,
			Prompt:          "check CI",
			TimeoutMinutes:  15,
			NextRunAt:       time.Now().Add(-time.Minute),
		}},
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: app.headlessRuntime.Paths.StateDir,
		},
	})
	var launches int
	var capturedEnv []string
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches++
		capturedEnv = append([]string(nil), opts.Env...)
		return 4321, nil
	}

	app.maybeScheduleCronJobsLocked(time.Now())

	if launches != 1 {
		t.Fatalf("launches = %d, want 1", launches)
	}
	if !containsEnvEntry(capturedEnv, "CODEX_REMOTE_INSTANCE_SOURCE=headless") {
		t.Fatalf("expected headless source env, got %#v", capturedEnv)
	}
	if !containsEnvEntry(capturedEnv, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected daemon-owned lifetime env, got %#v", capturedEnv)
	}
	if containsEnvEntry(capturedEnv, "CODEX_REMOTE_INSTANCE_MANAGED=1") {
		t.Fatalf("cron run must not mark managed headless env, got %#v", capturedEnv)
	}
	if len(app.cronRuns) != 1 {
		t.Fatalf("cronRuns = %#v, want one active run", app.cronRuns)
	}
	active := app.cronJobActiveRuns[cronJobActiveKey("rec-task-1", "Nightly")]
	if !strings.HasPrefix(active, cronInstancePrefix) {
		t.Fatalf("active cron instance id = %q, want prefix %q", active, cronInstancePrefix)
	}
	if app.cronState.Jobs[0].NextRunAt.IsZero() || !app.cronState.Jobs[0].NextRunAt.After(time.Now()) {
		t.Fatalf("next run was not advanced: %#v", app.cronState.Jobs[0].NextRunAt)
	}
}

func TestCronHelloAndCompletionStayHiddenAndWriteBackFinalMessage(t *testing.T) {
	workspace := t.TempDir()
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID: "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	instanceID := cronInstanceIDForRun("rec-task-1", time.Now())
	app.cronRuns[instanceID] = &cronRunState{
		RunID:          instanceID,
		InstanceID:     instanceID,
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   workspace,
		Prompt:         "check CI",
		TimeoutMinutes: 15,
		TriggeredAt:    time.Now().UTC(),
		PID:            4321,
	}
	app.cronJobActiveRuns[cronJobActiveKey("rec-task-1", "Nightly")] = instanceID

	var commands []agentproto.Command
	app.sendAgentCommand = func(target string, command agentproto.Command) error {
		if target != instanceID {
			t.Fatalf("unexpected target = %q", target)
		}
		commands = append(commands, command)
		return nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID: instanceID,
			Source:     "headless",
			PID:        4321,
		},
	})

	if len(commands) != 1 || commands[0].Kind != agentproto.CommandPromptSend {
		t.Fatalf("expected one prompt.send command, got %#v", commands)
	}
	if !commands[0].Target.CreateThreadIfMissing || !commands[0].Target.InternalHelper {
		t.Fatalf("expected hidden prompt target flags, got %#v", commands[0].Target)
	}
	if app.service.Instance(instanceID) != nil {
		t.Fatalf("cron instance must stay hidden from normal service instances")
	}

	app.onEvents(context.Background(), instanceID, []agentproto.Event{
		{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1"},
		{Kind: agentproto.EventItemDelta, ThreadID: "thread-1", TurnID: "turn-1", ItemID: "item-1", ItemKind: "agent_message", Delta: "done"},
		{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed"},
	})

	if len(commands) != 2 || commands[1].Kind != agentproto.CommandProcessExit {
		t.Fatalf("expected prompt.send then process.exit, got %#v", commands)
	}
	if _, ok := app.cronRuns[instanceID]; ok {
		t.Fatalf("completed cron run should be removed from active map")
	}
	if _, ok := app.cronExitTargets[instanceID]; !ok {
		t.Fatalf("completed cron run should queue exit target")
	}

	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := api.createRecords[0].Fields["最终回复"]; got != "done" {
		t.Fatalf("final message = %#v, want %q", got, "done")
	}
	if got := api.createRecords[0].Fields["状态"]; got != "成功" {
		t.Fatalf("history status = %#v, want 成功", got)
	}
	if api.updateRecords[0].RecordID != "rec-task-1" {
		t.Fatalf("updated record id = %q, want rec-task-1", api.updateRecords[0].RecordID)
	}
	if got := api.updateRecords[0].Fields["最近结果摘要"]; got != "done" {
		t.Fatalf("summary = %#v, want %q", got, "done")
	}
}

func TestCronSchedulerSkipsWhenPreviousRunIsStillActive(t *testing.T) {
	workspace := t.TempDir()
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID: "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronJobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronScheduleTypeInterval,
			IntervalMinutes: 5,
			WorkspaceKey:    workspace,
			Prompt:          "check CI",
			TimeoutMinutes:  15,
			NextRunAt:       time.Now().Add(-time.Minute),
		}},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	app.cronRuns["inst-running"] = &cronRunState{
		InstanceID:     "inst-running",
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   workspace,
		TriggeredAt:    time.Now().Add(-2 * time.Minute),
		TimeoutMinutes: 15,
	}
	app.cronJobActiveRuns[cronJobActiveKey("rec-task-1", "Nightly")] = "inst-running"
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatalf("scheduler must not launch another hidden run while previous run is active")
		return 0, nil
	}

	app.maybeScheduleCronJobsLocked(time.Now())

	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := api.createRecords[0].Fields["状态"]; got != "跳过" {
		t.Fatalf("history status = %#v, want 跳过", got)
	}
	if got := api.updateRecords[0].Fields["最近错误"]; !strings.Contains(got.(string), "跳过") {
		t.Fatalf("skip reason = %#v, want contains 跳过", got)
	}
}

func TestCronSchedulerTimesOutRunAndRequestsExit(t *testing.T) {
	workspace := t.TempDir()
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		GatewayID: "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-1",
			Tables: cronTableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	instanceID := cronInstanceIDForRun("rec-task-1", time.Now().Add(-time.Hour))
	app.cronRuns[instanceID] = &cronRunState{
		InstanceID:     instanceID,
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   workspace,
		TriggeredAt:    time.Now().Add(-40 * time.Minute),
		StartedAt:      time.Now().Add(-35 * time.Minute),
		TimeoutMinutes: 30,
		PID:            9876,
	}
	app.cronJobActiveRuns[cronJobActiveKey("rec-task-1", "Nightly")] = instanceID
	var commands []agentproto.Command
	app.sendAgentCommand = func(target string, command agentproto.Command) error {
		if target != instanceID {
			t.Fatalf("unexpected target = %q", target)
		}
		commands = append(commands, command)
		return nil
	}

	app.maybeScheduleCronJobsLocked(time.Now())

	if len(commands) != 1 || commands[0].Kind != agentproto.CommandProcessExit {
		t.Fatalf("expected one process.exit command, got %#v", commands)
	}
	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := api.createRecords[0].Fields["状态"]; got != "超时" {
		t.Fatalf("history status = %#v, want 超时", got)
	}
	if _, ok := app.cronRuns[instanceID]; ok {
		t.Fatalf("timed-out cron run should be removed from active map")
	}
}

func TestCronJobFromRecordParsesLinkedWorkspaceValues(t *testing.T) {
	now := time.Now()
	record := &larkbitable.AppTableRecord{
		RecordId: stringPtr("rec-task-1"),
		Fields: map[string]any{
			"任务名":  "Nightly",
			"启用":   true,
			"调度类型": cronScheduleTypeInterval,
			"间隔":   "15分钟",
			"工作区": []any{
				map[string]any{
					"record_ids": []any{"rec-workspace-1"},
				},
			},
			"提示词":    "check CI",
			"超时（分钟）": "20",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronWorkspaceRow{
		"rec-workspace-1": {Key: "/tmp/project", Name: "project", Status: "可用"},
	}, now)
	if err != nil {
		t.Fatalf("cronJobFromRecord: %v", err)
	}
	if skip {
		t.Fatalf("expected enabled job, got skip")
	}
	if job.WorkspaceRecordID != "rec-workspace-1" || job.WorkspaceKey != "/tmp/project" {
		t.Fatalf("unexpected workspace mapping: %#v", job)
	}
	if job.IntervalMinutes != 15 || job.TimeoutMinutes != 20 {
		t.Fatalf("unexpected parsed job timing: %#v", job)
	}
}

func TestCronJobFromRecordParsesDailyClockField(t *testing.T) {
	now := time.Now()
	record := &larkbitable.AppTableRecord{
		RecordId: stringPtr("rec-task-daily"),
		Fields: map[string]any{
			"任务名":  "Morning",
			"启用":   true,
			"调度类型": cronScheduleTypeDaily,
			"调度时间": "09:30",
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "daily check",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronWorkspaceRow{
		"rec-workspace-1": {Key: "/tmp/project", Name: "project", Status: "可用"},
	}, now)
	if err != nil {
		t.Fatalf("cronJobFromRecord daily clock: %v", err)
	}
	if skip {
		t.Fatalf("expected enabled daily job, got skip")
	}
	if job.DailyHour != 9 || job.DailyMinute != 30 {
		t.Fatalf("unexpected daily clock parse: %#v", job)
	}
}

func TestCronJobFromRecordSupportsLegacyDailyHourMinuteFields(t *testing.T) {
	now := time.Now()
	record := &larkbitable.AppTableRecord{
		RecordId: stringPtr("rec-task-daily-legacy"),
		Fields: map[string]any{
			"任务名":  "Legacy Daily",
			"启用":   true,
			"调度类型": cronScheduleTypeDaily,
			"每天-时": 7,
			"每天-分": 5,
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "daily check",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronWorkspaceRow{
		"rec-workspace-1": {Key: "/tmp/project", Name: "project", Status: "可用"},
	}, now)
	if err != nil {
		t.Fatalf("cronJobFromRecord legacy daily fields: %v", err)
	}
	if skip {
		t.Fatalf("expected enabled legacy daily job, got skip")
	}
	if job.DailyHour != 7 || job.DailyMinute != 5 {
		t.Fatalf("unexpected legacy daily clock parse: %#v", job)
	}
}

func TestCronJobFromRecordSupportsLegacySelectEnabledValue(t *testing.T) {
	now := time.Now()
	record := &larkbitable.AppTableRecord{
		RecordId: stringPtr("rec-task-legacy"),
		Fields: map[string]any{
			"任务名":  "Legacy",
			"启用":   "启用",
			"调度类型": cronScheduleTypeInterval,
			"间隔":   "10分钟",
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "check CI",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronWorkspaceRow{
		"rec-workspace-1": {Key: "/tmp/project", Name: "project", Status: "可用"},
	}, now)
	if err != nil {
		t.Fatalf("cronJobFromRecord legacy select: %v", err)
	}
	if skip {
		t.Fatalf("expected legacy enabled job, got skip")
	}
	if job.IntervalMinutes != 10 {
		t.Fatalf("unexpected interval from legacy select field: %#v", job)
	}
}

func TestEnsureCronUserPermissionGrantsEditAccess(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	api := &fakeCronBitableAPI{}

	if err := app.ensureCronUserPermission(context.Background(), api, "app-cron", "ou_7588194bf7ffe98ef2845026aa398169"); err != nil {
		t.Fatalf("ensureCronUserPermission: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.grantCalls) != 1 {
		t.Fatalf("grantCalls = %d, want 1", len(api.grantCalls))
	}
	grant := api.grantCalls[0]
	if grant.MemberType != "openid" || grant.PrincipalType != "user" {
		t.Fatalf("unexpected principal mapping: %#v", grant)
	}
	if grant.Perm != cronBitablePermissionPermEdit {
		t.Fatalf("perm = %q, want %q", grant.Perm, cronBitablePermissionPermEdit)
	}
}

func TestEnsureCronUserPermissionUpgradesViewAccessToEdit(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	api := &fakeCronBitableAPI{
		permissions: map[string]feishu.BitablePermissionMember{
			"openid:ou_7588194bf7ffe98ef2845026aa398169": {Perm: "view", PermType: "container"},
		},
	}

	if err := app.ensureCronUserPermission(context.Background(), api, "app-cron", "ou_7588194bf7ffe98ef2845026aa398169"); err != nil {
		t.Fatalf("ensureCronUserPermission upgrade: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.grantCalls) != 1 {
		t.Fatalf("grantCalls = %d, want 1", len(api.grantCalls))
	}
	grant := api.grantCalls[0]
	if grant.Perm != cronBitablePermissionPermEdit || grant.PermType != "container" {
		t.Fatalf("unexpected upgraded permission: %#v", grant)
	}
}

func cloneAnyMap(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]any, len(fields))
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func stringPtr(value string) *string {
	return &value
}

type flakyCronBootstrapBitableAPI struct {
	mu               sync.Mutex
	createAppCalls   int
	getAppCalls      int
	createTableCalls int
	failCreateField  bool
	tables           map[string]*larkbitable.AppTable
	fieldsByTable    map[string][]*larkbitable.AppTableField
	grantCalls       []fakeCronPermissionGrant
}

func newFlakyCronBootstrapBitableAPI() *flakyCronBootstrapBitableAPI {
	return &flakyCronBootstrapBitableAPI{
		failCreateField: true,
		tables: map[string]*larkbitable.AppTable{
			"tbl-default": {
				TableId: stringPtr("tbl-default"),
				Name:    stringPtr("未命名表格"),
			},
		},
		fieldsByTable: map[string][]*larkbitable.AppTableField{
			"tbl-default": {{
				FieldId:   stringPtr("fld-default-primary"),
				FieldName: stringPtr("默认列"),
				Type:      intPtr(1),
				IsPrimary: boolPtr(true),
			}},
		},
	}
}

func (f *flakyCronBootstrapBitableAPI) GetApp(context.Context, string) (*larkbitable.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getAppCalls++
	return &larkbitable.App{
		AppToken: stringPtr("app-cron"),
		Url:      stringPtr("https://example.feishu.cn/base/app-cron"),
	}, nil
}

func (f *flakyCronBootstrapBitableAPI) CreateApp(context.Context, string, string) (*larkbitable.App, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createAppCalls++
	return &larkbitable.App{
		AppToken:       stringPtr("app-cron"),
		Url:            stringPtr("https://example.feishu.cn/base/app-cron"),
		DefaultTableId: stringPtr("tbl-default"),
	}, nil
}

func (f *flakyCronBootstrapBitableAPI) ListTables(context.Context, string) ([]*larkbitable.AppTable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	values := make([]*larkbitable.AppTable, 0, len(f.tables))
	for _, table := range f.tables {
		cloned := *table
		values = append(values, &cloned)
	}
	return values, nil
}

func (f *flakyCronBootstrapBitableAPI) CreateTable(_ context.Context, _ string, table *larkbitable.ReqTable) (*larkbitable.AppTable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createTableCalls++
	tableID := "tbl-created-" + strings.ReplaceAll(strings.TrimSpace(stringValue(table.Name)), " ", "-")
	created := &larkbitable.AppTable{
		TableId: stringPtr(tableID),
		Name:    table.Name,
	}
	f.tables[tableID] = created
	primaryName := "主列"
	if len(table.Fields) > 0 && table.Fields[0] != nil && strings.TrimSpace(stringValue(table.Fields[0].FieldName)) != "" {
		primaryName = strings.TrimSpace(stringValue(table.Fields[0].FieldName))
	}
	f.fieldsByTable[tableID] = []*larkbitable.AppTableField{{
		FieldId:   stringPtr("fld-" + tableID + "-primary"),
		FieldName: stringPtr(primaryName),
		Type:      intPtr(1),
		IsPrimary: boolPtr(true),
	}}
	return created, nil
}

func (f *flakyCronBootstrapBitableAPI) RenameTable(_ context.Context, _ string, tableID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if table := f.tables[tableID]; table != nil {
		table.Name = stringPtr(name)
	}
	return nil
}

func (f *flakyCronBootstrapBitableAPI) ListFields(_ context.Context, _ string, tableID string) ([]*larkbitable.AppTableField, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fields := f.fieldsByTable[tableID]
	values := make([]*larkbitable.AppTableField, 0, len(fields))
	for _, field := range fields {
		cloned := *field
		values = append(values, &cloned)
	}
	return values, nil
}

func (f *flakyCronBootstrapBitableAPI) CreateField(_ context.Context, _ string, tableID string, field *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreateField {
		return nil, context.DeadlineExceeded
	}
	cloned := &larkbitable.AppTableField{
		FieldId:   stringPtr("fld-" + tableID + "-" + strings.TrimSpace(stringValue(field.FieldName))),
		FieldName: field.FieldName,
		Type:      field.Type,
		Property:  field.Property,
		IsPrimary: boolPtr(false),
	}
	f.fieldsByTable[tableID] = append(f.fieldsByTable[tableID], cloned)
	return cloned, nil
}

func (f *flakyCronBootstrapBitableAPI) UpdateField(_ context.Context, _ string, tableID, fieldID string, field *larkbitable.AppTableField) (*larkbitable.AppTableField, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.fieldsByTable[tableID] {
		if strings.TrimSpace(stringValue(existing.FieldId)) != fieldID {
			continue
		}
		existing.FieldName = field.FieldName
		existing.Type = field.Type
		existing.Property = field.Property
		cloned := *existing
		return &cloned, nil
	}
	return nil, errors.New("field not found")
}

func (f *flakyCronBootstrapBitableAPI) ListRecords(context.Context, string, string, []string) ([]*larkbitable.AppTableRecord, error) {
	return nil, nil
}

func (f *flakyCronBootstrapBitableAPI) CreateRecord(context.Context, string, string, map[string]any) (*larkbitable.AppTableRecord, error) {
	return &larkbitable.AppTableRecord{RecordId: stringPtr("rec-meta")}, nil
}

func (f *flakyCronBootstrapBitableAPI) UpdateRecord(context.Context, string, string, string, map[string]any) (*larkbitable.AppTableRecord, error) {
	return &larkbitable.AppTableRecord{RecordId: stringPtr("rec-meta")}, nil
}

func (f *flakyCronBootstrapBitableAPI) BatchCreateRecords(_ context.Context, _ string, _ string, values []map[string]any) ([]*larkbitable.AppTableRecord, error) {
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for range values {
		records = append(records, &larkbitable.AppTableRecord{RecordId: stringPtr("rec-meta")})
	}
	return records, nil
}

func (f *flakyCronBootstrapBitableAPI) BatchUpdateRecords(_ context.Context, _ string, _ string, values []feishu.BitableRecordUpdate) ([]*larkbitable.AppTableRecord, error) {
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for _, update := range values {
		recordID := strings.TrimSpace(update.RecordID)
		if recordID == "" {
			recordID = "rec-meta"
		}
		records = append(records, &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)})
	}
	return records, nil
}

func (f *flakyCronBootstrapBitableAPI) ListPermissionMembers(context.Context, string, string) (map[string]feishu.BitablePermissionMember, error) {
	return map[string]feishu.BitablePermissionMember{}, nil
}

func (f *flakyCronBootstrapBitableAPI) GrantPermission(_ context.Context, token, docType, memberType, memberID, principalType, perm string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grantCalls = append(f.grantCalls, fakeCronPermissionGrant{
		Token:         token,
		DocType:       docType,
		MemberType:    memberType,
		MemberID:      memberID,
		PrincipalType: principalType,
		Perm:          perm,
	})
	return nil
}

func (f *flakyCronBootstrapBitableAPI) UpdatePermission(_ context.Context, token, docType, memberType, memberID, principalType, perm, permType string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grantCalls = append(f.grantCalls, fakeCronPermissionGrant{
		Token:         token,
		DocType:       docType,
		MemberType:    memberType,
		MemberID:      memberID,
		PrincipalType: principalType,
		Perm:          perm,
		PermType:      permType,
	})
	return nil
}

func intPtr(value int) *int {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func TestEnsureCronBitablePersistsProgressAndReusesRemoteObjectsAfterTimeout(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable:          &cronBitableState{},
		Jobs:             []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	_, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ensureCronBitable first error = %v, want context deadline exceeded", err)
	}
	if app.cronState == nil || app.cronState.Bitable == nil {
		t.Fatalf("cron state = %#v, want persisted bitable binding", app.cronState)
	}
	if app.cronState.Bitable.AppToken != "app-cron" {
		t.Fatalf("app token = %q, want app-cron", app.cronState.Bitable.AppToken)
	}
	if app.cronState.Bitable.DefaultTable != "tbl-default" {
		t.Fatalf("default table = %q, want tbl-default", app.cronState.Bitable.DefaultTable)
	}
	if app.cronState.Bitable.Tables.Tasks == "" || app.cronState.Bitable.Tables.Workspaces == "" || app.cronState.Bitable.Tables.Runs == "" || app.cronState.Bitable.Tables.Meta == "" {
		t.Fatalf("table binding not persisted after timeout: %#v", app.cronState.Bitable.Tables)
	}
	if api.createAppCalls != 1 {
		t.Fatalf("createAppCalls after first attempt = %d, want 1", api.createAppCalls)
	}
	if api.createTableCalls != 4 {
		t.Fatalf("createTableCalls after first attempt = %d, want 4 with a fresh tasks table", api.createTableCalls)
	}

	api.mu.Lock()
	api.failCreateField = false
	api.mu.Unlock()

	_, _, err = app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"})
	if err != nil {
		t.Fatalf("ensureCronBitable second attempt: %v", err)
	}
	if api.createAppCalls != 1 {
		t.Fatalf("createAppCalls after retry = %d, want still 1", api.createAppCalls)
	}
	if api.getAppCalls == 0 {
		t.Fatalf("expected retry to reuse existing app token via GetApp")
	}
	if api.createTableCalls != 4 {
		t.Fatalf("createTableCalls after retry = %d, want still 4", api.createTableCalls)
	}
	if app.cronState.Bitable.LastVerified.IsZero() {
		t.Fatalf("expected successful retry to verify binding, got %#v", app.cronState.Bitable)
	}
}

func TestEnsureCronBitableRecoversLegacyPartialStateWithFreshTasksTable(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable: &cronBitableState{
			AppToken: "app-cron",
		},
		Jobs: []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	_, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"})
	if err != nil {
		t.Fatalf("ensureCronBitable with legacy partial state: %v", err)
	}
	if api.createAppCalls != 0 {
		t.Fatalf("createAppCalls = %d, want 0 for persisted app token", api.createAppCalls)
	}
	if api.createTableCalls != 4 {
		t.Fatalf("createTableCalls = %d, want 4 when tasks table is created fresh", api.createTableCalls)
	}
	if got := app.cronState.Bitable.Tables.Tasks; got == "tbl-default" {
		t.Fatalf("tasks table = %q, want a fresh table instead of the auto-created default table", got)
	}
}

func TestEnsureCronBitableDoesNotLeakDefaultTemplateColumnsIntoTasksTable(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false
	api.fieldsByTable["tbl-default"] = append(api.fieldsByTable["tbl-default"],
		&larkbitable.AppTableField{FieldId: stringPtr("fld-default-single"), FieldName: stringPtr("单选"), Type: intPtr(3), IsPrimary: boolPtr(false)},
		&larkbitable.AppTableField{FieldId: stringPtr("fld-default-date"), FieldName: stringPtr("日期"), Type: intPtr(5), IsPrimary: boolPtr(false)},
		&larkbitable.AppTableField{FieldId: stringPtr("fld-default-attachment"), FieldName: stringPtr("附件"), Type: intPtr(17), IsPrimary: boolPtr(false)},
	)

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable:          &cronBitableState{},
		Jobs:             []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("ensureCronBitable: %v", err)
	}

	if got := app.cronState.Bitable.Tables.Tasks; got == "tbl-default" {
		t.Fatalf("tasks table = %q, want a fresh clean table", got)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	fields := api.fieldsByTable[app.cronState.Bitable.Tables.Tasks]
	for _, field := range fields {
		if field == nil {
			continue
		}
		switch stringValue(field.FieldName) {
		case "单选", "日期", "附件":
			t.Fatalf("unexpected default template column leaked into tasks table: %s", stringValue(field.FieldName))
		}
	}
}

func TestEnsureCronBitableTaskSchemaMatchesProductOrder(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronLoaded = true
	app.cronState = &cronStateFile{
		SchemaVersion:    cronStateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		GatewayID:        "gateway-1",
		Bitable:          &cronBitableState{},
		Jobs:             []cronJobState{},
	}
	app.cronBitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, _, err := app.ensureCronBitable(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("ensureCronBitable: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	fields := api.fieldsByTable[app.cronState.Bitable.Tables.Tasks]
	gotNames := make([]string, 0, len(fields))
	enableType := 0
	for _, field := range fields {
		if field == nil {
			continue
		}
		name := stringValue(field.FieldName)
		gotNames = append(gotNames, name)
		if name == "启用" && field.Type != nil {
			enableType = *field.Type
		}
	}
	wantNames := []string{
		"任务名",
		"启用",
		"工作区",
		"提示词",
		"调度类型",
		"调度时间",
		"间隔",
		"超时（分钟）",
		"最近运行时间",
		"最近状态",
		"最近结果摘要",
		"最近错误",
	}
	if fmt.Sprintf("%q", gotNames) != fmt.Sprintf("%q", wantNames) {
		t.Fatalf("task field order = %v, want %v", gotNames, wantNames)
	}
	if enableType != 7 {
		t.Fatalf("enable field type = %d, want 7 (checkbox)", enableType)
	}
}

var _ feishu.BitableAPI = (*fakeCronBitableAPI)(nil)
var _ feishu.BitableAPI = (*flakyCronBootstrapBitableAPI)(nil)
