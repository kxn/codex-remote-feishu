package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	larkbitable "github.com/larksuite/oapi-sdk-go/v3/service/bitable/v1"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	cronrt "github.com/kxn/codex-remote-feishu/internal/app/cronruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type fakeCronBitableAPI struct {
	mu             sync.Mutex
	createRecords  []fakeCronRecordWrite
	updateRecords  []fakeCronRecordWrite
	grantCalls     []fakeCronPermissionGrant
	permissions    map[string]feishu.BitablePermissionMember
	recordsByTable map[string][]*larkbitable.AppTableRecord
	listCalls      []fakeCronRecordWrite
	listRecordsErr error
	batchCreateErr error
	batchUpdateErr error
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

func setCronGatewayLookup(app *App, values ...string) {
	identities := map[string]string{}
	for index := 0; index < len(values); index += 2 {
		gatewayID := strings.TrimSpace(values[index])
		if gatewayID == "" {
			continue
		}
		appID := gatewayID
		if index+1 < len(values) && strings.TrimSpace(values[index+1]) != "" {
			appID = strings.TrimSpace(values[index+1])
		}
		identities[gatewayID] = appID
	}
	app.cronRuntime.gatewayIdentityLookup = func(gatewayID string) (cronrt.GatewayIdentity, bool, error) {
		appID, ok := identities[strings.TrimSpace(gatewayID)]
		if !ok {
			return cronrt.GatewayIdentity{}, false, nil
		}
		return cronrt.GatewayIdentity{GatewayID: strings.TrimSpace(gatewayID), AppID: appID}, true, nil
	}
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

func (f *fakeCronBitableAPI) ListRecords(_ context.Context, appToken, tableID string, _ []string) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listRecordsErr != nil {
		return nil, f.listRecordsErr
	}
	values := f.recordsByTable
	if len(values) == 0 {
		return nil, nil
	}
	tableID = strings.TrimSpace(tableID)
	records := values[tableID]
	cloned := make([]*larkbitable.AppTableRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		copyRecord := *record
		if record.Fields != nil {
			copyRecord.Fields = cloneAnyMap(record.Fields)
		}
		cloned = append(cloned, &copyRecord)
	}
	f.listCalls = append(f.listCalls, fakeCronRecordWrite{AppToken: appToken, TableID: tableID})
	return cloned, nil
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
	if f.batchCreateErr != nil {
		return nil, f.batchCreateErr
	}
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
	if f.batchUpdateErr != nil {
		return nil, f.batchUpdateErr
	}
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

func runCronTestGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func createCronGitTestRepo(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runCronTestGitCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runCronTestGitCommand(t, repoRoot, "add", "README.md")
	runCronTestGitCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init")
	runCronTestGitCommand(t, repoRoot, "branch", "-M", "main")
	return "file://" + filepath.ToSlash(repoRoot), "main"
}

func TestCronSchedulerLaunchesFreshHiddenRun(t *testing.T) {
	workspace := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronrt.JobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronrt.ScheduleTypeInterval,
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

	app.mu.Lock()
	app.maybeScheduleCronJobsLocked(time.Now())
	app.mu.Unlock()

	if launches != 1 {
		t.Fatalf("launches = %d, want 1", launches)
	}
	if !containsEnvEntry(capturedEnv, "CODEX_REMOTE_INSTANCE_SOURCE=cron") {
		t.Fatalf("expected cron source env, got %#v", capturedEnv)
	}
	if !containsEnvEntry(capturedEnv, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected daemon-owned lifetime env, got %#v", capturedEnv)
	}
	if containsEnvEntry(capturedEnv, "CODEX_REMOTE_INSTANCE_MANAGED=1") {
		t.Fatalf("cron run must not mark managed headless env, got %#v", capturedEnv)
	}
	if len(app.cronRuntime.runs) != 1 {
		t.Fatalf("cronRuns = %#v, want one active run", app.cronRuntime.runs)
	}
	active := app.cronRuntime.jobActiveRuns[cronrt.JobActiveKey("rec-task-1", "Nightly")]
	if len(active) != 1 {
		t.Fatalf("active cron runs = %#v, want one instance", active)
	}
	for instanceID := range active {
		if !strings.HasPrefix(instanceID, cronrt.InstancePrefix) {
			t.Fatalf("active cron instance id = %q, want prefix %q", instanceID, cronrt.InstancePrefix)
		}
	}
	if app.cronRuntime.state.Jobs[0].NextRunAt.IsZero() || !app.cronRuntime.state.Jobs[0].NextRunAt.After(time.Now()) {
		t.Fatalf("next run was not advanced: %#v", app.cronRuntime.state.Jobs[0].NextRunAt)
	}
}

func TestCronHelloAndCompletionStayHiddenAndWriteBackFinalMessage(t *testing.T) {
	workspace := t.TempDir()
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	instanceID := cronrt.InstanceIDForRun("rec-task-1", time.Now())
	app.cronRuntime.runs[instanceID] = &cronrt.RunState{
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
	app.cronRuntime.jobActiveRuns[cronrt.JobActiveKey("rec-task-1", "Nightly")] = map[string]struct{}{instanceID: {}}

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
	if _, ok := app.cronRuntime.runs[instanceID]; ok {
		t.Fatalf("completed cron run should be removed from active map")
	}
	if _, ok := app.cronRuntime.exitTargets[instanceID]; !ok {
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronrt.JobState{{
			RecordID:        "rec-task-1",
			Name:            "Nightly",
			ScheduleType:    cronrt.ScheduleTypeInterval,
			IntervalMinutes: 5,
			WorkspaceKey:    workspace,
			Prompt:          "check CI",
			TimeoutMinutes:  15,
			NextRunAt:       time.Now().Add(-time.Minute),
		}},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	app.cronRuntime.runs["inst-running"] = &cronrt.RunState{
		InstanceID:     "inst-running",
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   workspace,
		TriggeredAt:    time.Now().Add(-2 * time.Minute),
		TimeoutMinutes: 15,
	}
	app.cronRuntime.jobActiveRuns[cronrt.JobActiveKey("rec-task-1", "Nightly")] = map[string]struct{}{"inst-running": {}}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatalf("scheduler must not launch another hidden run while previous run is active")
		return 0, nil
	}

	app.mu.Lock()
	app.maybeScheduleCronJobsLocked(time.Now())
	app.mu.Unlock()

	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := api.createRecords[0].Fields["状态"]; got != "跳过" {
		t.Fatalf("history status = %#v, want 跳过", got)
	}
	if got := api.updateRecords[0].Fields["最近错误"]; !strings.Contains(got.(string), "并发上限") {
		t.Fatalf("skip reason = %#v, want contains 并发上限", got)
	}
}

func TestCronSchedulerTimesOutRunAndRequestsExit(t *testing.T) {
	workspace := t.TempDir()
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }
	instanceID := cronrt.InstanceIDForRun("rec-task-1", time.Now().Add(-time.Hour))
	app.cronRuntime.runs[instanceID] = &cronrt.RunState{
		InstanceID:     instanceID,
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   workspace,
		TriggeredAt:    time.Now().Add(-40 * time.Minute),
		StartedAt:      time.Now().Add(-35 * time.Minute),
		TimeoutMinutes: 30,
		PID:            9876,
	}
	app.cronRuntime.jobActiveRuns[cronrt.JobActiveKey("rec-task-1", "Nightly")] = map[string]struct{}{instanceID: {}}
	var commands []agentproto.Command
	app.sendAgentCommand = func(target string, command agentproto.Command) error {
		if target != instanceID {
			t.Fatalf("unexpected target = %q", target)
		}
		commands = append(commands, command)
		return nil
	}

	app.mu.Lock()
	app.maybeScheduleCronJobsLocked(time.Now())
	app.mu.Unlock()

	if len(commands) != 1 || commands[0].Kind != agentproto.CommandProcessExit {
		t.Fatalf("expected one process.exit command, got %#v", commands)
	}
	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	defer api.mu.Unlock()
	if got := api.createRecords[0].Fields["状态"]; got != "超时" {
		t.Fatalf("history status = %#v, want 超时", got)
	}
	if _, ok := app.cronRuntime.runs[instanceID]; ok {
		t.Fatalf("timed-out cron run should be removed from active map")
	}
}

func TestCronShowReturnsCatalogWithoutEnteringMutatingGate(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1", "gateway-2", "app-2")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.syncInFlight = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			AppURL:   "https://example.feishu.cn/base/app-cron",
		},
	}

	events := app.handleCronDaemonCommand(control.DaemonCommand{
		Text:             "/cron",
		GatewayID:        "gateway-2",
		SurfaceSessionID: "surface-2",
	})
	if len(events) != 1 || events[0].Kind != eventcontract.KindPage {
		t.Fatalf("events = %#v, want one direct page card", events)
	}
	if !app.cronRuntime.syncInFlight {
		t.Fatalf("view-only /cron should not clear or claim the mutating sync gate")
	}
	if app.cronRuntime.state.OwnerGatewayID != "gateway-1" {
		t.Fatalf("view-only /cron must not rewrite owner state: %#v", app.cronRuntime.state)
	}
	catalog := catalogFromUIEvent(t, events[0])
	if summary := catalogSummaryText(catalog); strings.Contains(summary, "当前状态：正常") {
		t.Fatalf("summary = %q, want root page without default status dump", summary)
	}
}

