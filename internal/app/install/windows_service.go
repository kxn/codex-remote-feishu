package install

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"golang.org/x/text/encoding/simplifiedchinese"
)

var taskSchedulerRunner = runTaskScheduler

var taskSchedulerPowerShellRunner = runTaskSchedulerPowerShell

func runTaskScheduler(ctx context.Context, args ...string) (string, error) {
	cmd := execlaunch.CommandContext(ctx, "schtasks", args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(decodeTaskSchedulerOutput(output))
	if err != nil {
		if trimmed == "" {
			return "", err
		}
		return trimmed, fmt.Errorf("%w: %s", err, trimmed)
	}
	return trimmed, nil
}

// runTaskSchedulerPowerShell runs a PowerShell script against the Task
// Scheduler module. schtasks.exe refuses to register a logon-trigger task
// (Access denied) for standard users, while the Task Scheduler COM API used
// by PowerShell can register the current user's logon task without elevation.
func runTaskSchedulerPowerShell(ctx context.Context, script string) (string, error) {
	cmd := execlaunch.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(decodeTaskSchedulerOutput(output))
	if err != nil {
		if trimmed == "" {
			return "", err
		}
		return trimmed, fmt.Errorf("%w: %s", err, trimmed)
	}
	return trimmed, nil
}

// serviceCurrentUser resolves the current interactive user in DOMAIN\user form
// for the logon trigger and task principal.
func serviceCurrentUser() (string, error) {
	if current, err := user.Current(); err == nil {
		if name := strings.TrimSpace(current.Username); name != "" {
			return name, nil
		}
	}
	if name := strings.TrimSpace(os.Getenv("USERNAME")); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("unable to resolve current user for logon task")
}

// buildTaskSchedulerRegisterScript renders a PowerShell script that registers
// the logon task via the Task Scheduler COM API. Values are embedded with
// PowerShell single-quote quoting so paths with spaces, $, or backticks are
// passed through literally.
func buildTaskSchedulerRegisterScript(state InstallState) (string, error) {
	binaryPath := normalizeServicePathValue(firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(state.CurrentBinaryPath)))
	if binaryPath == "" {
		return "", fmt.Errorf("installed binary path is missing")
	}
	arguments := windowsCommandLineArgs(windowsDaemonArgsForState(state)...)
	workingDirectory := normalizeServicePathValue(state.BaseDir)
	taskName := taskSchedulerTaskNameForInstance(state.InstanceID)
	currentUser, err := serviceCurrentUser()
	if err != nil {
		return "", err
	}
	psSingleQuote := func(value string) string {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	script := fmt.Sprintf(
		"[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; $ErrorActionPreference='Stop'; "+
			"try { "+
			"$action = New-ScheduledTaskAction -Execute %s -Argument %s -WorkingDirectory %s; "+
			"$trigger = New-ScheduledTaskTrigger -AtLogOn -User %s; "+
			"$principal = New-ScheduledTaskPrincipal -UserId %s -LogonType Interactive -RunLevel Limited; "+
			"Register-ScheduledTask -TaskName %s -Action $action -Trigger $trigger -Principal $principal -Description %s -Force | Out-Null "+
			"} catch { Write-Error ($_.Exception.Message); exit 1 }",
		psSingleQuote(binaryPath),
		psSingleQuote(arguments),
		psSingleQuote(workingDirectory),
		psSingleQuote(currentUser),
		psSingleQuote(currentUser),
		psSingleQuote(taskName),
		psSingleQuote("codex-remote auto start at logon"),
	)
	return script, nil
}

func decodeTaskSchedulerOutput(output []byte) string {
	if len(output) == 0 {
		return ""
	}
	if len(output) >= 2 {
		switch {
		case output[0] == 0xff && output[1] == 0xfe:
			return string(utf16.Decode(utf16BytesLE(output[2:])))
		case output[0] == 0xfe && output[1] == 0xff:
			return string(utf16.Decode(utf16BytesBE(output[2:])))
		}
	}
	if utf8.Valid(output) {
		return string(output)
	}
	if decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(output); err == nil && utf8.Valid(decoded) {
		return string(decoded)
	}
	return string(output)
}

func utf16BytesLE(output []byte) []uint16 {
	values := make([]uint16, 0, len(output)/2)
	for i := 0; i+1 < len(output); i += 2 {
		values = append(values, uint16(output[i])|uint16(output[i+1])<<8)
	}
	return values
}

func utf16BytesBE(output []byte) []uint16 {
	values := make([]uint16, 0, len(output)/2)
	for i := 0; i+1 < len(output); i += 2 {
		values = append(values, uint16(output[i])<<8|uint16(output[i+1]))
	}
	return values
}

func ensureWindowsTaskSchedulerSupport() error {
	if serviceRuntimeGOOS != "windows" {
		return fmt.Errorf("task scheduler logon service is only supported on windows (current: %s)", serviceRuntimeGOOS)
	}
	return nil
}

func taskSchedulerTaskNameForInstance(instanceID string) string {
	instanceID = normalizeInstanceID(instanceID)
	if instanceID == "" {
		instanceID = defaultInstanceID
	}
	return `\CodexRemoteFeishu\` + instanceID
}

func taskSchedulerXMLPathForInstance(baseDir, instanceID string) string {
	baseDir = filepath.Clean(strings.TrimSpace(baseDir))
	if baseDir == "" {
		return ""
	}
	layout := installLayoutForInstance(baseDir, instanceID)
	return filepath.Join(layout.StateDir, "task-scheduler-logon.xml")
}

func taskSchedulerLogonServiceState(state InstallState) (InstallState, error) {
	if err := ensureWindowsTaskSchedulerSupport(); err != nil {
		return InstallState{}, err
	}
	return managedServiceState(state, ServiceManagerTaskSchedulerLogon, taskSchedulerXMLPathForInstance, "task scheduler XML path")
}

func renderTaskSchedulerLogonXML(state InstallState) (string, error) {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return "", err
	}
	binaryPath := normalizeServicePathValue(firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(state.CurrentBinaryPath)))
	if binaryPath == "" {
		return "", fmt.Errorf("installed binary path is missing")
	}
	arguments := windowsCommandLineArgs(windowsDaemonArgsForState(state)...)
	taskName := taskSchedulerTaskNameForInstance(state.InstanceID)
	workingDirectory := normalizeServicePathValue(state.BaseDir)

	lines := []string{
		`<?xml version="1.0" encoding="UTF-16"?>`,
		`<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">`,
		`  <RegistrationInfo>`,
		`    <URI>` + xmlEscape(taskName) + `</URI>`,
		`  </RegistrationInfo>`,
		`  <Triggers>`,
		`    <LogonTrigger>`,
		`      <Enabled>true</Enabled>`,
		`    </LogonTrigger>`,
		`  </Triggers>`,
		`  <Principals>`,
		`    <Principal id="Author">`,
		`      <LogonType>InteractiveToken</LogonType>`,
		`      <RunLevel>LeastPrivilege</RunLevel>`,
		`    </Principal>`,
		`  </Principals>`,
		`  <Settings>`,
		`    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>`,
		`    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>`,
		`    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>`,
		`    <AllowHardTerminate>true</AllowHardTerminate>`,
		`    <StartWhenAvailable>true</StartWhenAvailable>`,
		`    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>`,
		`    <Enabled>true</Enabled>`,
		`    <Hidden>false</Hidden>`,
		`    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>`,
		`  </Settings>`,
		`  <Actions Context="Author">`,
		`    <Exec>`,
		`      <Command>` + xmlEscape(binaryPath) + `</Command>`,
		`      <Arguments>` + xmlEscape(arguments) + `</Arguments>`,
		`      <WorkingDirectory>` + xmlEscape(workingDirectory) + `</WorkingDirectory>`,
		`    </Exec>`,
		`  </Actions>`,
		`</Task>`,
		``,
	}
	return strings.Join(lines, "\n"), nil
}

