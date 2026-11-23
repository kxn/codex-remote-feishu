package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateHostLaunchFreezesRealProfileNamespaceAndCWD(t *testing.T) {
	ctx := t.Context()
	app := newManagedInstancesAdminTestApp(t)
	root := app.headlessRuntime.Paths.StateDir
	cwd := t.TempDir()
	home := t.TempDir()
	app.headlessRuntime.BaseEnv = []string{"HOME=" + home, "APPROVAL_SETTING=unchanged"}
	host := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	host.store, err = openPrivateNativeHostStore(filepath.Join(root, "private-host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.store.Close()
	host.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), host)
	if err != nil {
		t.Fatal(err)
	}
	defer host.registry.Close()
	host.outbound, err = executionowner.NewOutbound(host.registry)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := nativeHistoryNamespace(agentproto.BackendClaude, app.headlessRuntime.BaseEnv, cwd, "host")
	if err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendClaude, HistoryNamespace: ns}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	var got relayruntime.HeadlessLaunchOptions
	host.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) { got = v; return os.Getpid(), nil }
	if _, err = app.launchPrivateNative(ctx, host, privateNativeLaunchRequest{Record: rec, MigrationResume: &privatechat.MigrationResumeIntent{ConnectionIdentity: "forged-source-account"}}); err == nil {
		t.Fatal("migration with unverified account spawned")
	}
	if got.InstanceID != "" {
		t.Fatal("launcher called before migration identity verification")
	}
	if _, err = app.launchPrivateNative(ctx, host, privateNativeLaunchRequest{Record: rec}); err != nil {
		t.Fatal(err)
	}
	if got.WorkDir != cwd || got.LaunchMode != relayruntime.HeadlessLaunchModeClaudeAppServer {
		t.Fatal("wrong launch mode or working directory")
	}
	var secret string
	for _, entry := range got.Env {
		if strings.HasPrefix(entry, agentproto.PrivateNativeCredentialEnv+"=") {
			secret = strings.TrimPrefix(entry, agentproto.PrivateNativeCredentialEnv+"=")
		}
	}
	if secret == "" || !containsEnvEntry(got.Env, "APPROVAL_SETTING=unchanged") {
		t.Fatal("credential or inherited setting missing")
	}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: 1, CarrierIncarnation: "inc", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendClaude}}
	if err = host.store.authorize(hello, secret); err != nil {
		t.Fatal(err)
	}
	stored, err := host.store.lookup(ctx, "instance")
	if err != nil || stored.State != "running" || stored.Process.PID != os.Getpid() {
		t.Fatalf("launch not recorded: %+v %v", stored, err)
	}
}

func TestPrivatePreparedLaunchRecoversFrozenCredentialAndRejectsChangedConfig(t *testing.T) {
	ctx := t.Context()
	app := newManagedInstancesAdminTestApp(t)
	root := app.headlessRuntime.Paths.StateDir
	cwd := t.TempDir()
	home := t.TempDir()
	app.headlessRuntime.BaseEnv = []string{"HOME=" + home, "APPROVAL_SETTING=original"}
	host := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	host.store, err = openPrivateNativeHostStore(filepath.Join(root, "private-host.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.store.Close()
	host.registry, err = executionowner.Open(filepath.Join(root, "owners.db"), host)
	if err != nil {
		t.Fatal(err)
	}
	defer host.registry.Close()
	ns, err := nativeHistoryNamespace(agentproto.BackendClaude, app.headlessRuntime.BaseEnv, cwd, "host")
	if err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendClaude, HistoryNamespace: ns}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	var got relayruntime.HeadlessLaunchOptions
	host.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) { got = v; return os.Getpid(), nil }
	blocked := filepath.Join(root, "private-carriers", "inc", "launch.json")
	if err = os.MkdirAll(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = app.launchPrivateNative(ctx, host, privateNativeLaunchRequest{Record: rec}); err == nil {
		t.Fatal("expected snapshot write failure")
	}
	stored, err := host.store.lookup(ctx, "instance")
	if err != nil || stored.State != "prepared" {
		t.Fatalf("missing prepared launch: %+v %v", stored, err)
	}
	secret, err := host.store.credential(ctx, stored)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	app.headlessRuntime.BaseEnv[1] = "APPROVAL_SETTING=changed"
	if _, err = app.resumePreparedPrivateLaunch(ctx, host, "instance"); err == nil {
		t.Fatal("changed config launched")
	}
	app.headlessRuntime.BaseEnv[1] = "APPROVAL_SETTING=original"
	if _, err = app.resumePreparedPrivateLaunch(ctx, host, "instance"); err != nil {
		t.Fatal(err)
	}
	if !containsEnvEntry(got.Env, agentproto.PrivateNativeCredentialEnv+"="+secret) {
		t.Fatal("recovery changed credential")
	}
	if _, err = app.resumePreparedPrivateLaunch(ctx, host, "instance"); err == nil {
		t.Fatal("running launch restarted")
	}
}

func TestPrivateOpenCodeLaunchFreezesProfileAndHistoryRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	ctx := t.Context()
	app := newManagedInstancesAdminTestApp(t)
	root := app.headlessRuntime.Paths.StateDir
	cwd := t.TempDir()
	data := t.TempDir()
	binary := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '2.1.4\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	app.headlessRuntime.BaseEnv = []string{"HOME=" + t.TempDir(), "XDG_DATA_HOME=" + data, "APPROVAL_SETTING=unchanged", "OPENCODE_BIN=" + binary}
	host := &privateNativeHost{hostID: "host", stateDir: root}
	var err error
	host.store, err = openPrivateNativeHostStore(filepath.Join(root, "private-opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer host.store.Close()
	host.registry, err = executionowner.Open(filepath.Join(root, "owners-opencode.db"), host)
	if err != nil {
		t.Fatal(err)
	}
	defer host.registry.Close()
	host.outbound, err = executionowner.NewOutbound(host.registry)
	if err != nil {
		t.Fatal(err)
	}
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc-opencode", InstanceID: "opencode-instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendOpenCode}
	rec := privateCarrierRecord{Snapshot: agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native-opencode.db")}, Surface: privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}, SurfaceGeneration: 1}
	var got relayruntime.HeadlessLaunchOptions
	host.startHeadless = func(v relayruntime.HeadlessLaunchOptions) (int, error) { got = v; return os.Getpid(), nil }
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nprintf '1.18.7\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = app.launchPrivateNative(ctx, host, privateNativeLaunchRequest{Record: rec}); err == nil || !strings.Contains(err.Error(), "2.1.4") || got.InstanceID != "" {
		t.Fatalf("old version was not reported before spawn: %v", err)
	}
	if err = os.WriteFile(binary, []byte("#!/bin/sh\nprintf '2.1.4\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = app.launchPrivateNative(ctx, host, privateNativeLaunchRequest{Record: rec}); err != nil {
		t.Fatal(err)
	}
	stored, err := host.store.lookup(ctx, owner.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	want, err := nativeHistoryNamespace(agentproto.BackendOpenCode, got.Env, cwd, "host")
	if err != nil {
		t.Fatal(err)
	}
	if got.LaunchMode != relayruntime.HeadlessLaunchModeOpenCodeACP || got.WorkDir != cwd || stored.Snapshot.Owner.HistoryNamespace != want || !containsEnvEntry(got.Env, "APPROVAL_SETTING=unchanged") {
		t.Fatal("launch failed to preserve selected settings and actual history root")
	}
}