func TestCronReloadUsesResolvedOwnerGateway(t *testing.T) {
	api := &fakeCronBitableAPI{
		recordsByTable: map[string][]*larkbitable.AppTableRecord{
			"tbl-workspaces": {{
				RecordId: stringPtr("rec-workspace-1"),
				Fields: map[string]any{
					"工作区名称": "project",
					"工作区键":  "/tmp/project",
					"当前状态":  "可用",
				},
			}},
			"tbl-tasks": {{
				RecordId: stringPtr("rec-task-1"),
				Fields: map[string]any{
					"任务名":    "Nightly",
					"启用":     true,
					"调度类型":   cronrt.ScheduleTypeInterval,
					"间隔":     "15分钟",
					"工作区":    []any{"rec-workspace-1"},
					"提示词":    "check CI",
					"超时（分钟）": "20",
				},
			}},
		},
	}
	var factoryGateways []string
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1", "gateway-2", "app-2")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Workspaces: "tbl-workspaces",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		factoryGateways = append(factoryGateways, gatewayID)
		return api, nil
	}

	if _, err := app.reloadCronJobsNow(control.DaemonCommand{GatewayID: "gateway-2", SurfaceSessionID: "surface-2"}); err != nil {
		t.Fatalf("reloadCronJobsNow: %v", err)
	}
	if fmt.Sprintf("%q", factoryGateways) != fmt.Sprintf("%q", []string{"gateway-1"}) {
		t.Fatalf("factory gateways = %v, want owner gateway only", factoryGateways)
	}
	if len(app.cronRuntime.state.Jobs) != 1 || app.cronRuntime.state.Jobs[0].RecordID != "rec-task-1" {
		t.Fatalf("jobs = %#v, want one loaded task", app.cronRuntime.state.Jobs)
	}
	if app.cronRuntime.state.OwnerGatewayID != "gateway-1" {
		t.Fatalf("reload must not rewrite owner: %#v", app.cronRuntime.state)
	}
}

