package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

var launchctlUserRunner = runLaunchctl

func runLaunchctl(ctx context.Context, args ...string) (string, error) {
	cmd := execlaunch.CommandContext(ctx, "launchctl", args...)
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			return "", err
		}
		return trimmed, fmt.Errorf("%w: %s", err, trimmed)
	}
	return trimmed, nil
}

func ensureDarwinLaunchdUserSupport() error {
	if serviceRuntimeGOOS != "darwin" {
		return fmt.Errorf("launchd user service is only supported on darwin (current: %s)", serviceRuntimeGOOS)
	}
	return nil
}

func launchdUserGUITarget() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func launchdUserServiceTarget(state InstallState) string {
	return launchdUserGUITarget() + "/" + launchdLabelForInstance(state.InstanceID)
}

func isLaunchdAlreadyLoadedErr(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "already loaded")
}

func isLaunchdMissingErr(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(text, "could not find") ||
		strings.Contains(text, "no such process") ||
		strings.Contains(text, "service is not loaded")
}

func launchdUserServiceState(state InstallState) (InstallState, error) {
	if err := ensureDarwinLaunchdUserSupport(); err != nil {
		return InstallState{}, err
	}
	return managedServiceState(state, ServiceManagerLaunchdUser, launchdUserPlistPathForInstance, "launchd user plist path")
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func renderLaunchdUserPlist(state InstallState) (string, error) {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return "", err
	}
	binaryPath := normalizeServicePathValue(firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(state.CurrentBinaryPath)))
	if binaryPath == "" {
		return "", fmt.Errorf("installed binary path is missing")
	}

	layout := installLayoutForInstance(state.BaseDir, state.InstanceID)
	configHome := normalizeServicePathValue(layout.ConfigHome)
	dataHome := normalizeServicePathValue(layout.DataHome)
	stateHome := normalizeServicePathValue(layout.StateHome)
	label := launchdLabelForInstance(state.InstanceID)
	logPath := normalizeServicePathValue(filepath.Join(layout.StateDir, "logs", "codex-remote-relayd.log"))

	lines := []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<plist version="1.0">`,
		`<dict>`,
		`    <key>Label</key>`,
		`    <string>` + xmlEscape(label) + `</string>`,
		`    <key>ProgramArguments</key>`,
		`    <array>`,
		`        <string>` + xmlEscape(binaryPath) + `</string>`,
		`        <string>daemon</string>`,
		`    </array>`,
		`    <key>WorkingDirectory</key>`,
		`    <string>` + xmlEscape(normalizeServicePathValue(state.BaseDir)) + `</string>`,
		`    <key>EnvironmentVariables</key>`,
		`    <dict>`,
		`        <key>XDG_CONFIG_HOME</key>`,
		`        <string>` + xmlEscape(configHome) + `</string>`,
		`        <key>XDG_DATA_HOME</key>`,
		`        <string>` + xmlEscape(dataHome) + `</string>`,
		`        <key>XDG_STATE_HOME</key>`,
		`        <string>` + xmlEscape(stateHome) + `</string>`,
		`        <key>PATH</key>`,
		`        <string>` + xmlEscape(systemdUserServicePATH()) + `</string>`,
		`    </dict>`,
		`    <key>RunAtLoad</key>`,
		`    <true/>`,
		`    <key>KeepAlive</key>`,
		`    <dict>`,
		`        <key>SuccessfulExit</key>`,
		`        <false/>`,
		`    </dict>`,
		`    <key>StandardOutPath</key>`,
		`    <string>` + xmlEscape(logPath) + `</string>`,
		`    <key>StandardErrorPath</key>`,
		`    <string>` + xmlEscape(logPath) + `</string>`,
		`</dict>`,
		`</plist>`,
		``,
	}
	return strings.Join(lines, "\n"), nil
}

func installLaunchdUserPlist(ctx context.Context, state InstallState) (InstallState, error) {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return InstallState{}, err
	}
	plistContent, err := renderLaunchdUserPlist(state)
	if err != nil {
		return InstallState{}, err
	}
	layout := installLayoutForInstance(state.BaseDir, state.InstanceID)
	logsDir := filepath.Join(layout.StateDir, "logs")
	if err := serviceMkdirAll(logsDir, 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceMkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceWriteFile(state.ServiceUnitPath, []byte(plistContent), 0o644); err != nil {
		return InstallState{}, err
	}
	return state, nil
}

func uninstallLaunchdUserPlist(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	_, _ = launchctlUserRunner(ctx, "enable", launchdUserServiceTarget(state))
	_ = launchdUserBootout(ctx, state)
	if err := serviceRemoveFile(state.ServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func launchdUserBootstrap(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	_, err = launchctlUserRunner(ctx, "bootstrap", launchdUserGUITarget(), state.ServiceUnitPath)
	if isLaunchdAlreadyLoadedErr(err) {
		return nil
	}
	return err
}

func launchdUserBootout(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	_, err = launchctlUserRunner(ctx, "bootout", launchdUserServiceTarget(state))
	if isLaunchdMissingErr(err) {
		return nil
	}
	return err
}

func launchdUserEnable(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	if _, err := launchctlUserRunner(ctx, "enable", launchdUserServiceTarget(state)); err != nil {
		return err
	}
	return launchdUserBootstrap(ctx, state)
}

func launchdUserDisable(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	if _, err := launchctlUserRunner(ctx, "disable", launchdUserServiceTarget(state)); err != nil {
		return err
	}
	return launchdUserBootout(ctx, state)
}

func detectLaunchdUserEnabled(ctx context.Context, state InstallState) (bool, string, error) {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return false, "", err
	}
	output, err := launchctlUserRunner(ctx, "print-disabled", launchdUserGUITarget())
	if err != nil {
		return false, output, err
	}
	enabled, ok := parseLaunchdPrintDisabled(output, launchdLabelForInstance(state.InstanceID))
	if !ok {
		return false, "无法解析自动启动状态。", nil
	}
	return enabled, "", nil
}

func parseLaunchdPrintDisabled(output, label string) (bool, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return false, false
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, label) {
			continue
		}
		left, right, ok := strings.Cut(line, "=>")
		if !ok || !strings.Contains(left, label) {
			continue
		}
		switch strings.Trim(strings.TrimSpace(strings.ToLower(right)), ";") {
		case "true":
			return false, true
		case "false":
			return true, true
		default:
			return false, false
		}
	}
	// launchd services are enabled unless a disabled override says otherwise.
	return true, true
}

func launchdUserStart(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	if err := launchdUserBootstrap(ctx, state); err != nil {
		return err
	}
	_, err = launchctlUserRunner(ctx, "kickstart", launchdUserServiceTarget(state))
	return err
}

func launchdUserStop(ctx context.Context, state InstallState) error {
	return launchdUserBootout(ctx, state)
}

func launchdUserStopAndWait(ctx context.Context, state InstallState, timeout, poll time.Duration) error {
	if err := launchdUserStop(ctx, state); err != nil {
		return err
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	label := launchdLabelForInstance(state.InstanceID)
	for {
		running, err := launchdUserIsRunning(ctx, state)
		if err != nil {
			return fmt.Errorf("confirm launchd stop for %s: %w", label, err)
		}
		if !running {
			return nil
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return fmt.Errorf("launchd service %s still active after %s", label, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

func launchdUserRestart(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	if err := launchdUserBootout(ctx, state); err != nil {
		return err
	}
	return launchdUserBootstrap(ctx, state)
}

func launchdUserStatus(ctx context.Context, state InstallState) (string, error) {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return "", err
	}
	return launchctlUserRunner(ctx, "print", launchdUserServiceTarget(state))
}

func launchdUserIsRunning(ctx context.Context, state InstallState) (bool, error) {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return false, err
	}
	output, err := launchctlUserRunner(ctx, "print", launchdUserServiceTarget(state))
	if err != nil {
		if isLaunchdMissingErr(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "state = ") && strings.TrimSpace(strings.TrimPrefix(line, "state = ")) == "running" {
			return true, nil
		}
	}
	return false, nil
}