func windowsDaemonArgsForState(state InstallState) []string {
	layout := installLayoutForInstance(state.BaseDir, state.InstanceID)
	configPath := strings.TrimSpace(state.ConfigPath)
	if configPath == "" {
		configPath = filepath.Join(layout.ConfigDir, "config.json")
	}
	return []string{
		"daemon",
		"-config", normalizeServicePathValue(configPath),
		"-xdg-config-home", normalizeServicePathValue(layout.ConfigHome),
		"-xdg-data-home", normalizeServicePathValue(layout.DataHome),
		"-xdg-state-home", normalizeServicePathValue(layout.StateHome),
	}
}

func windowsCommandLineArgs(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, windowsCommandLineArg(arg))
	}
	return strings.Join(quoted, " ")
}

func windowsCommandLineArg(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		if r == '\\' {
			backslashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			b.WriteRune(r)
			backslashes = 0
			continue
		}
		if backslashes > 0 {
			b.WriteString(strings.Repeat(`\`, backslashes))
			backslashes = 0
		}
		b.WriteRune(r)
	}
	if backslashes > 0 {
		b.WriteString(strings.Repeat(`\`, backslashes*2))
	}
	b.WriteByte('"')
	return b.String()
}

func installTaskSchedulerLogonTask(ctx context.Context, state InstallState) (InstallState, error) {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return InstallState{}, err
	}
	xmlContent, err := renderTaskSchedulerLogonXML(state)
	if err != nil {
		return InstallState{}, err
	}
	layout := installLayoutForInstance(state.BaseDir, state.InstanceID)
	if err := serviceMkdirAll(filepath.Join(layout.StateDir, "logs"), 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceMkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceWriteFile(state.ServiceUnitPath, utf16LEWithBOM(xmlContent), 0o644); err != nil {
		return InstallState{}, err
	}
	script, err := buildTaskSchedulerRegisterScript(state)
	if err != nil {
		return InstallState{}, err
	}
	_, err = taskSchedulerPowerShellRunner(ctx, script)
	return state, err
}

func utf16LEWithBOM(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	out := make([]byte, 2, 2+len(encoded)*2)
	out[0] = 0xff
	out[1] = 0xfe
	for _, value := range encoded {
		out = appendUTF16LE(out, value)
	}
	return out
}

func appendUTF16LE(out []byte, value uint16) []byte {
	return append(out, byte(value), byte(value>>8))
}

func uninstallTaskSchedulerLogonTask(ctx context.Context, state InstallState) error {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return err
	}
	_, err = taskSchedulerRunner(ctx, "/Delete", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/F")
	if err != nil && !isTaskSchedulerMissingErr(err) {
		return err
	}
	if err := serviceRemoveFile(state.ServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func taskSchedulerLogonEnable(ctx context.Context, state InstallState) error {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return err
	}
	_, err = taskSchedulerRunner(ctx, "/Change", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/ENABLE")
	return err
}

func taskSchedulerLogonDisable(ctx context.Context, state InstallState) error {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return err
	}
	_, err = taskSchedulerRunner(ctx, "/Change", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/DISABLE")
	return err
}

func taskSchedulerLogonStart(ctx context.Context, state InstallState) error {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return err
	}
	_, err = taskSchedulerRunner(ctx, "/Run", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID))
	return err
}

func taskSchedulerLogonStop(ctx context.Context, state InstallState) error {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return err
	}
	_, err = taskSchedulerRunner(ctx, "/End", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID))
	if isTaskSchedulerNotRunningErr(err) {
		return nil
	}
	return err
}

func taskSchedulerLogonStopAndWait(ctx context.Context, state InstallState, timeout, poll time.Duration) error {
	if err := taskSchedulerLogonStop(ctx, state); err != nil {
		return err
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	taskName := taskSchedulerTaskNameForInstance(state.InstanceID)
	for {
		running, err := taskSchedulerLogonIsRunning(ctx, state)
		if err != nil {
			return fmt.Errorf("confirm task scheduler stop for %s: %w", taskName, err)
		}
		if !running {
			return nil
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return fmt.Errorf("task scheduler task %s still running after %s", taskName, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

func taskSchedulerLogonRestart(ctx context.Context, state InstallState) error {
	if err := taskSchedulerLogonStop(ctx, state); err != nil {
		return err
	}
	return taskSchedulerLogonStart(ctx, state)
}

func taskSchedulerLogonStatus(ctx context.Context, state InstallState) (string, error) {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return "", err
	}
	return taskSchedulerRunner(ctx, "/Query", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/FO", "LIST", "/V")
}

func detectTaskSchedulerLogonEnabled(ctx context.Context, state InstallState) (bool, string, error) {
	state, err := taskSchedulerLogonServiceState(state)
	if err != nil {
		return false, "", err
	}
	output, err := taskSchedulerRunner(ctx, "/Query", "/TN", taskSchedulerTaskNameForInstance(state.InstanceID), "/XML")
	if err != nil {
		if isTaskSchedulerMissingErr(err) {
			return false, "", nil
		}
		return false, output, err
	}
	enabled, ok := parseTaskSchedulerEnabled(output)
	if !ok {
		return false, "无法解析自动启动任务状态。", nil
	}
	return enabled, "", nil
}

func taskSchedulerLogonIsRunning(ctx context.Context, state InstallState) (bool, error) {
	output, err := taskSchedulerLogonStatus(ctx, state)
	if err != nil {
		if isTaskSchedulerMissingErr(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(strings.ToLower(key)) != "status" {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "running"), nil
	}
	return false, nil
}

func parseTaskSchedulerEnabled(output string) (bool, bool) {
	text := strings.TrimSpace(output)
	if strings.HasPrefix(text, "<?xml") {
		if idx := strings.Index(text, "?>"); idx >= 0 {
			text = strings.TrimSpace(text[idx+2:])
		}
	}
	var doc struct {
		Triggers struct {
			LogonTrigger struct {
				Enabled string `xml:"Enabled"`
			} `xml:"LogonTrigger"`
		} `xml:"Triggers"`
		Settings struct {
			Enabled string `xml:"Enabled"`
		} `xml:"Settings"`
	}
	if err := xml.Unmarshal([]byte(text), &doc); err == nil {
		if enabled, ok := parseTaskSchedulerBool(doc.Settings.Enabled); ok {
			return enabled, true
		}
		if enabled, ok := parseTaskSchedulerBool(doc.Triggers.LogonTrigger.Enabled); ok {
			return enabled, true
		}
		// Task Scheduler omits the Enabled element when a task is enabled
		// (the default). A well-formed task without an explicit Enabled is
		// therefore treated as enabled.
		return true, true
	}
	text = strings.ToLower(text)
	if strings.Contains(text, "<settings>") {
		settings := text[strings.Index(text, "<settings>"):]
		if idx := strings.Index(settings, "</settings>"); idx >= 0 {
			settings = settings[:idx]
		}
		if strings.Contains(settings, "<enabled>true</enabled>") {
			return true, true
		}
		if strings.Contains(settings, "<enabled>false</enabled>") {
			return false, true
		}
	}
	switch {
	case strings.Contains(text, "<enabled>true</enabled>"):
		return true, true
	case strings.Contains(text, "<enabled>false</enabled>"):
		return false, true
	default:
		return false, false
	}
}

func parseTaskSchedulerBool(value string) (bool, bool) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}

func isTaskSchedulerMissingErr(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "cannot find") ||
		strings.Contains(text, "not found") ||
		strings.Contains(text, "does not exist") ||
		strings.Contains(text, "找不到") ||
		strings.Contains(text, "不存在")
}

func isTaskSchedulerNotRunningErr(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "not currently running") ||
		strings.Contains(text, "not running")
}
