package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectAutostartReportsUnsupportedOutsideLinux(t *testing.T) {
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "darwin"
	defer func() {
		serviceRuntimeGOOS = originalGOOS
	}()

	status, err := DetectAutostart("")
	if err != nil {
		t.Fatalf("DetectAutostart: %v", err)
	}
	if status.Platform != "darwin" {
		t.Fatalf("Platform = %q, want darwin", status.Platform)
	}
	if status.Supported {
		t.Fatalf("expected unsupported status, got %#v", status)
	}
	if status.Status != "unsupported" {
		t.Fatalf("Status = %q, want unsupported", status.Status)
	}
}

func TestApplyAutostartInstallsAndEnablesSystemdUserService(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := systemctlUserRunner
	serviceRuntimeGOOS = "linux"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		systemctlUserRunner = originalRunner
	}()

	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "is-enabled" {
			return "enabled", nil
		}
		return "", nil
	}

	status, err := ApplyAutostart(AutostartApplyOptions{
		StatePath:       statePath,
		BaseDir:         baseDir,
		InstalledBinary: binaryPath,
		CurrentVersion:  "dev",
	})
	if err != nil {
		t.Fatalf("ApplyAutostart: %v", err)
	}
	if status.Status != "enabled" || !status.Enabled {
		t.Fatalf("unexpected autostart status: %#v", status)
	}
	if len(calls) != 3 || calls[0] != "daemon-reload" || calls[1] != "enable codex-remote.service" || calls[2] != "is-enabled codex-remote.service" {
		t.Fatalf("systemctl calls = %#v", calls)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want %q", loaded.ServiceManager, ServiceManagerSystemdUser)
	}
	if strings.TrimSpace(loaded.ServiceUnitPath) == "" {
		t.Fatalf("expected service unit path to be written, got %#v", loaded)
	}
	if _, err := os.Stat(loaded.ServiceUnitPath); err != nil {
		t.Fatalf("expected service unit file to exist: %v", err)
	}
}

func TestDetectAutostartReportsConfiguredDisabledState(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary"),
		ServiceManager:  ServiceManagerSystemdUser,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        baseDir,
		ServiceManager: state.ServiceManager,
	})
	if err := os.MkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(unit dir): %v", err)
	}
	if err := os.WriteFile(state.ServiceUnitPath, []byte("[Unit]\nDescription=test\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(unit): %v", err)
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := systemctlUserRunner
	serviceRuntimeGOOS = "linux"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		systemctlUserRunner = originalRunner
	}()

	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "is-enabled" {
			return "disabled", nil
		}
		return "", nil
	}

	status, err := DetectAutostart(statePath)
	if err != nil {
		t.Fatalf("DetectAutostart: %v", err)
	}
	if !status.Configured {
		t.Fatalf("expected configured status, got %#v", status)
	}
	if status.Enabled {
		t.Fatalf("expected disabled autostart, got %#v", status)
	}
	if status.Status != "disabled" {
		t.Fatalf("Status = %q, want disabled", status.Status)
	}
}

func TestApplyAutostartDebugInstanceUsesDebugUnit(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePathForInstance(baseDir, debugInstanceID)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := systemctlUserRunner
	serviceRuntimeGOOS = "linux"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		systemctlUserRunner = originalRunner
	}()

	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "is-enabled" {
			return "enabled", nil
		}
		return "", nil
	}

	status, err := ApplyAutostart(AutostartApplyOptions{
		InstanceID:      debugInstanceID,
		StatePath:       statePath,
		BaseDir:         baseDir,
		InstalledBinary: binaryPath,
		CurrentVersion:  "dev",
	})
	if err != nil {
		t.Fatalf("ApplyAutostart: %v", err)
	}
	if status.ServiceUnitPath != filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote-debug.service") {
		t.Fatalf("ServiceUnitPath = %q", status.ServiceUnitPath)
	}
	if len(calls) != 3 || calls[0] != "daemon-reload" || calls[1] != "enable codex-remote-debug.service" || calls[2] != "is-enabled codex-remote-debug.service" {
		t.Fatalf("systemctl calls = %#v", calls)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.InstanceID != debugInstanceID {
		t.Fatalf("InstanceID = %q, want %q", loaded.InstanceID, debugInstanceID)
	}
}
