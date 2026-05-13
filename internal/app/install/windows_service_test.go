package install

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func withWindowsGOOS(t *testing.T) {
	t.Helper()
	originalGOOS := serviceRuntimeGOOS
	serviceRuntimeGOOS = "windows"
	t.Cleanup(func() { serviceRuntimeGOOS = originalGOOS })
}

func withMockTaskScheduler(t *testing.T, fn func(ctx context.Context, args ...string) (string, error)) {
	t.Helper()
	originalRunner := taskSchedulerRunner
	taskSchedulerRunner = fn
	t.Cleanup(func() { taskSchedulerRunner = originalRunner })
}

func TestTaskSchedulerTaskNameForInstance(t *testing.T) {
	tests := []struct {
		instanceID string
		want       string
	}{
		{"", `\CodexRemoteFeishu\stable`},
		{"stable", `\CodexRemoteFeishu\stable`},
		{"debug", `\CodexRemoteFeishu\debug`},
	}
	for _, tc := range tests {
		got := taskSchedulerTaskNameForInstance(tc.instanceID)
		if got != tc.want {
			t.Fatalf("taskSchedulerTaskNameForInstance(%q) = %q, want %q", tc.instanceID, got, tc.want)
		}
	}
}

func TestRenderTaskSchedulerLogonXMLContainsLogonTriggerAndDaemonArgs(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex Remote")
	binaryPath := filepath.Join(baseDir, "bin", "codex-remote.exe")
	state := InstallState{
		InstanceID:      "debug",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePathForInstance(baseDir, "debug"),
		ConfigPath:      defaultConfigPathForInstance(baseDir, "debug"),
		InstalledBinary: binaryPath,
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	xmlText, err := renderTaskSchedulerLogonXML(state)
	if err != nil {
		t.Fatalf("renderTaskSchedulerLogonXML: %v", err)
	}
	mustContain := []string{
		`<LogonTrigger>`,
		`<LogonType>InteractiveToken</LogonType>`,
		`<RunLevel>LeastPrivilege</RunLevel>`,
		`<Command>` + xmlEscape(binaryPath) + `</Command>`,
		`<WorkingDirectory>` + xmlEscape(baseDir) + `</WorkingDirectory>`,
		`<Arguments>daemon `,
		`-config`,
		xmlEscape(state.ConfigPath),
		`-xdg-config-home`,
		`-xdg-data-home`,
		`-xdg-state-home`,
	}
	for _, s := range mustContain {
		if !strings.Contains(xmlText, s) {
			t.Fatalf("task XML missing %q:\n%s", s, xmlText)
		}
	}
}

func TestInstallTaskSchedulerLogonRegistersXMLTask(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex Remote")
	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePathForInstance(baseDir, "stable"),
		ConfigPath:      defaultConfigPathForInstance(baseDir, "stable"),
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	var calls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})

	updated, err := installTaskSchedulerLogonTask(context.Background(), state)
	if err != nil {
		t.Fatalf("installTaskSchedulerLogonTask: %v", err)
	}
	if updated.ServiceManager != ServiceManagerTaskSchedulerLogon {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerTaskSchedulerLogon)
	}
	if _, err := os.Stat(updated.ServiceUnitPath); err != nil {
		t.Fatalf("task XML was not written: %v", err)
	}
	wantCalls := []string{strings.Join([]string{"/Create", "/TN", taskSchedulerTaskNameForInstance("stable"), "/XML", updated.ServiceUnitPath, "/F"}, " ")}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("task scheduler calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestTaskSchedulerLifecycleCommands(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:      "debug",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePathForInstance(baseDir, "debug"),
		ConfigPath:      defaultConfigPathForInstance(baseDir, "debug"),
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	var calls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "/Query" {
			return "Status: Ready\r\n", nil
		}
		return "", nil
	})

	if err := taskSchedulerLogonEnable(context.Background(), state); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := taskSchedulerLogonDisable(context.Background(), state); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := taskSchedulerLogonStart(context.Background(), state); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := taskSchedulerLogonStop(context.Background(), state); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := taskSchedulerLogonRestart(context.Background(), state); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if _, err := taskSchedulerLogonStatus(context.Background(), state); err != nil {
		t.Fatalf("status: %v", err)
	}

	taskName := taskSchedulerTaskNameForInstance("debug")
	want := []string{
		"/Change /TN " + taskName + " /ENABLE",
		"/Change /TN " + taskName + " /DISABLE",
		"/Run /TN " + taskName,
		"/End /TN " + taskName,
		"/End /TN " + taskName,
		"/Run /TN " + taskName,
		"/Query /TN " + taskName + " /FO LIST /V",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("task scheduler calls = %#v, want %#v", calls, want)
	}
}

func TestRunServiceInstallUserWindowsWritesTaskState(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex Remote")
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      defaultConfigPath(baseDir),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
	}
	ApplyStateMetadata(&state, StateMetadataOptions{StatePath: statePath})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})

	var stdout bytes.Buffer
	if err := RunService([]string{"install-user", "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService install-user: %v", err)
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerTaskSchedulerLogon {
		t.Fatalf("ServiceManager = %q, want %q", updated.ServiceManager, ServiceManagerTaskSchedulerLogon)
	}
	if !strings.Contains(stdout.String(), "service manager: task_scheduler_logon") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0], "/Create /TN "+taskSchedulerTaskNameForInstance("stable")+" /XML ") {
		t.Fatalf("task scheduler calls = %#v", calls)
	}
}