func TestCronReloadParsesGitRepoSourceInput(t *testing.T) {
	repoURL, ref := createCronGitTestRepo(t)
	api := &fakeCronBitableAPI{
		recordsByTable: map[string][]*larkbitable.AppTableRecord{
			"tbl-workspaces": {},
			"tbl-tasks": {{
				RecordId: stringPtr("rec-task-git"),
				Fields: map[string]any{
					"任务名":                        "Git Nightly",
					"启用":                         true,
					cronrt.TaskSourceTypeField:   cronrt.TaskSourceGitRepoText,
					cronrt.TaskGitRepoInputField: repoURL + "#ref=" + ref,
					"调度类型":                       cronrt.ScheduleTypeInterval,
					"间隔":                         "15分钟",
					"提示词":                        "check repo",
					"超时（分钟）":                     "20",
				},
			}},
		},
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Workspaces: "tbl-workspaces",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		if gatewayID != "gateway-1" {
			return nil, fmt.Errorf("unexpected gateway %q", gatewayID)
		}
		return api, nil
	}

	if _, err := app.reloadCronJobsNow(control.DaemonCommand{GatewayID: "gateway-1", SurfaceSessionID: "surface-1"}); err != nil {
		t.Fatalf("reloadCronJobsNow: %v", err)
	}
	if len(app.cronRuntime.state.Jobs) != 1 {
		t.Fatalf("jobs = %#v, want one git job", app.cronRuntime.state.Jobs)
	}
	job := app.cronRuntime.state.Jobs[0]
	if job.SourceType != cronrt.JobSourceGitRepo {
		t.Fatalf("source type = %q, want %q", job.SourceType, cronrt.JobSourceGitRepo)
	}
	if job.GitRepoSourceInput != repoURL+"#ref="+ref || job.GitRepoURL != repoURL || job.GitRef != ref {
		t.Fatalf("unexpected git source fields: %#v", job)
	}
	if job.WorkspaceKey != "" || job.WorkspaceRecordID != "" {
		t.Fatalf("git job must not keep workspace fields: %#v", job)
	}
}

