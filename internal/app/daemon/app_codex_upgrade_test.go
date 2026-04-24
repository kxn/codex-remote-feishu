package daemon

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade"
	codexupgraderuntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/codexupgraderuntime"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCheckStandaloneCodexUpgradeAllowsRepeatedChecks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, packageJSONPath := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	first, err := app.checkStandaloneCodexUpgrade(context.Background(), "")
	if err != nil {
		t.Fatalf("checkStandaloneCodexUpgrade first: %v", err)
	}
	second, err := app.checkStandaloneCodexUpgrade(context.Background(), "")
	if err != nil {
		t.Fatalf("checkStandaloneCodexUpgrade second: %v", err)
	}
	if !first.HasUpdate || !first.CanUpgrade || !second.HasUpdate || !second.CanUpgrade {
		t.Fatalf("expected repeated checks to stay upgradeable, first=%#v second=%#v", first, second)
	}

	app.service.Instance("inst-1").ActiveTurnID = "turn-1"
	blocked, err := app.checkStandaloneCodexUpgrade(context.Background(), "")
	if err != nil {
		t.Fatalf("checkStandaloneCodexUpgrade blocked: %v", err)
	}
	if blocked.CanUpgrade || len(blocked.BusyReasons) == 0 {
		t.Fatalf("expected busy reasons after active turn, got %#v", blocked)
	}

	if raw, err := os.ReadFile(packageJSONPath); err != nil || !strings.Contains(string(raw), "\"0.123.0\"") {
		t.Fatalf("package version changed unexpectedly: err=%v raw=%q", err, string(raw))
	}
}

func TestStandaloneCodexUpgradeQueuesOtherSurfaceInputUntilFinish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, packageJSONPath := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	installStarted := make(chan struct{})
	releaseInstall := make(chan struct{})
	app.codexUpgradeRuntime.Install = func(_ context.Context, _ codexupgrade.Installation, version string) error {
		close(installStarted)
		<-releaseInstall
		return writeStandaloneCodexPackageVersion(packageJSONPath, version)
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", CWD: "/tmp/workspace", Loaded: true},
		},
	})
	attachStandaloneCodexTestSurface(app, "surface-init", "chat-init", "user-init", "inst-1")
	attachStandaloneCodexTestSurface(app, "surface-other", "chat-other", "user-other", "inst-1")

	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	done := make(chan error, 1)
	if err := app.startStandaloneCodexUpgrade(context.Background(), codexUpgradeStartRequest{
		SurfaceSessionID: "surface-init",
		ActorUserID:      "user-init",
		OnComplete: func(err error) {
			done <- err
		},
	}); err != nil {
		t.Fatalf("startStandaloneCodexUpgrade: %v", err)
	}

	select {
	case <-installStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for install hook to start")
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-other",
		ChatID:           "chat-other",
		ActorUserID:      "user-other",
		MessageID:        "msg-queued",
		Text:             "继续执行",
	})

	surface := app.service.Surface("surface-other")
	if surface == nil || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected one queued item during upgrade, got %#v", surface)
	}
	if len(sent) != 0 {
		t.Fatalf("expected no commands dispatched before upgrade finishes, got %#v", sent)
	}

	close(releaseInstall)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgrade completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade completion")
	}

	if app.codexUpgradeRuntime.Active != nil {
		t.Fatalf("expected runtime transaction to clear, got %#v", app.codexUpgradeRuntime.Active)
	}
	if len(sent) != 2 {
		t.Fatalf("expected restart plus queued prompt dispatch, got %#v", sent)
	}
	if sent[0].Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("first command kind = %q, want child restart", sent[0].Kind)
	}
	if sent[1].Kind != agentproto.CommandPromptSend {
		t.Fatalf("second command kind = %q, want prompt send", sent[1].Kind)
	}
}

func TestStandaloneCodexUpgradeBlocksInitiatorInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, packageJSONPath := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	installStarted := make(chan struct{})
	releaseInstall := make(chan struct{})
	app.codexUpgradeRuntime.Install = func(_ context.Context, _ codexupgrade.Installation, version string) error {
		close(installStarted)
		<-releaseInstall
		return writeStandaloneCodexPackageVersion(packageJSONPath, version)
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurface(app, "surface-init", "chat-init", "user-init", "inst-1")

	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	done := make(chan error, 1)
	if err := app.startStandaloneCodexUpgrade(context.Background(), codexUpgradeStartRequest{
		SurfaceSessionID: "surface-init",
		ActorUserID:      "user-init",
		OnComplete: func(err error) {
			done <- err
		},
	}); err != nil {
		t.Fatalf("startStandaloneCodexUpgrade: %v", err)
	}
	select {
	case <-installStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for install hook to start")
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-init",
		ChatID:           "chat-init",
		ActorUserID:      "user-init",
		MessageID:        "msg-blocked",
		Text:             "不要排队",
	})

	surface := app.service.Surface("surface-init")
	if surface == nil || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected initiator input to stay blocked, got %#v", surface)
	}
	if len(sent) != 0 {
		t.Fatalf("expected no prompt command while input is blocked, got %#v", sent)
	}

	close(releaseInstall)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgrade completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade completion")
	}
}

