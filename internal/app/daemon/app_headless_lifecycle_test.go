package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	headlessruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/headlessruntime"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/shutdownctx"
)

func TestDaemonStartsPreselectedHeadlessForGlobalThreadUse(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		BaseEnv:    []string{"PATH=/usr/bin"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
		},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected pending preselected headless snapshot, got %#v", snapshot)
	}
	if captured.WorkDir != "/data/dl/droid" || captured.InstanceID != snapshot.PendingHeadless.InstanceID {
		t.Fatalf("unexpected preselected headless launch opts: %#v", captured)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected explicit daemon-owned lifetime for managed headless launch, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=headless") {
		t.Fatalf("did not expect default headless display override when thread cwd is known, got %#v", captured.Env)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    snapshot.PendingHeadless.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID == "" || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected preselected headless hello to auto-attach target thread, got %#v", snapshot)
	}
}

func TestDaemonStartsClaudeHeadlessWithBackendEnv(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		BaseEnv:    []string{"PATH=/usr/bin"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
		},
	})
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline-claude",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendClaude,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4322, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-claude",
	})

	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_BACKEND=claude") {
		t.Fatalf("expected claude backend env for managed headless launch, got %#v", captured.Env)
	}
}

func TestDaemonKillInstanceStopsManagedHeadlessProcess(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Hour, KillGrace: time.Second})

	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           4321,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	app.managedHeadlessRuntime.Processes["inst-headless-1"] = &headlessruntime.Process{InstanceID: "inst-headless-1", PID: 4321, StartedAt: time.Now()}
	app.service.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-headless-1"})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if stoppedPID != 0 {
		t.Fatalf("expected detach not to stop managed headless pid, got %d", stoppedPID)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected surface to detach after detach, got %#v", snapshot)
	}
	if app.service.Instance("inst-headless-1") == nil {
		t.Fatalf("expected managed headless instance to remain after detach")
	}
}

func TestDaemonIdleHeadlessCleanupStopsDetachedManagedInstance(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{IdleTTL: time.Minute, KillGrace: time.Second, MinIdle: 0})

	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-2",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})

	base := time.Now().UTC()
	app.onTick(context.Background(), base)
	app.onTick(context.Background(), base.Add(2*time.Minute))

	if stoppedPID != 2468 {
		t.Fatalf("expected idle managed headless pid to stop, got %d", stoppedPID)
	}
	if app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected idle managed headless instance to be removed, got %#v", app.service.Instance("inst-headless-2"))
	}
}

func TestDaemonIdleManagedHeadlessRefreshesOnInterval(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:             time.Hour,
		KillGrace:           time.Second,
		IdleRefreshInterval: 5 * time.Minute,
		IdleRefreshTimeout:  time.Minute,
	})

	var commands []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-headless-2" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
		return nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-2",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected initial hello refresh, got %#v", commands)
	}

	app.onEvents(context.Background(), "inst-headless-2", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
	}})
	base := time.Now().UTC()
	app.managedHeadlessRuntime.Processes["inst-headless-2"].LastRefreshCompletedAt = base
	app.managedHeadlessRuntime.Processes["inst-headless-2"].RefreshInFlight = false
	app.managedHeadlessRuntime.Processes["inst-headless-2"].RefreshCommandID = ""

	app.onTick(context.Background(), base.Add(2*time.Minute))
	if len(commands) != 1 {
		t.Fatalf("expected no idle refresh before interval, got %#v", commands)
	}

	app.onTick(context.Background(), base.Add(6*time.Minute))
	if len(commands) != 2 || commands[1].Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected scheduled idle refresh after interval, got %#v", commands)
	}
	if managed := app.managedHeadlessRuntime.Processes["inst-headless-2"]; managed == nil || managed.Status != headlessruntime.StatusIdle || !managed.RefreshInFlight {
		t.Fatalf("expected idle managed headless to track in-flight refresh, got %#v", managed)
	}
}

func TestDaemonShutdownStopsManagedHeadlessAndRemovesRuntimeState(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{KillGrace: time.Second})

	var stopped []int
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopped = append(stopped, pid)
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           4321,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.managedHeadlessRuntime.Processes["inst-headless-1"] = &headlessruntime.Process{
		InstanceID:    "inst-headless-1",
		PID:           4321,
		WorkspaceRoot: "/data/dl/droid",
		DisplayName:   "headless",
		Status:        headlessruntime.StatusBusy,
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != 4321 {
		t.Fatalf("expected managed headless pid 4321 to stop, got %#v", stopped)
	}
	if len(app.managedHeadlessRuntime.Processes) != 0 {
		t.Fatalf("expected managed headless map cleared, got %#v", app.managedHeadlessRuntime.Processes)
	}
	if app.service.Instance("inst-headless-1") != nil {
		t.Fatalf("expected managed headless instance removed from service, got %#v", app.service.Instance("inst-headless-1"))
	}
}

