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
	"unicode/utf16"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func withMockTaskSchedulerPowerShell(t *testing.T, fn func(ctx context.Context, script string) (string, error)) {
	t.Helper()
	originalRunner := taskSchedulerPowerShellRunner
	taskSchedulerPowerShellRunner = fn
	t.Cleanup(func() { taskSchedulerPowerShellRunner = originalRunner })
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

	var psScript string
	withMockTaskSchedulerPowerShell(t, func(_ context.Context, script string) (string, error) {
		psScript = script
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
	xmlBytes, err := os.ReadFile(updated.ServiceUnitPath)
	if err != nil {
		t.Fatalf("ReadFile(task XML): %v", err)
	}
	if !bytes.HasPrefix(xmlBytes, []byte{0xff, 0xfe}) {
		t.Fatalf("task XML should be UTF-16LE with BOM, got prefix % x", xmlBytes[:min(4, len(xmlBytes))])
	}
	taskName := taskSchedulerTaskNameForInstance("stable")
	mustContain := []string{
		"Register-ScheduledTask",
		"-AtLogOn",
		taskName,
		"codex-remote.exe",
		"-config",
		"-xdg-config-home",
		"-xdg-data-home",
		"-xdg-state-home",
	}
	for _, s := range mustContain {
		if !strings.Contains(psScript, s) {
			t.Fatalf("PowerShell script missing %q:\n%s", s, psScript)
		}
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

	var psScript string
	withMockTaskSchedulerPowerShell(t, func(_ context.Context, script string) (string, error) {
		psScript = script
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
	if !strings.Contains(psScript, "Register-ScheduledTask") || !strings.Contains(psScript, "-AtLogOn") {
		t.Fatalf("PowerShell script missing Register-ScheduledTask or -AtLogOn: %s", psScript)
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

	var psCalls int
	withMockTaskSchedulerPowerShell(t, func(_ context.Context, script string) (string, error) {
		if !strings.Contains(script, "Register-ScheduledTask") || !strings.Contains(script, "-AtLogOn") {
			t.Errorf("PowerShell script missing Register-ScheduledTask or -AtLogOn: %s", script)
		}
		psCalls++
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
	if psCalls != 3 { // enable, start, restart each call installManagedService
		t.Fatalf("PowerShell task registration calls = %d, want 3", psCalls)
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

func TestTaskSchedulerDetectsEnabledFromXMLWhenEnabledIsMissing(t *testing.T) {
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
			return `<Task><Triggers><LogonTrigger></LogonTrigger></Triggers><Settings><DisallowStartIfOnBatteries>true</DisallowStartIfOnBatteries></Settings></Task>`, nil
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
		t.Fatal("expected enabled=true when Enabled element is absent (default)")
	}
}

func TestTaskSchedulerDetectsEnabledFromPSRegisteredXMLWithUTF16Declaration(t *testing.T) {
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

	// Real-world output of `schtasks /Query /TN \CodexRemoteFeishu\stable /XML`
	// for a task registered via PowerShell Register-ScheduledTask: the XML
	// declaration advertises UTF-16 (which Go's xml.Unmarshal rejects without a
	// CharsetReader) and the Task Scheduler omits the <Enabled> element because
	// the task is enabled by default.
	withMockTaskScheduler(t, func(_ context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "/Query" {
			return `<?xml version="1.0" encoding="UTF-16"?>

<Task version="1.3" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">

  <RegistrationInfo>

    <Description>codex-remote auto start at logon</Description>

    <URI>\CodexRemoteFeishu\stable</URI>

  </RegistrationInfo>

  <Principals>

    <Principal id="Author">

      <UserId>S-1-5-21-2141574936-2934653207-2305175561-1001</UserId>

      <LogonType>InteractiveToken</LogonType>

    </Principal>

  </Principals>

  <Settings>

    <DisallowStartIfOnBatteries>true</DisallowStartIfOnBatteries>

    <StopIfGoingOnBatteries>true</StopIfGoingOnBatteries>

    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>

    <UseUnifiedSchedulingEngine>true</UseUnifiedSchedulingEngine>

  </Settings>

  <Triggers>

    <LogonTrigger>

      <UserId>KXN-PC\kxn</UserId>

    </LogonTrigger>

  </Triggers>

  <Actions Context="Author">

    <Exec>

      <Command>E:\Downloads\codex-remote.exe</Command>

      <Arguments>daemon -config C:\Users\kxn\.config\codex-remote\config.json</Arguments>

      <WorkingDirectory>C:\Users\kxn</WorkingDirectory>

    </Exec>

  </Actions>

</Task>`, nil
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
		t.Fatal("expected enabled=true for PowerShell-registered task XML with UTF-16 declaration")
	}
}

func TestTaskSchedulerMissingLocalizedOutputIsDisabled(t *testing.T) {
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
			return "错误: 系统找不到指定的文件。", fmt.Errorf("exit status 1: 错误: 系统找不到指定的文件。")
		}
		return "", fmt.Errorf("unexpected schtasks call: %v", args)
	})

	enabled, warning, err := detectTaskSchedulerLogonEnabled(context.Background(), state)
	if err != nil {
		t.Fatalf("detectTaskSchedulerLogonEnabled: %v", err)
	}
	if enabled || warning != "" {
		t.Fatalf("enabled=%v warning=%q, want disabled without warning", enabled, warning)
	}
}

func TestDecodeTaskSchedulerOutputHandlesLocalizedEncodings(t *testing.T) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("错误: 系统找不到指定的文件。"))
	if err != nil {
		t.Fatalf("encode gbk: %v", err)
	}
	if got := decodeTaskSchedulerOutput(gbk); !strings.Contains(got, "系统找不到指定的文件") {
		t.Fatalf("gbk decoded as %q", got)
	}

	utf16Raw := append([]byte{0xff, 0xfe}, utf16LETestBytes("错误: 系统找不到指定的文件。")...)
	if got := decodeTaskSchedulerOutput(utf16Raw); !strings.Contains(got, "系统找不到指定的文件") {
		t.Fatalf("utf16 decoded as %q", got)
	}
}

func utf16LETestBytes(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	out := make([]byte, 0, len(encoded)*2)
	for _, item := range encoded {
		out = append(out, byte(item), byte(item>>8))
	}
	return out
}
