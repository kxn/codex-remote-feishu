package install

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
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

	err := StartUpgradeHelperProcess(context.Background(), UpgradeHelperLaunchOptions{
		State: InstallState{
			ServiceManager: ServiceManagerDetached,
		},
		HelperBinary: "/tmp/helper",
		StatePath:    "/tmp/install-state.json",
		LogPath:      "/tmp/helper.log",
		Env:          []string{"A=B"},
		WorkDir:      "/tmp/work",
	})
	if err != nil {
		t.Fatalf("StartUpgradeHelperProcess: %v", err)
	}
	if detached.BinaryPath != "/tmp/helper" {
		t.Fatalf("binary = %q, want /tmp/helper", detached.BinaryPath)
	}
	if got, want := strings.Join(detached.Args, "\x00"), strings.Join([]string{"upgrade-helper", "-state-path", "/tmp/install-state.json"}, "\x00"); got != want {
		t.Fatalf("args = %#v, want %#v", detached.Args, []string{"upgrade-helper", "-state-path", "/tmp/install-state.json"})
	}
	if detached.WorkDir != "/tmp/work" {
		t.Fatalf("workdir = %q, want /tmp/work", detached.WorkDir)
	}
	if detached.StdoutPath != "/tmp/helper.log" || detached.StderrPath != "/tmp/helper.log" {
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

	err := StartUpgradeHelperProcess(context.Background(), UpgradeHelperLaunchOptions{
		State: InstallState{
			ServiceManager: ServiceManagerSystemdUser,
		},
		HelperBinary: "/tmp/helper",
		StatePath:    "/tmp/install-state.json",
		LogPath:      "/tmp/helper.log",
		Env:          []string{"A=B"},
		WorkDir:      "/tmp/work",
	})
	if err != nil {
		t.Fatalf("StartUpgradeHelperProcess: %v", err)
	}
	if transient.BinaryPath != "/tmp/helper" {
		t.Fatalf("binary = %q, want /tmp/helper", transient.BinaryPath)
	}
	if got, want := strings.Join(transient.Args, "\x00"), strings.Join([]string{"upgrade-helper", "-state-path", "/tmp/install-state.json"}, "\x00"); got != want {
		t.Fatalf("args = %#v, want %#v", transient.Args, []string{"upgrade-helper", "-state-path", "/tmp/install-state.json"})
	}
	if transient.WorkDir != "/tmp/work" {
		t.Fatalf("workdir = %q, want /tmp/work", transient.WorkDir)
	}
	if transient.LogPath != "/tmp/helper.log" {
		t.Fatalf("log path = %q, want /tmp/helper.log", transient.LogPath)
	}
	if !strings.HasPrefix(transient.UnitName, "codex-remote-upgrade-helper-") || filepath.Ext(transient.UnitName) != ".service" {
		t.Fatalf("unit name = %q, want codex-remote-upgrade-helper-*.service", transient.UnitName)
	}
}