func TestDaemonShutdownRequestsConnectedWrapperExitAndSkipsSecondKill(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.shutdownDrainTimeout = 150 * time.Millisecond
	app.shutdownDrainPoll = 5 * time.Millisecond

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-live",
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           3456,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.managedHeadlessRuntime.Processes["inst-headless-live"] = &headlessruntime.Process{
		InstanceID: "inst-headless-live",
		PID:        3456,
		Status:     headlessruntime.StatusBusy,
	}
	app.rememberRelayConnectionWithPID("inst-headless-live", 7, 3456)

	var commands []agentproto.Command
	var stopped []int
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-headless-live" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
		if command.Kind == agentproto.CommandProcessExit {
			go func() {
				time.Sleep(20 * time.Millisecond)
				app.markRelayConnectionDropped("inst-headless-live", 7)
			}()
		}
		return nil
	}
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopped = append(stopped, pid)
		return nil
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(commands) != 1 || commands[0].Kind != agentproto.CommandProcessExit {
		t.Fatalf("expected one process.exit command, got %#v", commands)
	}
	if len(stopped) != 0 {
		t.Fatalf("expected relay drain to avoid second stopProcess call, got %#v", stopped)
	}
	if len(app.managedHeadlessRuntime.Processes) != 0 {
		t.Fatalf("expected managed headless map cleared, got %#v", app.managedHeadlessRuntime.Processes)
	}
	if app.service.Instance("inst-headless-live") != nil {
		t.Fatalf("expected managed headless service state removed, got %#v", app.service.Instance("inst-headless-live"))
	}
}

func TestDaemonShutdownSkipsVSCodeWrapperConnections(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.shutdownDrainTimeout = 25 * time.Millisecond
	app.shutdownDrainPoll = 5 * time.Millisecond

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-live",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "vscode",
		Managed:       false,
		PID:           4567,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.rememberRelayConnectionWithPID("inst-vscode-live", 9, 4567)

	var commands []agentproto.Command
	stopCalled := false
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-vscode-live" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
		return nil
	}
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopCalled = true
		return nil
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("expected vscode wrapper to be skipped during shutdown drain, got %#v", commands)
	}
	if stopCalled {
		t.Fatal("expected vscode wrapper pid not to be force-stopped by daemon shutdown")
	}
}

func TestDaemonShutdownForConsoleCloseSkipsGatewayWait(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	cancelled := false
	gatewayDone := make(chan struct{})
	app.setGatewayRuntime(func() { cancelled = true }, gatewayDone)

	ctx, setMode := shutdownctx.WithHolder(context.Background())
	setMode(shutdownctx.ModeConsoleClose)

	started := time.Now()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if !cancelled {
		t.Fatal("expected gateway runtime cancel to be called")
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("expected console-close shutdown not to wait for gateway, took %s", elapsed)
	}
}

func TestDaemonShutdownContinuesManagedHeadlessCleanupAfterStopError(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{KillGrace: time.Second})

	var stopped []int
	app.stopProcess = func(pid int, _ time.Duration) error {
		stopped = append(stopped, pid)
		if pid == 1111 {
			return errors.New("terminate failed")
		}
		return nil
	}

	for _, inst := range []*state.InstanceRecord{
		{
			InstanceID:    "inst-headless-1",
			DisplayName:   "headless-1",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			Source:        "headless",
			Managed:       true,
			PID:           1111,
			Threads:       map[string]*state.ThreadRecord{},
		},
		{
			InstanceID:    "inst-headless-2",
			DisplayName:   "headless-2",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			Source:        "headless",
			Managed:       true,
			PID:           2222,
			Threads:       map[string]*state.ThreadRecord{},
		},
	} {
		app.service.UpsertInstance(inst)
	}
	app.managedHeadlessRuntime.Processes["inst-headless-1"] = &headlessruntime.Process{InstanceID: "inst-headless-1", PID: 1111}
	app.managedHeadlessRuntime.Processes["inst-headless-2"] = &headlessruntime.Process{InstanceID: "inst-headless-2", PID: 2222}

	err := app.Shutdown(context.Background())
	if err == nil || !strings.Contains(err.Error(), "inst-headless-1") {
		t.Fatalf("expected shutdown cleanup error for first managed headless, got %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("expected both managed headless processes to be attempted, got %#v", stopped)
	}
	if len(app.managedHeadlessRuntime.Processes) != 0 {
		t.Fatalf("expected managed headless map cleared after cleanup, got %#v", app.managedHeadlessRuntime.Processes)
	}
	if app.service.Instance("inst-headless-1") != nil || app.service.Instance("inst-headless-2") != nil {
		t.Fatalf("expected managed headless service state removed after cleanup, got %#v %#v", app.service.Instance("inst-headless-1"), app.service.Instance("inst-headless-2"))
	}
}

