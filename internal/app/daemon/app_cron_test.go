package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type fakeCronBitableAPI struct {
	mu            sync.Mutex
	createRecords []fakeCronRecordWrite
	updateRecords []fakeCronRecordWrite
}

type fakeCronRecordWrite struct {
	AppToken string
	TableID  string
	RecordID string
	Fields   map[string]any
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

func (f *fakeCronBitableAPI) ListPermissionMembers(context.Context, string, string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (f *fakeCronBitableAPI) GrantPermission(context.Context, string, string, string, string, string) error {
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
			"启用":   "启用",
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

var _ feishu.BitableAPI = (*fakeCronBitableAPI)(nil)