func TestRunServiceWindowsLifecycleCommandsUseTaskScheduler(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex Remote")
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      defaultConfigPath(baseDir),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        baseDir,
		ServiceManager: state.ServiceManager,
	})
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 0 && args[0] == "/Query" {
			return "Status: Ready\r\n", nil
		}
		return "", nil
	})

	for _, subcommand := range []string{"enable", "disable", "start", "stop", "restart", "status"} {
		var stdout bytes.Buffer
		if err := RunService([]string{subcommand, "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
			t.Fatalf("RunService %s: %v", subcommand, err)
		}
	}

	taskName := taskSchedulerTaskNameForInstance("stable")
	want := []string{
		"/Create /TN " + taskName + " /XML " + state.ServiceUnitPath + " /F",
		"/Change /TN " + taskName + " /ENABLE",
		"/Change /TN " + taskName + " /DISABLE",
		"/Create /TN " + taskName + " /XML " + state.ServiceUnitPath + " /F",
		"/Run /TN " + taskName,
		"/End /TN " + taskName,
		"/Create /TN " + taskName + " /XML " + state.ServiceUnitPath + " /F",
		"/End /TN " + taskName,
		"/Run /TN " + taskName,
		"/Query /TN " + taskName + " /FO LIST /V",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("task scheduler calls = %#v, want %#v", calls, want)
	}
}

func TestRunServiceUninstallUserWindowsDeletesTaskAndDetachesState(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := filepath.Join(t.TempDir(), "Codex Remote")
	statePath := defaultInstallStatePath(baseDir)
	state := InstallState{
		BaseDir:         baseDir,
		ConfigPath:      defaultConfigPath(baseDir),
		StatePath:       statePath,
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      statePath,
		BaseDir:        baseDir,
		ServiceManager: state.ServiceManager,
	})
	if err := os.MkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(task xml dir): %v", err)
	}
	if err := os.WriteFile(state.ServiceUnitPath, []byte("<Task/>"), 0o644); err != nil {
		t.Fatalf("WriteFile(task xml): %v", err)
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	var calls []string
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})

	var stdout bytes.Buffer
	if err := RunService([]string{"uninstall-user", "-state-path", statePath}, strings.NewReader(""), &stdout, &bytes.Buffer{}, "vtest"); err != nil {
		t.Fatalf("RunService uninstall-user: %v", err)
	}
	taskName := taskSchedulerTaskNameForInstance("stable")
	if got, want := calls, []string{"/Delete /TN " + taskName + " /F"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task scheduler calls = %#v, want %#v", got, want)
	}
	if _, err := os.Stat(state.ServiceUnitPath); !os.IsNotExist(err) {
		t.Fatalf("task XML should be removed, stat err = %v", err)
	}
	updated, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if updated.ServiceManager != ServiceManagerDetached || strings.TrimSpace(updated.ServiceUnitPath) != "" {
		t.Fatalf("state should be detached after uninstall, got %#v", updated)
	}
}

func TestTaskSchedulerDetectsEnabledFromXML(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		ConfigPath:      defaultConfigPath(baseDir),
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "/Query" {
			return `<Task><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers></Task>`, nil
		}
		return "", fmt.Errorf("unexpected schtasks call: %v", args)
	})

	enabled, warning, err := detectTaskSchedulerLogonEnabled(context.Background(), state)
	if err != nil {
		t.Fatalf("detectTaskSchedulerLogonEnabled: %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
	if !enabled {
		t.Fatal("expected enabled=true")
	}
}

func TestTaskSchedulerDetectsDisabledFromSettingsXMLWhenTriggerStaysEnabled(t *testing.T) {
	withWindowsGOOS(t)
	baseDir := t.TempDir()
	state := InstallState{
		InstanceID:      "stable",
		BaseDir:         baseDir,
		StatePath:       defaultInstallStatePath(baseDir),
		ConfigPath:      defaultConfigPath(baseDir),
		InstalledBinary: seedBinary(t, filepath.Join(baseDir, "bin", "codex-remote.exe"), "binary"),
		ServiceManager:  ServiceManagerTaskSchedulerLogon,
	}
	ApplyStateMetadata(&state, StateMetadataOptions{
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})

	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "/Query" {
			return `<Task><Triggers><LogonTrigger><Enabled>true</Enabled></LogonTrigger></Triggers><Settings><Enabled>false</Enabled></Settings></Task>`, nil
		}
		return "", fmt.Errorf("unexpected schtasks call: %v", args)
	})

	enabled, warning, err := detectTaskSchedulerLogonEnabled(context.Background(), state)
	if err != nil {
		t.Fatalf("detectTaskSchedulerLogonEnabled: %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
	if enabled {
		t.Fatal("expected enabled=false")
	}
}
