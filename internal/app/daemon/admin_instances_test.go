package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestCreateManagedHeadlessInstanceSetsExplicitDaemonOwnedLifetime(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	workspaceRoot := t.TempDir()

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	summary, err := app.createManagedHeadlessInstance(workspaceRoot, "Alpha")
	if err != nil {
		t.Fatalf("createManagedHeadlessInstance: %v", err)
	}
	if summary.InstanceID == "" || captured.InstanceID != summary.InstanceID {
		t.Fatalf("expected launched instance id to match summary, got summary=%#v launch=%#v", summary, captured)
	}
	if !testutil.SamePath(captured.WorkDir, workspaceRoot) {
		t.Fatalf("expected managed headless workdir = %q, got %#v", workspaceRoot, captured)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_SOURCE=headless") ||
		!containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_MANAGED=1") ||
		!containsEnvEntry(captured.Env, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected explicit daemon-owned managed headless env, got %#v", captured.Env)
	}
	if captured.LaunchMode != relayruntime.HeadlessLaunchModeAppServer {
		t.Fatalf("expected admin-managed headless to default to codex app-server mode, got %#v", captured)
	}
}

func newManagedInstancesAdminTestApp(t *testing.T) *App {
	t.Helper()

	cfg := config.DefaultAppConfig()
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Paths: relayruntime.Paths{
			StateDir: t.TempDir(),
			LogsDir:  t.TempDir(),
		},
		KillGrace: time.Second,
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	return app
}
