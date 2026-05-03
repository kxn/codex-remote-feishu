package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestRepairCommandReconnectsFeishuAndRestartsIdleChild(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
	})

	started := make(chan struct{})
	release := make(chan struct{})
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" || command.Kind != agentproto.CommandProcessChildRestart {
			t.Fatalf("unexpected repair child command: instance=%s command=%#v", instanceID, command)
		}
		go func() {
			close(started)
			<-release
			app.onCommandAck(context.Background(), instanceID, agentproto.CommandAck{
				CommandID: command.CommandID,
				Accepted:  true,
			})
			app.onEvents(context.Background(), instanceID, []agentproto.Event{
				agentproto.NewChildRestartUpdatedEvent(command.CommandID, "thread-1", agentproto.ChildRestartStatusSucceeded, nil),
			})
		}()
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for repair child restart dispatch")
	}
	close(release)

	waitForRepairGatewayUpsert(t, gateway, "main")
	if len(gateway.upserted) != 1 {
		t.Fatalf("expected one feishu runtime reconnect, got %#v", gateway.upserted)
	}
}

func TestRepairCommandSkipsBusyChildButStillReconnectsFeishu(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	surface.ActiveQueueItemID = "queue-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
	})
	app.sendAgentCommand = func(string, agentproto.Command) error {
		t.Fatal("repair must not restart a busy child")
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}

	waitForRepairGatewayUpsert(t, gateway, "main")
	if len(gateway.upserted) != 1 {
		t.Fatalf("expected one feishu runtime reconnect, got %#v", gateway.upserted)
	}
}

func TestRepairCommandReportsPartialWhenFeishuRuntimeMissing(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "missing", "chat-1", "user-1")
	app.sendAgentCommand = func(string, agentproto.Command) error {
		t.Fatal("repair must not restart child for detached surface")
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "missing",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}

	waitForRepairOperation(t, gateway, "找不到 gateway `missing`")
	waitForRepairOperation(t, gateway, "修复未完全完成")
	if len(gateway.upserted) != 0 {
		t.Fatalf("missing gateway must not reconnect runtime, got %#v", gateway.upserted)
	}
}

func TestRepairCommandRechecksChildSafetyAfterFeishuReconnect(t *testing.T) {
	app := newRepairTestApp(t, nil)
	gateway := &fakeAdminGatewayController{
		onUpsert: func(feishu.GatewayAppConfig) {
			app.mu.Lock()
			defer app.mu.Unlock()
			surface := app.service.Surface("surface-1")
			surface.ActiveQueueItemID = "queue-1"
		},
	}
	app.gateway = gateway
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
	})
	app.sendAgentCommand = func(string, agentproto.Command) error {
		t.Fatal("repair must recheck child safety before dispatching restart")
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}

	waitForRepairGatewayUpsert(t, gateway, "main")
	waitForRepairOperation(t, gateway, "当前请求正在派发或执行")
	waitForRepairOperation(t, gateway, "修复未完全完成")
}

func TestRepairCommandRemovesOfflineManagedHeadlessAndStartsRestore(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.headlessRuntime.BinaryPath = "/bin/codex-remote"
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		if opts.InstanceID == "inst-headless-old" {
			t.Fatalf("repair must not restart stale headless id directly")
		}
		return 4321, nil
	}
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-headless-old"
	surface.SelectedThreadID = "thread-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-old",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "Design",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}

	waitForRepairGatewayUpsert(t, gateway, "main")
	waitForRepairCondition(t, "offline headless removal", func() bool {
		return app.service.Instance("inst-headless-old") == nil &&
			strings.TrimSpace(app.service.AttachedInstanceID("surface-1")) == ""
	})
	if pending := app.service.Surface("surface-1").PendingHeadless; pending == nil || pending.ThreadID != "thread-1" {
		t.Fatalf("expected repair to start headless restore for thread-1, got %#v", pending)
	}
}

func TestRepairCommandRechecksManagedHeadlessBeforeRestore(t *testing.T) {
	app := newRepairTestApp(t, nil)
	gateway := &fakeAdminGatewayController{
		onUpsert: func(feishu.GatewayAppConfig) {
			app.mu.Lock()
			defer app.mu.Unlock()
			inst := app.service.Instance("inst-headless-old")
			inst.Source = "external"
			inst.Managed = false
		},
	}
	app.gateway = gateway
	app.headlessRuntime.BinaryPath = "/bin/codex-remote"
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Fatal("repair must not restore an instance that is no longer managed headless")
		return 0, nil
	}
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	surface := app.service.Surface("surface-1")
	surface.AttachedInstanceID = "inst-headless-old"
	surface.SelectedThreadID = "thread-1"
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-old",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "Design",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	})

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_started" {
		t.Fatalf("expected repair started notice, got %#v", events)
	}

	waitForRepairGatewayUpsert(t, gateway, "main")
	waitForRepairOperation(t, gateway, "不会清理或重建这个实例")
	if app.service.Instance("inst-headless-old") == nil {
		t.Fatal("repair must keep an instance that is no longer managed headless")
	}
	if pending := app.service.Surface("surface-1").PendingHeadless; pending != nil {
		t.Fatalf("repair must not start headless restore, got %#v", pending)
	}
}