func TestCronSchedulerMaterializesGitRepoSourceAndWritesSourceLabel(t *testing.T) {
	repoURL, ref := createCronGitTestRepo(t)
	api := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: t.TempDir(),
		},
	})
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks",
				Runs:  "tbl-runs",
			},
		},
		Jobs: []cronrt.JobState{{
			RecordID:           "rec-task-git",
			Name:               "Git Nightly",
			ScheduleType:       cronrt.ScheduleTypeInterval,
			IntervalMinutes:    5,
			SourceType:         cronrt.JobSourceGitRepo,
			GitRepoSourceInput: repoURL + "#ref=" + ref,
			GitRepoURL:         repoURL,
			GitRef:             ref,
			Prompt:             "check repo",
			TimeoutMinutes:     15,
			NextRunAt:          time.Now().Add(-time.Minute),
		}},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	var capturedWorkDir string
	var capturedEnv []string
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		capturedWorkDir = opts.WorkDir
		capturedEnv = append([]string(nil), opts.Env...)
		return 4321, nil
	}
	var commands []agentproto.Command
	app.sendAgentCommand = func(target string, command agentproto.Command) error {
		commands = append(commands, command)
		return nil
	}

	app.mu.Lock()
	app.maybeScheduleCronJobsLocked(time.Now())
	var instanceID string
	var runCopy cronrt.RunState
	for id, run := range app.cronRuntime.runs {
		instanceID = id
		runCopy = *run
	}
	app.mu.Unlock()

	if instanceID == "" {
		t.Fatal("expected one active cron git run")
	}
	if !containsEnvEntry(capturedEnv, "CODEX_REMOTE_INSTANCE_SOURCE=cron") {
		t.Fatalf("expected cron source env, got %#v", capturedEnv)
	}
	if !strings.Contains(filepath.ToSlash(capturedWorkDir), "/cron-repos/runs/") {
		t.Fatalf("git cron run dir = %q, want cron-repos/runs prefix", capturedWorkDir)
	}
	if _, err := os.Stat(filepath.Join(capturedWorkDir, "README.md")); err != nil {
		t.Fatalf("materialized worktree missing repo file: %v", err)
	}
	if runCopy.SourceType != cronrt.JobSourceGitRepo || runCopy.SourceLabel == "" || runCopy.RunRoot == "" || runCopy.GitSourceKey == "" {
		t.Fatalf("unexpected run state: %#v", runCopy)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID: instanceID,
			Source:     "cron",
			PID:        4321,
		},
	})
	app.onEvents(context.Background(), instanceID, []agentproto.Event{
		{Kind: agentproto.EventTurnStarted, ThreadID: "thread-git-1", TurnID: "turn-git-1"},
		{Kind: agentproto.EventItemDelta, ThreadID: "thread-git-1", TurnID: "turn-git-1", ItemID: "item-git-1", ItemKind: "agent_message", Delta: "done"},
		{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-git-1", TurnID: "turn-git-1", Status: "completed"},
	})

	api.waitForWrites(t, 1, 1)
	api.mu.Lock()
	runFields := cloneAnyMap(api.createRecords[0].Fields)
	api.mu.Unlock()
	if got := runFields["工作区"]; got != runCopy.SourceLabel {
		t.Fatalf("run history source label = %#v, want %q", got, runCopy.SourceLabel)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(runCopy.RunRoot); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(runCopy.RunRoot); err == nil {
		t.Fatalf("expected git cron run root to be cleaned up: %s", runCopy.RunRoot)
	}
}

func TestCronReloadResultTracksLoadedDisabledStoppedAndErrors(t *testing.T) {
	api := &fakeCronBitableAPI{
		recordsByTable: map[string][]*larkbitable.AppTableRecord{
			"tbl-workspaces": {{
				RecordId: stringPtr("rec-workspace-1"),
				Fields: map[string]any{
					"工作区名称": "project",
					"工作区键":  "/tmp/project",
					"当前状态":  "可用",
				},
			}},
			"tbl-tasks": {
				{
					RecordId: stringPtr("rec-keep"),
					Fields: map[string]any{
						"任务名":    "Keep",
						"启用":     true,
						"调度类型":   cronrt.ScheduleTypeInterval,
						"间隔":     "15分钟",
						"工作区":    []any{"rec-workspace-1"},
						"提示词":    "keep running",
						"超时（分钟）": "20",
					},
				},
				{
					RecordId: stringPtr("rec-add"),
					Fields: map[string]any{
						"任务名":  "Add",
						"启用":   true,
						"调度类型": cronrt.ScheduleTypeDaily,
						"调度时间": "09:30",
						"工作区":  []any{"rec-workspace-1"},
						"提示词":  "daily check",
					},
				},
				{
					RecordId: stringPtr("rec-disable"),
					Fields: map[string]any{
						"任务名":  "Disable",
						"启用":   false,
						"调度类型": cronrt.ScheduleTypeInterval,
						"间隔":   "30分钟",
						"工作区":  []any{"rec-workspace-1"},
						"提示词":  "disabled for now",
					},
				},
				{
					RecordId: stringPtr("rec-error"),
					Fields: map[string]any{
						"任务名":  "Broken",
						"启用":   true,
						"调度类型": cronrt.ScheduleTypeInterval,
						"间隔":   "15分钟",
						"工作区":  []any{"rec-workspace-1"},
					},
				},
			},
		},
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Workspaces: "tbl-workspaces",
			},
		},
		Jobs: []cronrt.JobState{
			{RecordID: "rec-keep", Name: "Keep", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 15, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(15 * time.Minute)},
			{RecordID: "rec-disable", Name: "Disable", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 30, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(30 * time.Minute)},
			{RecordID: "rec-delete", Name: "Delete", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 10, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(10 * time.Minute)},
			{RecordID: "rec-error", Name: "Broken", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 15, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(15 * time.Minute)},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		if gatewayID != "gateway-1" {
			return nil, fmt.Errorf("unexpected gateway %q", gatewayID)
		}
		return api, nil
	}

	result, err := app.reloadCronJobsResultNow(control.DaemonCommand{GatewayID: "gateway-1", SurfaceSessionID: "surface-1"})
	if err != nil {
		t.Fatalf("reloadCronJobsResultNow: %v", err)
	}
	if len(result.Loaded) != 2 {
		t.Fatalf("loaded = %#v, want 2", result.Loaded)
	}
	if result.Loaded[0].RecordID != "rec-keep" || result.Loaded[0].ChangeKind != cronrt.ReloadTaskChangeKept {
		t.Fatalf("expected kept loaded item first, got %#v", result.Loaded[0])
	}
	if result.Loaded[1].RecordID != "rec-add" || result.Loaded[1].ChangeKind != cronrt.ReloadTaskChangeAdded {
		t.Fatalf("expected added loaded item second, got %#v", result.Loaded[1])
	}
	if len(result.Disabled) != 1 || result.Disabled[0].RecordID != "rec-disable" {
		t.Fatalf("disabled = %#v, want rec-disable", result.Disabled)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("errors = %#v, want 1", result.Errors)
	}
	if result.Errors[0].RecordID != "rec-error" || result.Errors[0].FieldName != "提示词" || result.Errors[0].TableName != cronrt.TasksTableName || result.Errors[0].RowNumber != 4 {
		t.Fatalf("unexpected structured error: %#v", result.Errors[0])
	}
	if len(result.Stopped) != 3 {
		t.Fatalf("stopped = %#v, want 3", result.Stopped)
	}
	if result.Stopped[0].RecordID != "rec-disable" || result.Stopped[0].Reason != "表格中已停用" {
		t.Fatalf("expected disabled stopped reason, got %#v", result.Stopped[0])
	}
	if result.Stopped[1].RecordID != "rec-delete" || result.Stopped[1].Reason != "表格中已删除" {
		t.Fatalf("expected deleted stopped reason, got %#v", result.Stopped[1])
	}
	if result.Stopped[2].RecordID != "rec-error" || result.Stopped[2].Reason != "配置错误，未继续生效" {
		t.Fatalf("expected error stopped reason, got %#v", result.Stopped[2])
	}
	if len(app.cronRuntime.state.Jobs) != 2 || app.cronRuntime.state.Jobs[0].RecordID != "rec-keep" || app.cronRuntime.state.Jobs[1].RecordID != "rec-add" {
		t.Fatalf("persisted jobs = %#v, want only loaded jobs", app.cronRuntime.state.Jobs)
	}
	if summary := app.cronRuntime.state.LastReloadSummary; !strings.Contains(summary, "已加载 2 条任务") || !strings.Contains(summary, "停用 1 条") || !strings.Contains(summary, "停止 3 条") || !strings.Contains(summary, "发现 1 条配置错误") {
		t.Fatalf("unexpected compact summary: %q", summary)
	}
}