func TestCheckStandaloneCodexUpgradeIgnoresVSCodeBusyState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, _ := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless",
		WorkspaceRoot: "/tmp/workspace-headless",
		WorkspaceKey:  "/tmp/workspace-headless",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode",
		WorkspaceRoot: "/tmp/workspace-vscode",
		WorkspaceKey:  "/tmp/workspace-vscode",
		Source:        "vscode",
		Online:        true,
		ActiveTurnID:  "turn-vscode",
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-vscode", "chat-vscode", "user-vscode", "inst-vscode", state.ProductModeVSCode)
	app.service.Surface("surface-vscode").ActiveQueueItemID = "queue-vscode"

	check, err := app.checkStandaloneCodexUpgrade(context.Background(), "")
	if err != nil {
		t.Fatalf("checkStandaloneCodexUpgrade: %v", err)
	}
	if !check.HasUpdate || !check.CanUpgrade {
		t.Fatalf("expected vscode activity to be ignored for standalone upgrade, got %#v", check)
	}
	if len(check.BusyReasons) != 0 {
		t.Fatalf("expected no busy reasons from vscode activity, got %#v", check.BusyReasons)
	}
}

func TestStandaloneCodexUpgradeRestartsOnlyAffectedInstances(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, packageJSONPath := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	app.codexUpgradeRuntime.Install = func(_ context.Context, _ codexupgrade.Installation, version string) error {
		return writeStandaloneCodexPackageVersion(packageJSONPath, version)
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless",
		WorkspaceRoot: "/tmp/workspace-headless",
		WorkspaceKey:  "/tmp/workspace-headless",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode",
		WorkspaceRoot: "/tmp/workspace-vscode",
		WorkspaceKey:  "/tmp/workspace-vscode",
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-init", "chat-init", "user-init", "inst-headless", state.ProductModeNormal)

	var targets []string
	var commands []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		targets = append(targets, instanceID)
		commands = append(commands, command)
		return nil
	}

	done := make(chan error, 1)
	if err := app.startStandaloneCodexUpgrade(context.Background(), codexUpgradeStartRequest{
		SurfaceSessionID: "surface-init",
		ActorUserID:      "user-init",
		OnComplete: func(err error) {
			done <- err
		},
	}); err != nil {
		t.Fatalf("startStandaloneCodexUpgrade: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgrade completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade completion")
	}

	if len(commands) != 1 {
		t.Fatalf("expected exactly one restart command, got targets=%#v commands=%#v", targets, commands)
	}
	if targets[0] != "inst-headless" {
		t.Fatalf("restart target = %q, want inst-headless", targets[0])
	}
	if commands[0].Kind != agentproto.CommandProcessChildRestart {
		t.Fatalf("command kind = %q, want child restart", commands[0].Kind)
	}
}

func TestStandaloneCodexUpgradePausesOnlyAffectedSurfaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test fixture uses unix shell scripts")
	}

	app, packageJSONPath := newStandaloneCodexUpgradeTestApp(t)
	app.codexUpgradeRuntime.LatestLookup = func(context.Context) (string, error) {
		return "0.124.0", nil
	}
	installStarted := make(chan struct{})
	releaseInstall := make(chan struct{})
	app.codexUpgradeRuntime.Install = func(_ context.Context, _ codexupgrade.Installation, version string) error {
		close(installStarted)
		<-releaseInstall
		return writeStandaloneCodexPackageVersion(packageJSONPath, version)
	}

	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless",
		WorkspaceRoot: "/tmp/workspace-headless",
		WorkspaceKey:  "/tmp/workspace-headless",
		Source:        "headless",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode",
		WorkspaceRoot: "/tmp/workspace-vscode",
		WorkspaceKey:  "/tmp/workspace-vscode",
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-init", "chat-init", "user-init", "inst-headless", state.ProductModeNormal)
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-normal", "chat-normal", "user-normal", "inst-headless", state.ProductModeNormal)
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-vscode", "chat-vscode", "user-vscode", "inst-vscode", state.ProductModeVSCode)
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	done := make(chan error, 1)
	if err := app.startStandaloneCodexUpgrade(context.Background(), codexUpgradeStartRequest{
		SurfaceSessionID: "surface-init",
		ActorUserID:      "user-init",
		OnComplete: func(err error) {
			done <- err
		},
	}); err != nil {
		t.Fatalf("startStandaloneCodexUpgrade: %v", err)
	}

	select {
	case <-installStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for install hook to start")
	}

	if got := app.service.Surface("surface-normal").DispatchMode; got != state.DispatchModePausedForLocal {
		t.Fatalf("surface-normal dispatch mode = %q, want paused_for_local", got)
	}
	if got := app.service.Surface("surface-vscode").DispatchMode; got != state.DispatchModeNormal {
		t.Fatalf("surface-vscode dispatch mode = %q, want normal", got)
	}

	close(releaseInstall)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgrade completion: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upgrade completion")
	}

	if got := app.service.Surface("surface-normal").DispatchMode; got != state.DispatchModeNormal {
		t.Fatalf("surface-normal dispatch mode after resume = %q, want normal", got)
	}
	if got := app.service.Surface("surface-vscode").DispatchMode; got != state.DispatchModeNormal {
		t.Fatalf("surface-vscode dispatch mode after resume = %q, want normal", got)
	}
}

