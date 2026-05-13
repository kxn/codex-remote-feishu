package install

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDetectAutostartReportsSupportedDisabledOnWindows(t *testing.T) {
	baseDir := t.TempDir()
	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := taskSchedulerRunner
	serviceRuntimeGOOS = "windows"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		taskSchedulerRunner = originalRunner
	}()
	taskSchedulerRunner = func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "/Query" {
			return "", os.ErrNotExist
		}
		return "", nil
	}

	status, err := DetectAutostart("")
	if err != nil {
		t.Fatalf("DetectAutostart: %v", err)
	}
	if status.Platform != "windows" {
		t.Fatalf("Platform = %q, want windows", status.Platform)
	}
	if !status.Supported {
		t.Fatalf("expected supported status, got %#v", status)
	}
	if status.Manager != ServiceManagerTaskSchedulerLogon {
		t.Fatalf("Manager = %q, want %q", status.Manager, ServiceManagerTaskSchedulerLogon)
	}
	if status.Status != "disabled" || status.Enabled || status.Configured {
		t.Fatalf("unexpected status: %#v", status)
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

func TestApplyAutostartInstallsAndEnablesLaunchdUserService(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote"), "binary")

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := launchctlUserRunner
	serviceRuntimeGOOS = "darwin"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		launchctlUserRunner = originalRunner
	}()

	var calls []string
	launchctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		switch {
		case len(args) > 0 && args[0] == "print":
			return "state = running\npid = 12345\n", nil
		default:
			return "", nil
		}
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
	wantTarget := "gui/" + strconv.Itoa(os.Getuid()) + "/com.codex-remote.service"
	wantPlist := filepath.Join(baseDir, "Library", "LaunchAgents", "com.codex-remote.service.plist")
	if len(calls) != 3 ||
		calls[0] != "enable "+wantTarget ||
		calls[1] != "bootstrap gui/"+strconv.Itoa(os.Getuid())+" "+wantPlist ||
		calls[2] != "print "+wantTarget {
		t.Fatalf("launchctl calls = %#v", calls)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.ServiceManager != ServiceManagerLaunchdUser {
		t.Fatalf("ServiceManager = %q, want %q", loaded.ServiceManager, ServiceManagerLaunchdUser)
	}
	if strings.TrimSpace(loaded.ServiceUnitPath) == "" {
		t.Fatalf("expected service unit path to be written, got %#v", loaded)
	}
	if _, err := os.Stat(loaded.ServiceUnitPath); err != nil {
		t.Fatalf("expected launchd plist to exist: %v", err)
	}
}

func TestApplyAutostartInstallsAndEnablesTaskSchedulerLogonService(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	binaryPath := seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary")

	originalGOOS := serviceRuntimeGOOS
	originalHome := serviceUserHomeDir
	originalRunner := taskSchedulerRunner
	serviceRuntimeGOOS = "windows"
	serviceUserHomeDir = func() (string, error) { return baseDir, nil }
	defer func() {
		serviceRuntimeGOOS = originalGOOS
		serviceUserHomeDir = originalHome
		taskSchedulerRunner = originalRunner
	}()

	var calls []string
	taskSchedulerRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "/Query" {
			return `<Task><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers></Task>`, nil
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
	taskName := taskSchedulerTaskNameForInstance("stable")
	if len(calls) != 3 ||
		!strings.HasPrefix(calls[0], "/Create /TN "+taskName+" /XML ") ||
		calls[1] != "/Change /TN "+taskName+" /ENABLE" ||
		calls[2] != "/Query /TN "+taskName+" /XML" {
		t.Fatalf("task scheduler calls = %#v", calls)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.ServiceManager != ServiceManagerTaskSchedulerLogon {
		t.Fatalf("ServiceManager = %q, want %q", loaded.ServiceManager, ServiceManagerTaskSchedulerLogon)
	}
	if _, err := os.Stat(loaded.ServiceUnitPath); err != nil {
		t.Fatalf("expected task XML file to exist: %v", err)
	}
}

func TestDisableAutostartDisablesSystemdUserService(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	stubServiceUserHome(t, baseDir)
	state := InstallState{
		BaseDir:         baseDir,
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

	enabled := true
	var calls []string
	systemctlUserRunner = func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) == 0 {
			return "", nil
		}
		switch args[0] {
		case "is-enabled":
			if enabled {
				return "enabled", nil
			}
			return "disabled", nil
		case "disable":
			enabled = false
			return "", nil
		default:
			return "", nil
		}
	}

	status, err := DisableAutostart(statePath)
	if err != nil {
		t.Fatalf("DisableAutostart: %v", err)
	}
	if status.Enabled {
		t.Fatalf("expected autostart to be disabled, got %#v", status)
	}
	if status.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", status.Status)
	}
	wantCalls := []string{
		"is-enabled codex-remote.service",
		"disable codex-remote.service",
		"is-enabled codex-remote.service",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("systemctl calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestDetectAutostartReportsConfiguredDisabledState(t *testing.T) {
	baseDir := t.TempDir()
	statePath := defaultInstallStatePath(baseDir)
	stubServiceUserHome(t, baseDir)
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
	if !strings.HasPrefix(state.ServiceUnitPath, baseDir+string(filepath.Separator)) {
		t.Fatalf("ServiceUnitPath = %q, want temp-dir-scoped path under %q", state.ServiceUnitPath, baseDir)
	}
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