func TestCronReloadNoticeShowsStructuredSections(t *testing.T) {
	api := &fakeCronBitableAPI{
		recordsByTable: map[string][]*larkbitable.AppTableRecord{
			"tbl-workspaces": {{
				RecordId: stringPtr("rec-workspace-1"),
				Fields: map[string]any{
					"工作区名称": "project",
					"工作区键":  "/tmp/project",
					"当前状态":  "可用",
				},
			}},
			"tbl-tasks": {
				{
					RecordId: stringPtr("rec-keep"),
					Fields: map[string]any{
						"任务名":  "Keep",
						"启用":   true,
						"调度类型": cronrt.ScheduleTypeInterval,
						"间隔":   "15分钟",
						"工作区":  []any{"rec-workspace-1"},
						"提示词":  "keep running",
					},
				},
				{
					RecordId: stringPtr("rec-disable"),
					Fields: map[string]any{
						"任务名":  "Disable",
						"启用":   false,
						"调度类型": cronrt.ScheduleTypeDaily,
						"调度时间": "09:30",
						"工作区":  []any{"rec-workspace-1"},
						"提示词":  "disabled for now",
					},
				},
				{
					RecordId: stringPtr("rec-error"),
					Fields: map[string]any{
						"任务名":  "Broken",
						"启用":   true,
						"调度类型": cronrt.ScheduleTypeInterval,
						"间隔":   "15分钟",
						"工作区":  []any{"rec-workspace-1"},
					},
				},
			},
		},
	}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		OwnerAppID:       "app-1",
		OwnerBoundAt:     time.Now().UTC().Add(-time.Hour),
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
			Tables: cronrt.TableIDs{
				Tasks:      "tbl-tasks",
				Workspaces: "tbl-workspaces",
			},
		},
		Jobs: []cronrt.JobState{
			{RecordID: "rec-keep", Name: "Keep", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 15, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(15 * time.Minute)},
			{RecordID: "rec-disable", Name: "Disable", ScheduleType: cronrt.ScheduleTypeDaily, DailyHour: 9, DailyMinute: 30, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(24 * time.Hour)},
			{RecordID: "rec-delete", Name: "Delete", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 10, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(10 * time.Minute)},
			{RecordID: "rec-error", Name: "Broken", ScheduleType: cronrt.ScheduleTypeInterval, IntervalMinutes: 15, WorkspaceKey: "/tmp/project", NextRunAt: time.Now().Add(15 * time.Minute)},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		if gatewayID != "gateway-1" {
			return nil, fmt.Errorf("unexpected gateway %q", gatewayID)
		}
		return api, nil
	}

	event, err := app.reloadCronJobs(control.DaemonCommand{GatewayID: "gateway-1", SurfaceSessionID: "surface-1"})
	if err != nil {
		t.Fatalf("reloadCronJobs: %v", err)
	}
	if event == nil || event.Notice == nil {
		t.Fatalf("expected notice event, got %#v", event)
	}
	text := event.Notice.Text
	for _, fragment := range []string{
		"已加载：",
		"`Keep（保留）`",
		"已停用：",
		"`Disable`",
		"本次停止：",
		"`Delete`",
		"配置错误：",
		"任务配置表 第 3 行",
		"字段：提示词",
		"记录：rec-error",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected reload notice to contain %q, got %q", fragment, text)
		}
	}
	if strings.Contains(app.cronRuntime.state.LastReloadSummary, "`Keep`") || strings.Contains(app.cronRuntime.state.LastReloadSummary, "已加载：") {
		t.Fatalf("status summary should stay compact, got %q", app.cronRuntime.state.LastReloadSummary)
	}
}

