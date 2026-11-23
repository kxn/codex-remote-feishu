package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

// Uses production composition in entry order. Only the platform writes and
// native process launcher are replaced; no application router/reducer is faked.
func newApplicationRuntimeTestApp(t *testing.T) (*App, *messageIDAssigningGateway) {
	app, gateway, _ := newApplicationRuntimeTestAppWithLock(t)
	return app, gateway
}
func newApplicationRuntimeTestAppWithLock(t *testing.T) (*App, *messageIDAssigningGateway, *relayruntime.FileLock) {
	t.Helper()
	root := t.TempDir()
	paths := relayruntime.Paths{StateDir: root, DataDir: root, LogsDir: root}
	paths.ConfigFile = filepath.Join(root, "config.json")
	paths.DaemonLockFile = filepath.Join(root, "daemon.lock")
	cfg := config.DefaultAppConfig()
	enabled := true
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "test-secret-not-a-live-credential", Enabled: &enabled}}
	if err := config.WriteAppConfig(paths.ConfigFile, cfg); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureTeamLegacyFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := relayruntime.AcquireLock(t.Context(), paths.DaemonLockFile, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Release() })
	gateway := &messageIDAssigningGateway{notify: make(chan struct{}, 16)}
	app := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = app.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Error("unexpected native process launch")
		return 0, context.Canceled
	}
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{LegacyHistoryOnly: true, Paths: paths, ConfigPath: paths.ConfigFile, BinaryPath: filepath.Join(root, "test-relayd")})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: paths.ConfigFile, Services: defaultFeishuServices()})
	if err = app.configureWorkRuntime(t.Context(), teamPrivateCommands{app: app}, app.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := app.applications.stopWorkers(ctx); err != nil {
			t.Error(err)
		}
		if err := app.applications.close(); err != nil {
			t.Error(err)
		}
		if err := app.teamRuntime.store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = app.activateWorkRuntime(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	controller := feishu.NewMultiGatewayController()
	if err = app.configureApplicationIngress(t.Context(), controller, lock); err != nil {
		t.Fatal(err)
	}
	return app, gateway, lock
}

func TestApplicationCompositionRoutesFreshPrivateMenuWithoutTeamWork(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	if app.service != r.core.Service() || app.privateTools == nil || r.native.startHeadless == nil {
		t.Fatal("application dependencies not wired")
	}
	if app.gatewayRuntimeHooks().RawIngressHook != r.hook {
		t.Fatal("gateway has wrong hook")
	}
	if !app.teamRuntime.legacyFenced.Load() {
		t.Fatal("old business writer still live")
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "sdk-menu", MessageID: "menu-message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if allowed, err := r.hook.Authorize(t.Context(), raw); err != nil || !allowed {
		t.Fatalf("authorize=%v err=%v", allowed, err)
	}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errors); err != nil {
		t.Fatal(err)
	}
	ops := gateway.waitForOperationCount(1, 3*time.Second)
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].ReplyToMessageID != "" || ops[0].MessageID == "" {
		t.Fatalf("private menu=%+v", ops)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	default:
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := r.stopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
}