func TestParseRepairCommandTextRecognizesDaemon(t *testing.T) {
	parsed, err := parseRepairCommandText("/repair daemon")
	if err != nil {
		t.Fatalf("parseRepairCommandText: %v", err)
	}
	if parsed.Mode != repairCommandDaemon {
		t.Fatalf("mode = %q, want %q", parsed.Mode, repairCommandDaemon)
	}
}

func TestRepairDaemonCommandRestartsManagedDaemon(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	statePath := filepath.Join(app.headlessRuntime.Paths.DataDir, "install-state.json")
	if err := install.WriteState(statePath, install.InstallState{
		StatePath:       statePath,
		ConfigPath:      filepath.Join(app.headlessRuntime.Paths.ConfigDir, "config.json"),
		ServiceManager:  install.ServiceManagerSystemdUser,
		InstalledBinary: "/bin/codex-remote",
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	called := make(chan install.InstallState, 1)
	app.restartDaemon = func(_ context.Context, stateValue install.InstallState) error {
		called <- stateValue
		return nil
	}
	app.sendAgentCommand = func(string, agentproto.Command) error {
		t.Fatal("repair daemon must not restart provider child")
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair daemon",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_daemon_restart_started" {
		t.Fatalf("expected daemon restart started notice, got %#v", events)
	}

	select {
	case stateValue := <-called:
		if stateValue.ServiceManager != install.ServiceManagerSystemdUser {
			t.Fatalf("ServiceManager = %q, want %q", stateValue.ServiceManager, install.ServiceManagerSystemdUser)
		}
		if stateValue.StatePath != statePath {
			t.Fatalf("StatePath = %q, want %q", stateValue.StatePath, statePath)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for daemon restart hook")
	}
	waitForRepairOperation(t, gateway, "已向托管 lifecycle manager 发起 daemon 重启")
	if len(gateway.upserted) != 0 {
		t.Fatalf("repair daemon must not reconnect Feishu runtime, got %#v", gateway.upserted)
	}
}

func TestRepairDaemonCommandReportsUnsupportedDetachedDaemon(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app := newRepairTestApp(t, gateway)
	app.service.MaterializeSurface("surface-1", "main", "chat-1", "user-1")
	statePath := filepath.Join(app.headlessRuntime.Paths.DataDir, "install-state.json")
	if err := install.WriteState(statePath, install.InstallState{
		StatePath:       statePath,
		ConfigPath:      filepath.Join(app.headlessRuntime.Paths.ConfigDir, "config.json"),
		ServiceManager:  install.ServiceManagerDetached,
		InstalledBinary: "/bin/codex-remote",
	}); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	called := make(chan struct{}, 1)
	app.restartDaemon = func(_ context.Context, stateValue install.InstallState) error {
		called <- struct{}{}
		return install.RestartInstalledDaemon(context.Background(), stateValue)
	}
	app.sendAgentCommand = func(string, agentproto.Command) error {
		t.Fatal("repair daemon must not restart provider child")
		return nil
	}

	events := app.handleDaemonCommand(control.DaemonCommand{
		Kind:             control.DaemonCommandRepair,
		GatewayID:        "main",
		SurfaceSessionID: "surface-1",
		Text:             "/repair daemon",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "repair_daemon_restart_started" {
		t.Fatalf("expected daemon restart started notice, got %#v", events)
	}

	waitForRepairOperation(t, gateway, "无法从飞书重启 daemon")
	waitForRepairOperation(t, gateway, "server/supervisor")
	select {
	case <-called:
		t.Fatal("repair daemon must not call restart hook for detached manager")
	default:
	}
	if len(gateway.upserted) != 0 {
		t.Fatalf("repair daemon must not reconnect Feishu runtime, got %#v", gateway.upserted)
	}
}

func newRepairTestApp(t *testing.T, gateway *fakeAdminGatewayController) *App {
	t.Helper()
	if gateway == nil {
		gateway = &fakeAdminGatewayController{}
	}
	dir := t.TempDir()
	cfg := config.DefaultAppConfig()
	enabled := true
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main Bot",
		AppID:     "cli_main",
		AppSecret: "secret",
		Enabled:   &enabled,
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		Paths: relayruntime.Paths{
			DataDir:  filepath.Join(dir, "data"),
			StateDir: filepath.Join(dir, "state"),
			LogsDir:  filepath.Join(dir, "logs"),
		},
	})
	return app
}

func waitForRepairGatewayUpsert(t *testing.T, gateway *fakeAdminGatewayController, gatewayID string) {
	t.Helper()
	waitForRepairCondition(t, "feishu runtime reconnect", func() bool {
		for _, app := range gateway.upserted {
			if strings.TrimSpace(app.GatewayID) == gatewayID {
				return true
			}
		}
		return false
	})
}

func waitForRepairOperation(t *testing.T, gateway *fakeAdminGatewayController, bodyText string) {
	t.Helper()
	waitForRepairCondition(t, "repair result operation", func() bool {
		for _, op := range gateway.applied {
			if op.CardTitle == "Repair" && strings.Contains(op.CardBody, bodyText) {
				return true
			}
		}
		return false
	})
}

func waitForRepairCondition(t *testing.T, label string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", label)
}