func TestCronCompletionUsesFrozenWritebackTargetAfterOwnerChange(t *testing.T) {
	api1 := &fakeCronBitableAPI{}
	api2 := &fakeCronBitableAPI{}
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1", "gateway-2", "app-2")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		OwnerGatewayID: "gateway-1",
		OwnerAppID:     "app-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-1",
			Tables: cronrt.TableIDs{
				Tasks: "tbl-tasks-1",
				Runs:  "tbl-runs-1",
			},
		},
	}
	app.cronRuntime.bitableFactory = func(gatewayID string) (feishu.BitableAPI, error) {
		switch gatewayID {
		case "gateway-1":
			return api1, nil
		case "gateway-2":
			return api2, nil
		default:
			return nil, fmt.Errorf("unexpected gateway %q", gatewayID)
		}
	}
	instanceID := "inst-cron-frozen-target"
	app.cronRuntime.runs[instanceID] = &cronrt.RunState{
		RunID:      instanceID,
		InstanceID: instanceID,
		GatewayID:  "gateway-1",
		WritebackTarget: cronrt.WritebackTarget{
			GatewayID: "gateway-1",
			Bitable: cronrt.BitableState{
				AppToken: "app-1",
				Tables: cronrt.TableIDs{
					Tasks: "tbl-tasks-1",
					Runs:  "tbl-runs-1",
				},
			},
		},
		JobRecordID:    "rec-task-1",
		JobName:        "Nightly",
		WorkspaceKey:   "/tmp/project",
		TriggeredAt:    time.Now().UTC().Add(-time.Minute),
		StartedAt:      time.Now().UTC().Add(-30 * time.Second),
		CompletedAt:    time.Now().UTC(),
		FinalMessage:   "done",
		TimeoutMinutes: 15,
	}
	app.cronRuntime.state.OwnerGatewayID = "gateway-2"
	app.cronRuntime.state.OwnerAppID = "app-2"
	app.cronRuntime.state.Bitable = &cronrt.BitableState{
		AppToken: "app-2",
		Tables: cronrt.TableIDs{
			Tasks: "tbl-tasks-2",
			Runs:  "tbl-runs-2",
		},
	}

	app.completeCronRunLocked(instanceID, "completed", "", time.Now().UTC(), false)

	api1.waitForWrites(t, 1, 1)
	api1.mu.Lock()
	defer api1.mu.Unlock()
	if len(api2.createRecords) != 0 || len(api2.updateRecords) != 0 {
		t.Fatalf("frozen writeback must not switch to the newer owner: api2=%#v %#v", api2.createRecords, api2.updateRecords)
	}
	if api1.createRecords[0].AppToken != "app-1" || api1.updateRecords[0].AppToken != "app-1" {
		t.Fatalf("writeback target app token = %#v / %#v, want app-1", api1.createRecords[0], api1.updateRecords[0])
	}
}