func TestStandaloneCodexUpgradeIngressBypassesVSCodeSurface(t *testing.T) {
	app := New(":0", ":0", newLifecycleGateway(), agentproto.ServerIdentity{})
	app.codexUpgradeRuntime.Active = &codexupgraderuntime.Transaction{
		ID:               "codex-upgrade-1",
		InitiatorSurface: "surface-init",
		PausedSurfaceIDs: map[string]bool{},
	}
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode",
		WorkspaceRoot: "/tmp/workspace-vscode",
		WorkspaceKey:  "/tmp/workspace-vscode",
		Source:        "vscode",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	attachStandaloneCodexTestSurfaceWithMode(app, "surface-vscode", "chat-vscode", "user-vscode", "inst-vscode", state.ProductModeVSCode)

	app.mu.Lock()
	handled := app.maybeHandleStandaloneCodexUpgradeActionLocked(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-vscode",
		ActorUserID:      "user-vscode",
		Text:             "continue",
	})
	app.mu.Unlock()

	if handled {
		t.Fatal("expected standalone codex upgrade ingress to ignore vscode surface input")
	}
}

func newStandaloneCodexUpgradeTestApp(t *testing.T) (*App, string) {
	t.Helper()

	root := t.TempDir()
	packageJSONPath, fakeBin := writeStandaloneCodexFixture(t, root, "0.123.0")
	t.Setenv("PATH", fakeBin)

	app := New(":0", ":0", newLifecycleGateway(), agentproto.ServerIdentity{})
	cfg := config.DefaultAppConfig()
	cfg.Wrapper.CodexRealBinary = "codex"
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{
			Path:   filepath.Join(root, "config.json"),
			Config: cfg,
		}, nil
	}
	return app, packageJSONPath
}

func writeStandaloneCodexFixture(t *testing.T, root, version string) (string, string) {
	t.Helper()

	packageRoot := filepath.Join(root, "node", "lib", "node_modules", "@openai", "codex")
	packageBin := filepath.Join(packageRoot, "bin", "codex.js")
	if err := os.MkdirAll(filepath.Dir(packageBin), 0o755); err != nil {
		t.Fatalf("MkdirAll package bin: %v", err)
	}
	packageJSONPath := filepath.Join(packageRoot, "package.json")
	if err := writeStandaloneCodexPackageVersion(packageJSONPath, version); err != nil {
		t.Fatalf("write package version: %v", err)
	}
	if err := os.WriteFile(packageBin, []byte("#!/usr/bin/env node\n"), 0o755); err != nil {
		t.Fatalf("WriteFile package bin: %v", err)
	}

	fakeBin := filepath.Join(root, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o755); err != nil {
		t.Fatalf("MkdirAll fake bin: %v", err)
	}
	if err := os.Symlink(packageBin, filepath.Join(fakeBin, "codex")); err != nil {
		t.Fatalf("Symlink codex: %v", err)
	}
	npmScript := "#!/bin/sh\n" +
		"if [ \"$1\" = \"config\" ] && [ \"$2\" = \"get\" ] && [ \"$3\" = \"prefix\" ]; then\n" +
		"  printf '%s\\n' \"" + filepath.Join(root, "node") + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"root\" ] && [ \"$2\" = \"-g\" ]; then\n" +
		"  printf '%s\\n' \"" + filepath.Join(root, "node", "lib", "node_modules") + "\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "npm"), []byte(npmScript), 0o755); err != nil {
		t.Fatalf("WriteFile npm: %v", err)
	}
	return packageJSONPath, fakeBin
}

func writeStandaloneCodexPackageVersion(path, version string) error {
	return os.WriteFile(path, []byte("{\"name\":\"@openai/codex\",\"version\":\""+version+"\",\"bin\":{\"codex\":\"bin/codex.js\"}}\n"), 0o644)
}

func attachStandaloneCodexTestSurface(app *App, surfaceID, chatID, actorUserID, instanceID string) {
	attachStandaloneCodexTestSurfaceWithMode(app, surfaceID, chatID, actorUserID, instanceID, state.ProductModeNormal)
}

func attachStandaloneCodexTestSurfaceWithMode(app *App, surfaceID, chatID, actorUserID, instanceID string, mode state.ProductMode) {
	app.service.MaterializeSurface(surfaceID, "", chatID, actorUserID)
	surface := app.service.Surface(surfaceID)
	if surface == nil {
		return
	}
	surface.ProductMode = state.NormalizeProductMode(mode)
	surface.AttachedInstanceID = instanceID
	surface.RouteMode = state.RouteModeUnbound
}
