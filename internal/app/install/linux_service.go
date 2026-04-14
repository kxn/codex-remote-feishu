package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

var (
	serviceRuntimeGOOS  = runtime.GOOS
	serviceUserHomeDir  = pathscope.UserHomeDir
	serviceMkdirAll     = os.MkdirAll
	serviceWriteFile    = os.WriteFile
	serviceRemoveFile   = os.Remove
	systemctlUserRunner = runSystemctlUser
)

var defaultSystemdUserPATH = []string{
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
}

type systemdUserUnitState struct {
	ActiveState string
	MainPID     string
}

func runSystemctlUser(ctx context.Context, args ...string) (string, error) {
	commandArgs := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(ctx, "systemctl", commandArgs...)
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

func ensureLinuxSystemdUserSupport() error {
	if serviceRuntimeGOOS != "linux" {
		return fmt.Errorf("systemd user service is only supported on linux (current: %s)", serviceRuntimeGOOS)
	}
	return nil
}

func normalizedServiceState(state InstallState) InstallState {
	updated := state
	ApplyStateMetadata(&updated, StateMetadataOptions{
		InstanceID:     state.InstanceID,
		StatePath:      state.StatePath,
		BaseDir:        state.BaseDir,
		ServiceManager: state.ServiceManager,
	})
	return updated
}

func systemdUserServiceState(state InstallState) (InstallState, error) {
	if err := ensureLinuxSystemdUserSupport(); err != nil {
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
	updated.ServiceManager = ServiceManagerSystemdUser
	if strings.TrimSpace(updated.ServiceUnitPath) == "" {
		updated.ServiceUnitPath = systemdUserUnitPathForInstance(updated.BaseDir, updated.InstanceID)
	}
	if strings.TrimSpace(updated.ServiceUnitPath) == "" {
		return InstallState{}, fmt.Errorf("unable to resolve systemd user unit path")
	}
	return updated, nil
}

func renderSystemdUserUnit(state InstallState) (string, error) {
	state, err := systemdUserServiceState(state)
	if err != nil {
		return "", err
	}
	binaryPath := filepath.Clean(firstNonEmpty(strings.TrimSpace(state.InstalledBinary), strings.TrimSpace(state.CurrentBinaryPath)))
	if binaryPath == "" {
		return "", fmt.Errorf("installed binary path is missing")
	}

	layout := installLayoutForInstance(state.BaseDir, state.InstanceID)
	description := "codex-remote daemon"
	if !isDefaultInstance(state.InstanceID) {
		description = fmt.Sprintf("codex-remote daemon (%s)", state.InstanceID)
	}

	lines := []string{
		"[Unit]",
		"Description=" + description,
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=" + systemdEscapeValue(state.BaseDir),
		"ExecStart=" + systemdEscapeExecWord(binaryPath) + " daemon",
		"Environment=PATH=" + systemdEscapeValue(systemdUserServicePATH()),
		"Environment=XDG_CONFIG_HOME=" + systemdEscapeValue(layout.ConfigHome),
		"Environment=XDG_DATA_HOME=" + systemdEscapeValue(layout.DataHome),
		"Environment=XDG_STATE_HOME=" + systemdEscapeValue(layout.StateHome),
		"Restart=on-failure",
		"RestartSec=2s",
		"",
		"[Install]",
		"WantedBy=default.target",
		"",
	}
	return strings.Join(lines, "\n"), nil
}

func installSystemdUserUnit(ctx context.Context, state InstallState) (InstallState, error) {
	state, err := systemdUserServiceState(state)
	if err != nil {
		return InstallState{}, err
	}
	unitContent, err := renderSystemdUserUnit(state)
	if err != nil {
		return InstallState{}, err
	}
	if err := serviceMkdirAll(filepath.Dir(state.ServiceUnitPath), 0o755); err != nil {
		return InstallState{}, err
	}
	if err := serviceWriteFile(state.ServiceUnitPath, []byte(unitContent), 0o644); err != nil {
		return InstallState{}, err
	}
	if _, err := systemctlUserRunner(ctx, "daemon-reload"); err != nil {
		return InstallState{}, err
	}
	return state, nil
}

func uninstallSystemdUserUnit(ctx context.Context, state InstallState) error {
	state, err := systemdUserServiceState(state)
	if err != nil {
		return err
	}
	_, _ = systemctlUserRunner(ctx, "disable", "--now", systemdUserUnitName(state))
	if err := serviceRemoveFile(state.ServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err = systemctlUserRunner(ctx, "daemon-reload")
	return err
}

func systemdUserUnitName(state InstallState) string {
	if strings.TrimSpace(state.ServiceUnitPath) != "" {
		return filepath.Base(strings.TrimSpace(state.ServiceUnitPath))
	}
	return systemdUserServiceNameForInstance(state.InstanceID)
}

func systemdUserEnable(ctx context.Context, state InstallState) error {
	_, err := systemctlUserRunner(ctx, "enable", systemdUserUnitName(state))
	return err
}

func systemdUserDisable(ctx context.Context, state InstallState) error {
	_, err := systemctlUserRunner(ctx, "disable", systemdUserUnitName(state))
	return err
}

func systemdUserStart(ctx context.Context, state InstallState) error {
	_, err := systemctlUserRunner(ctx, "start", systemdUserUnitName(state))
	return err
}

func systemdUserStop(ctx context.Context, state InstallState) error {
	_, err := systemctlUserRunner(ctx, "stop", systemdUserUnitName(state))
	return err
}

func systemdUserStopAndWait(ctx context.Context, state InstallState, timeout, poll time.Duration) error {
	if err := systemdUserStop(ctx, state); err != nil {
		return err
	}
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	unitName := systemdUserUnitName(state)
	for {
		current, err := systemdUserReadUnitState(ctx, state)
		if err != nil {
			return fmt.Errorf("confirm systemd user stop for %s: %w", unitName, err)
		}
		if systemdUserUnitStopped(current) {
			return nil
		}
		if timeout <= 0 || time.Now().After(deadline) {
			return fmt.Errorf("systemd user service %s still active after %s (active=%s mainPID=%s)", unitName, timeout, current.ActiveState, current.MainPID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

func systemdUserReadUnitState(ctx context.Context, state InstallState) (systemdUserUnitState, error) {
	output, err := systemctlUserRunner(ctx, "show", "--property=ActiveState", "--property=MainPID", systemdUserUnitName(state))
	if err != nil {
		return systemdUserUnitState{}, err
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(output), "\r\n", "\n"), "\n")
	current := systemdUserUnitState{}
	var legacyValues []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.IndexByte(line, '='); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			switch key {
			case "ActiveState":
				current.ActiveState = value
			case "MainPID":
				current.MainPID = value
			}
			continue
		}
		legacyValues = append(legacyValues, line)
	}

	if current.ActiveState == "" && len(legacyValues) > 0 {
		current.ActiveState = legacyValues[0]
	}
	if current.MainPID == "" && len(legacyValues) > 1 {
		current.MainPID = legacyValues[1]
	}
	// Some systemctl variants can emit value-only rows in unexpected order.
	// If values look swapped, correct them before evaluating stop completion.
	if looksNumericMainPID(current.ActiveState) && !looksNumericMainPID(current.MainPID) {
		current.ActiveState, current.MainPID = current.MainPID, current.ActiveState
	}
	if current.ActiveState == "" {
		return systemdUserUnitState{}, fmt.Errorf("empty ActiveState")
	}
	return current, nil
}

func looksNumericMainPID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func systemdUserUnitStopped(current systemdUserUnitState) bool {
	switch strings.TrimSpace(strings.ToLower(current.ActiveState)) {
	case "inactive", "failed":
		pid := strings.TrimSpace(current.MainPID)
		return pid == "" || pid == "0"
	default:
		return false
	}
}

func systemdUserRestart(ctx context.Context, state InstallState) error {
	_, err := systemctlUserRunner(ctx, "restart", systemdUserUnitName(state))
	return err
}

func systemdUserStatus(ctx context.Context, state InstallState) (string, error) {
	return systemctlUserRunner(ctx, "status", "--no-pager", "--full", systemdUserUnitName(state))
}

func systemdEscapeValue(value string) string {
	replacer := strings.NewReplacer(
		`\\`, `\\\\`,
		` `, `\\x20`,
		"\t", `\\x09`,
		"\n", `\\x0a`,
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func systemdEscapeExecWord(value string) string {
	return systemdEscapeValue(value)
}

func systemdUserServicePATH() string {
	parts := normalizePathList(os.Getenv("PATH"))
	if len(parts) == 0 {
		return strings.Join(defaultSystemdUserPATH, string(os.PathListSeparator))
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func normalizePathList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	parts := strings.Split(value, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		part = filepath.Clean(part)
		key := part
		if serviceRuntimeGOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, part)
	}
	return out
}