func TestCronJobFromRecordParsesLinkedWorkspaceValues(t *testing.T) {
	now := time.Now()
	record := &larkbitable.AppTableRecord{
		RecordId: stringPtr("rec-task-1"),
		Fields: map[string]any{
			"任务名":  "Nightly",
			"启用":   true,
			"调度类型": cronrt.ScheduleTypeInterval,
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
	job, skip, err := cronJobFromRecord(record, map[string]cronrt.WorkspaceRow{
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
	if job.IntervalMinutes != 15 || job.TimeoutMinutes != 20 || job.MaxConcurrency != 1 {
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
			"调度类型": cronrt.ScheduleTypeDaily,
			"调度时间": "09:30",
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "daily check",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronrt.WorkspaceRow{
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
			"调度类型": cronrt.ScheduleTypeDaily,
			"每天-时": 7,
			"每天-分": 5,
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "daily check",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronrt.WorkspaceRow{
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
			"调度类型": cronrt.ScheduleTypeInterval,
			"间隔":   "10分钟",
			"工作区":  []any{"rec-workspace-1"},
			"提示词":  "check CI",
		},
	}
	job, skip, err := cronJobFromRecord(record, map[string]cronrt.WorkspaceRow{
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
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
	recordsByTable   map[string][]*larkbitable.AppTableRecord
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
		recordsByTable: map[string][]*larkbitable.AppTableRecord{},
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

func (f *flakyCronBootstrapBitableAPI) ListRecords(_ context.Context, _ string, tableID string, _ []string) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	records := f.recordsByTable[tableID]
	values := make([]*larkbitable.AppTableRecord, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		cloned := *record
		if record.Fields != nil {
			cloned.Fields = cloneAnyMap(record.Fields)
		}
		values = append(values, &cloned)
	}
	return values, nil
}

func (f *flakyCronBootstrapBitableAPI) CreateRecord(_ context.Context, _ string, tableID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	recordID := fmt.Sprintf("%s-rec-%d", tableID, len(f.recordsByTable[tableID])+1)
	record := &larkbitable.AppTableRecord{RecordId: stringPtr(recordID), Fields: cloneAnyMap(fields)}
	f.recordsByTable[tableID] = append(f.recordsByTable[tableID], record)
	return &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)}, nil
}

func (f *flakyCronBootstrapBitableAPI) UpdateRecord(_ context.Context, _ string, tableID, recordID string, fields map[string]any) (*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, record := range f.recordsByTable[tableID] {
		if record == nil || strings.TrimSpace(stringValue(record.RecordId)) != strings.TrimSpace(recordID) {
			continue
		}
		record.Fields = cloneAnyMap(fields)
		return &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)}, nil
	}
	record := &larkbitable.AppTableRecord{RecordId: stringPtr(recordID), Fields: cloneAnyMap(fields)}
	f.recordsByTable[tableID] = append(f.recordsByTable[tableID], record)
	return &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)}, nil
}

func (f *flakyCronBootstrapBitableAPI) BatchCreateRecords(_ context.Context, _ string, tableID string, values []map[string]any) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for _, fields := range values {
		recordID := fmt.Sprintf("rec-%d", len(f.recordsByTable[tableID])+1)
		record := &larkbitable.AppTableRecord{RecordId: stringPtr(recordID), Fields: cloneAnyMap(fields)}
		f.recordsByTable[tableID] = append(f.recordsByTable[tableID], record)
		records = append(records, &larkbitable.AppTableRecord{RecordId: stringPtr(recordID)})
	}
	return records, nil
}

