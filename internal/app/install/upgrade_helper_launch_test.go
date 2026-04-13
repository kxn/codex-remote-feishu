package install

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestStartUpgradeHelperProcessUsesDetachedCommandForDetachedService(t *testing.T) {
	originalDetached := upgradeHelperStartDetachedCommandFunc
	originalSystemd := upgradeHelperStartSystemdUserTransientFunc
	defer func() {
		upgradeHelperStartDetachedCommandFunc = originalDetached
		upgradeHelperStartSystemdUserTransientFunc = originalSystemd
	}()

	var detached relayruntime.DetachedCommandOptions
	upgradeHelperStartDetachedCommandFunc = func(opts relayruntime.DetachedCommandOptions) (int, error) {
		detached = opts
		return 123, nil
	}
	upgradeHelperStartSystemdUserTransientFunc = func(context.Context, systemdUserTransientCommandOptions) (string, error) {
		t.Fatal("unexpected systemd-run launcher")
		return "", nil
	}

	result, err := StartUpgradeHelperProcess(context.Background(), UpgradeHelperLaunchOptions{
		State: InstallState{
			ServiceManager: ServiceManagerDetached,
		},
		HelperBinary: testutil.WorkspacePath("tmp", "helper"),
		StatePath:    testutil.WorkspacePath("tmp", "install-state.json"),
		LogPath:      testutil.WorkspacePath("tmp", "helper.log"),
		Env:          []string{"A=B"},
		WorkDir:      testutil.WorkspacePath("tmp", "work"),
	})
	if err != nil {
		t.Fatalf("StartUpgradeHelperProcess: %v", err)
	}
	if result.UnitName != "" {
		t.Fatalf("unit name = %q, want empty for detached helper", result.UnitName)
	}
	if !testutil.SamePath(detached.BinaryPath, testutil.WorkspacePath("tmp", "helper")) {
		t.Fatalf("binary = %q, want %q", detached.BinaryPath, testutil.WorkspacePath("tmp", "helper"))
	}
	if got, want := strings.Join(detached.Args, "\x00"), strings.Join([]string{"upgrade-helper", "-state-path", testutil.WorkspacePath("tmp", "install-state.json")}, "\x00"); got != want {
		t.Fatalf("args = %#v, want %#v", detached.Args, []string{"upgrade-helper", "-state-path", testutil.WorkspacePath("tmp", "install-state.json")})
	}
	if !testutil.SamePath(detached.WorkDir, testutil.WorkspacePath("tmp", "work")) {
		t.Fatalf("workdir = %q, want %q", detached.WorkDir, testutil.WorkspacePath("tmp", "work"))
	}
	if !testutil.SamePath(detached.StdoutPath, testutil.WorkspacePath("tmp", "helper.log")) || !testutil.SamePath(detached.StderrPath, testutil.WorkspacePath("tmp", "helper.log")) {
		t.Fatalf("stdout/stderr = %q %q, want helper log", detached.StdoutPath, detached.StderrPath)
	}
}

func TestStartUpgradeHelperProcessUsesSystemdRunForSystemdUser(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd user service is linux-only")
	}

	originalDetached := upgradeHelperStartDetachedCommandFunc
	originalSystemd := upgradeHelperStartSystemdUserTransientFunc
	defer func() {
		upgradeHelperStartDetachedCommandFunc = originalDetached
		upgradeHelperStartSystemdUserTransientFunc = originalSystemd
	}()

	var transient systemdUserTransientCommandOptions
	upgradeHelperStartDetachedCommandFunc = func(relayruntime.DetachedCommandOptions) (int, error) {
		t.Fatal("unexpected detached launcher")
		return 0, nil
	}
	upgradeHelperStartSystemdUserTransientFunc = func(_ context.Context, opts systemdUserTransientCommandOptions) (string, error) {
		transient = opts
		return "codex-remote-upgrade-helper-test.service", nil
	}

	result, err := StartUpgradeHelperProcess(context.Background(), UpgradeHelperLaunchOptions{
		State: InstallState{
			ServiceManager: ServiceManagerSystemdUser,
		},
		HelperBinary: testutil.WorkspacePath("tmp", "helper"),
		StatePath:    testutil.WorkspacePath("tmp", "install-state.json"),
		LogPath:      testutil.WorkspacePath("tmp", "helper.log"),
		Env:          []string{"A=B"},
		WorkDir:      testutil.WorkspacePath("tmp", "work"),
	})
	if err != nil {
		t.Fatalf("StartUpgradeHelperProcess: %v", err)
	}
	if result.UnitName != transient.UnitName {
		t.Fatalf("result unit name = %q, want %q", result.UnitName, transient.UnitName)
	}
	if !testutil.SamePath(transient.BinaryPath, testutil.WorkspacePath("tmp", "helper")) {
		t.Fatalf("binary = %q, want %q", transient.BinaryPath, testutil.WorkspacePath("tmp", "helper"))
	}
	if got, want := strings.Join(transient.Args, "\x00"), strings.Join([]string{"upgrade-helper", "-state-path", testutil.WorkspacePath("tmp", "install-state.json")}, "\x00"); got != want {
		t.Fatalf("args = %#v, want %#v", transient.Args, []string{"upgrade-helper", "-state-path", testutil.WorkspacePath("tmp", "install-state.json")})
	}
	if !testutil.SamePath(transient.WorkDir, testutil.WorkspacePath("tmp", "work")) {
		t.Fatalf("workdir = %q, want %q", transient.WorkDir, testutil.WorkspacePath("tmp", "work"))
	}
	if !testutil.SamePath(transient.LogPath, testutil.WorkspacePath("tmp", "helper.log")) {
		t.Fatalf("log path = %q, want %q", transient.LogPath, testutil.WorkspacePath("tmp", "helper.log"))
	}
	if !strings.HasPrefix(transient.UnitName, "codex-remote-upgrade-helper-") || filepath.Ext(transient.UnitName) != ".service" {
		t.Fatalf("unit name = %q, want codex-remote-upgrade-helper-*.service", transient.UnitName)
	}
}
