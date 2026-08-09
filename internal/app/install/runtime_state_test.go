package install

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRepairRuntimeStateUpdatesBinaryVersionAndPromotesLiveSystemdUnit(t *testing.T) {
	baseDir := t.TempDir()
	homeDir := t.TempDir()
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "linux"
	defer func() { serviceRuntimeGOOS = originalGOOS }()
	originalHome := serviceUserHomeDir
	serviceUserHomeDir = func() (string, error) { return homeDir, nil }
	defer func() { serviceUserHomeDir = originalHome }()

	originalRunner := systemctlUserRunner
	systemctlUserRunner = func(ctx context.Context, args ...string) (string, error) {
		if len(args) >= 1 && args[0] == "show" {
			return "ActiveState=active\nMainPID=43210\n", nil
		}
		return "", nil
	}
	defer func() { systemctlUserRunner = originalRunner }()

	state := InstallState{
		InstanceID:        "beta",
		BaseDir:           baseDir,
		StatePath:         defaultInstallStatePathForInstance(baseDir, "beta"),
		ServiceManager:    ServiceManagerDetached,
		CurrentBinaryPath: "/old/bin/codex-remote",
		CurrentVersion:    "v1.0.0",
		ConfigPath:        "/old/config.json",
	}

	changed := RepairRuntimeState(&state, RuntimeStateRepairOptions{
		CurrentBinaryPath: "/new/bin/codex-remote",
		CurrentVersion:    "v1.5.0-beta.9",
		ConfigPath:        "/new/config.json",
		PID:               43210,
	})
	if !changed {
		t.Fatal("expected runtime state repair to report changes")
	}
	if state.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", state.ServiceManager, ServiceManagerSystemdUser)
	}
	if state.ServiceUnitPath != filepath.Join(homeDir, ".config", "systemd", "user", "codex-remote-beta.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
	if state.CurrentBinaryPath != "/new/bin/codex-remote" {
		t.Fatalf("CurrentBinaryPath = %q", state.CurrentBinaryPath)
	}
	if state.CurrentVersion != "v1.5.0-beta.9" {
		t.Fatalf("CurrentVersion = %q", state.CurrentVersion)
	}
	if state.ConfigPath != "/new/config.json" {
		t.Fatalf("ConfigPath = %q", state.ConfigPath)
	}
}

func TestRepairRuntimeStateReplacesCrossPlatformManagerWithLiveSystemdUnit(t *testing.T) {
	baseDir := t.TempDir()
	homeDir := t.TempDir()
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "linux"
	defer func() { serviceRuntimeGOOS = originalGOOS }()
	originalHome := serviceUserHomeDir
	serviceUserHomeDir = func() (string, error) { return homeDir, nil }
	defer func() { serviceUserHomeDir = originalHome }()

	var showUnit string
	originalRunner := systemctlUserRunner
	systemctlUserRunner = func(ctx context.Context, args ...string) (string, error) {
		if len(args) >= 4 && args[0] == "show" {
			showUnit = args[len(args)-1]
			return "ActiveState=active\nMainPID=54321\n", nil
		}
		return "", nil
	}
	defer func() { systemctlUserRunner = originalRunner }()

	state := InstallState{
		InstanceID:        "stable",
		BaseDir:           baseDir,
		StatePath:         defaultInstallStatePathForInstance(baseDir, "stable"),
		ServiceManager:    ServiceManagerLaunchdUser,
		ServiceUnitPath:   filepath.Join(homeDir, "Library", "LaunchAgents", "com.codex-remote.service.plist"),
		CurrentBinaryPath: "/old/bin/codex-remote",
		CurrentVersion:    "dev-old",
	}

	changed := RepairRuntimeState(&state, RuntimeStateRepairOptions{
		CurrentBinaryPath: "/new/bin/codex-remote",
		CurrentVersion:    "dev-new",
		PID:               54321,
	})
	if !changed {
		t.Fatal("expected runtime state repair to report changes")
	}
	if showUnit != "codex-remote.service" {
		t.Fatalf("systemctl show unit = %q, want codex-remote.service", showUnit)
	}
	if state.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", state.ServiceManager, ServiceManagerSystemdUser)
	}
	if state.ServiceUnitPath != filepath.Join(homeDir, ".config", "systemd", "user", "codex-remote.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
}
