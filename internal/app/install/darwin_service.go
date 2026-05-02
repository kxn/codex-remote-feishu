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

func launchdUserServiceState(state InstallState) (InstallState, error) {
	if err := ensureDarwinLaunchdUserSupport(); err != nil {
		return InstallState{}, err
	}
	updated := normalizedServiceState(state)
	if strings.TrimSpace(updated.BaseDir) == "" {
		homeDir, err := serviceUserHomeDir()
		if err != nil {
			return InstallState{}, err
		}
		updated.BaseDir = homeDir
	}
	updated.ServiceManager = ServiceManagerLaunchdUser
	if strings.TrimSpace(updated.ServiceUnitPath) == "" {
		updated.ServiceUnitPath = launchdUserPlistPathForInstance(updated.BaseDir, updated.InstanceID)
	}
	if strings.TrimSpace(updated.ServiceUnitPath) == "" {
		return InstallState{}, fmt.Errorf("unable to resolve launchd user plist path")
	}
	return updated, nil
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
	if err := serviceMkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceWriteFile(state.ServiceUnitPath, []byte(plistContent), 0o644); err != nil {
		return InstallState{}, err
	}
	domain := launchdUserGUITarget()
	if _, err := launchctlUserRunner(ctx, "bootstrap", domain, state.ServiceUnitPath); err != nil {
		return InstallState{}, err
	}
	return state, nil
}

func uninstallLaunchdUserPlist(ctx context.Context, state InstallState) error {
	state, err := launchdUserServiceState(state)
	if err != nil {
		return err
	}
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	_, _ = launchctlUserRunner(ctx, "bootout", domain+"/"+label)
	if err := serviceRemoveFile(state.ServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func launchdUserEnable(ctx context.Context, state InstallState) error {
	_, err := installLaunchdUserPlist(ctx, state)
	return err
}

func launchdUserDisable(ctx context.Context, state InstallState) error {
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	_, err := launchctlUserRunner(ctx, "bootout", domain+"/"+label)
	return err
}

func launchdUserStart(ctx context.Context, state InstallState) error {
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	_, err := launchctlUserRunner(ctx, "kickstart", domain+"/"+label)
	return err
}

func launchdUserStop(ctx context.Context, state InstallState) error {
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	_, err := launchctlUserRunner(ctx, "bootout", domain+"/"+label)
	return err
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
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	_, err := launchctlUserRunner(ctx, "kickstart", "-k", domain+"/"+label)
	return err
}

func launchdUserStatus(ctx context.Context, state InstallState) (string, error) {
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	return launchctlUserRunner(ctx, "print", domain+"/"+label)
}

func launchdUserIsRunning(ctx context.Context, state InstallState) (bool, error) {
	label := launchdLabelForInstance(state.InstanceID)
	domain := launchdUserGUITarget()
	output, err := launchctlUserRunner(ctx, "print", domain+"/"+label)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "could not find") ||
			strings.Contains(strings.ToLower(err.Error()), "no such process") {
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