func TestDaemonPrewarmsManagedHeadlessToMinIdle(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	var launches []relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches = append(launches, opts)
		return 5000 + len(launches), nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		MinIdle: 1,
	})

	now := time.Now().UTC()
	app.onTick(context.Background(), now)
	if len(launches) != 1 {
		t.Fatalf("expected one prewarm launch, got %#v", launches)
	}
	if launches[0].WorkDir != stateDir {
		t.Fatalf("expected prewarm workdir to use state dir, got %#v", launches[0])
	}
	if !containsEnvEntry(launches[0].Env, "CODEX_REMOTE_INSTANCE_SOURCE=headless") ||
		!containsEnvEntry(launches[0].Env, "CODEX_REMOTE_INSTANCE_MANAGED=1") ||
		!containsEnvEntry(launches[0].Env, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected managed headless prewarm env, got %#v", launches[0].Env)
	}
	if len(app.managedHeadlessRuntime.Processes) != 1 {
		t.Fatalf("expected one managed headless record, got %#v", app.managedHeadlessRuntime.Processes)
	}
	for _, managed := range app.managedHeadlessRuntime.Processes {
		if managed.Status != headlessruntime.StatusStarting || managed.WorkspaceRoot != stateDir || managed.DisplayName != "headless" {
			t.Fatalf("unexpected prewarmed managed headless record: %#v", managed)
		}
	}

	app.onTick(context.Background(), now.Add(10*time.Second))
	if len(launches) != 1 {
		t.Fatalf("expected fresh starting instance to count toward min-idle, got %#v", launches)
	}
}

func TestDaemonPrewarmsReplacementWhenOfflineManagedHeadlessDoesNotCount(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	var launches []relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches = append(launches, opts)
		return 6000 + len(launches), nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		StartTTL: time.Minute,
		MinIdle:  1,
	})

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-old",
			DisplayName:   "old",
			WorkspaceRoot: stateDir,
			WorkspaceKey:  stateDir,
			ShortName:     "old",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})
	app.onDisconnect(context.Background(), "inst-headless-old")

	app.onTick(context.Background(), time.Now().UTC())
	if len(launches) != 1 {
		t.Fatalf("expected offline managed headless to trigger replacement prewarm, got %#v", launches)
	}
	if len(app.managedHeadlessRuntime.Processes) != 2 {
		t.Fatalf("expected offline member to remain visible alongside replacement, got %#v", app.managedHeadlessRuntime.Processes)
	}
	if app.managedHeadlessRuntime.Processes["inst-headless-old"] == nil || app.managedHeadlessRuntime.Processes["inst-headless-old"].Status != headlessruntime.StatusOffline {
		t.Fatalf("expected original member to stay offline, got %#v", app.managedHeadlessRuntime.Processes["inst-headless-old"])
	}
}

func TestDaemonPrewarmLaunchFailureRollsBackReservation(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	launches := 0
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches++
		if launches == 1 {
			return 0, errors.New("boom")
		}
		return 7000 + launches, nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		MinIdle: 1,
	})

	base := time.Now().UTC()
	app.onTick(context.Background(), base)
	if launches != 1 {
		t.Fatalf("expected one failed prewarm launch, got %d", launches)
	}
	if len(app.managedHeadlessRuntime.Processes) != 0 {
		t.Fatalf("expected failed prewarm reservation to be rolled back, got %#v", app.managedHeadlessRuntime.Processes)
	}

	app.onTick(context.Background(), base.Add(time.Minute))
	if launches != 2 {
		t.Fatalf("expected second tick to retry prewarm launch, got %d launches", launches)
	}
	if len(app.managedHeadlessRuntime.Processes) != 1 {
		t.Fatalf("expected successful retry to leave one managed headless record, got %#v", app.managedHeadlessRuntime.Processes)
	}
}

func TestDaemonPrewarmReservationBlocksDuplicateLaunchWhileStartInFlight(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	launches := 0
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		launches++
		if launches == 1 {
			close(startEntered)
			<-releaseStart
		}
		return 8000 + launches, nil
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: "/tmp/config.json",
		Paths: relayruntime.Paths{
			StateDir: stateDir,
			LogsDir:  t.TempDir(),
		},
		MinIdle: 1,
	})

	base := time.Now().UTC()
	firstTickDone := make(chan struct{})
	go func() {
		app.onTick(context.Background(), base)
		close(firstTickDone)
	}()

	<-startEntered
	if len(app.managedHeadlessRuntime.Processes) != 1 {
		t.Fatalf("expected reserved prewarm slot while launch is in flight, got %#v", app.managedHeadlessRuntime.Processes)
	}

	app.onTick(context.Background(), base.Add(5*time.Second))
	if launches != 1 {
		t.Fatalf("expected in-flight prewarm reservation to block duplicate launch, got %d launches", launches)
	}

	close(releaseStart)
	<-firstTickDone
	if len(app.managedHeadlessRuntime.Processes) != 1 {
		t.Fatalf("expected one managed headless record after launch settles, got %#v", app.managedHeadlessRuntime.Processes)
	}
}