func (f *flakyCronBootstrapBitableAPI) BatchUpdateRecords(_ context.Context, _ string, tableID string, values []feishu.BitableRecordUpdate) ([]*larkbitable.AppTableRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	records := make([]*larkbitable.AppTableRecord, 0, len(values))
	for _, update := range values {
		recordID := strings.TrimSpace(update.RecordID)
		if recordID == "" {
			recordID = "rec-meta"
		}
		found := false
		for _, record := range f.recordsByTable[tableID] {
			if record == nil || strings.TrimSpace(stringValue(record.RecordId)) != recordID {
				continue
			}
			record.Fields = cloneAnyMap(update.Fields)
			found = true
			break
		}
		if !found {
			f.recordsByTable[tableID] = append(f.recordsByTable[tableID], &larkbitable.AppTableRecord{
				RecordId: stringPtr(recordID),
				Fields:   cloneAnyMap(update.Fields),
			})
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		Bitable:          &cronrt.BitableState{},
		Jobs:             []cronrt.JobState{},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	_, err := app.repairCronBitableNow(control.DaemonCommand{GatewayID: "gateway-1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repairCronBitableNow first error = %v, want context deadline exceeded", err)
	}
	if app.cronRuntime.state == nil || app.cronRuntime.state.Bitable == nil {
		t.Fatalf("cron state = %#v, want persisted bitable binding", app.cronRuntime.state)
	}
	if app.cronRuntime.state.Bitable.AppToken != "app-cron" {
		t.Fatalf("app token = %q, want app-cron", app.cronRuntime.state.Bitable.AppToken)
	}
	if app.cronRuntime.state.Bitable.DefaultTable != "tbl-default" {
		t.Fatalf("default table = %q, want tbl-default", app.cronRuntime.state.Bitable.DefaultTable)
	}
	if app.cronRuntime.state.Bitable.Tables.Tasks == "" || app.cronRuntime.state.Bitable.Tables.Workspaces == "" || app.cronRuntime.state.Bitable.Tables.Runs == "" || app.cronRuntime.state.Bitable.Tables.Meta == "" {
		t.Fatalf("table binding not persisted after timeout: %#v", app.cronRuntime.state.Bitable.Tables)
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

	_, err = app.repairCronBitableNow(control.DaemonCommand{GatewayID: "gateway-1"})
	if err != nil {
		t.Fatalf("repairCronBitableNow second attempt: %v", err)
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
	if app.cronRuntime.state.Bitable.LastVerified.IsZero() {
		t.Fatalf("expected successful retry to verify binding, got %#v", app.cronRuntime.state.Bitable)
	}
}

func TestEnsureCronBitableRecoversLegacyPartialStateWithFreshTasksTable(t *testing.T) {
	api := newFlakyCronBootstrapBitableAPI()
	api.failCreateField = false

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		Bitable: &cronrt.BitableState{
			AppToken: "app-cron",
		},
		Jobs: []cronrt.JobState{},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	_, err := app.repairCronBitableNow(control.DaemonCommand{GatewayID: "gateway-1"})
	if err != nil {
		t.Fatalf("repairCronBitableNow with legacy partial state: %v", err)
	}
	if api.createAppCalls != 0 {
		t.Fatalf("createAppCalls = %d, want 0 for persisted app token", api.createAppCalls)
	}
	if api.createTableCalls != 4 {
		t.Fatalf("createTableCalls = %d, want 4 when tasks table is created fresh", api.createTableCalls)
	}
	if got := app.cronRuntime.state.Bitable.Tables.Tasks; got == "tbl-default" {
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
	setCronGatewayLookup(app, "gateway-1", "app-1")
	app.headlessRuntime.Paths.StateDir = t.TempDir()
	app.cronRuntime.loaded = true
	app.cronRuntime.state = &cronrt.StateFile{
		SchemaVersion:    cronrt.StateSchemaVersion,
		InstanceScopeKey: "stable",
		InstanceLabel:    "stable",
		OwnerGatewayID:   "gateway-1",
		Bitable:          &cronrt.BitableState{},
		Jobs:             []cronrt.JobState{},
	}
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) { return api, nil }

	if _, err := app.repairCronBitableNow(control.DaemonCommand{GatewayID: "gateway-1"}); err != nil {
		t.Fatalf("repairCronBitableNow: %v", err)
	}

	if got := app.cronRuntime.state.Bitable.Tables.Tasks; got == "tbl-default" {
		t.Fatalf("tasks table = %q, want a fresh clean table", got)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	fields := api.fieldsByTable[app.cronRuntime.state.Bitable.Tables.Tasks]
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

func TestHandleActionCronCommandDoesNotHoldAppLockDuringNoticeDelivery(t *testing.T) {
	gateway := &reentrantAppLockGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	gateway.app = app
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.cronRuntime.bitableFactory = func(string) (feishu.BitableAPI, error) {
		return nil, errors.New("cron bitable unavailable in test")
	}

	done := make(chan struct{})
	go func() {
		app.HandleAction(context.Background(), control.Action{
			Kind:             control.ActionCronCommand,
			SurfaceSessionID: "surface-1",
			GatewayID:        "app-1",
			ChatID:           "chat-1",
			ActorUserID:      "user-1",
			Text:             "/cron",
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cron action timed out (possible app-lock deadlock)")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		operations, err := gateway.snapshot()
		if err != nil {
			t.Fatalf("cron notice delivery should not fail while reentering app lock: %v", err)
		}
		if len(operations) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	operations, err := gateway.snapshot()
	if err != nil {
		t.Fatalf("cron notice delivery should not fail while reentering app lock: %v", err)
	}
	t.Fatalf("expected cron action to emit at least one gateway operation, got %#v", operations)
}

var _ feishu.BitableAPI = (*fakeCronBitableAPI)(nil)
var _ feishu.BitableAPI = (*flakyCronBootstrapBitableAPI)(nil)
