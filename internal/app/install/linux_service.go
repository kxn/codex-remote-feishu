package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const systemdUserServiceName = "codex-remote.service"

var (
	serviceRuntimeGOOS  = runtime.GOOS
	serviceUserHomeDir  = os.UserHomeDir
	serviceMkdirAll     = os.MkdirAll
	serviceWriteFile    = os.WriteFile
	serviceRemoveFile   = os.Remove
	systemctlUserRunner = runSystemctlUser
)

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
		updated.ServiceUnitPath = systemdUserUnitPath(updated.BaseDir)
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

	configHome := filepath.Join(state.BaseDir, ".config")
	dataHome := filepath.Join(state.BaseDir, ".local", "share")
	stateHome := filepath.Join(state.BaseDir, ".local", "state")

	lines := []string{
		"[Unit]",
		"Description=codex-remote daemon",
		"After=network-online.target",
		"Wants=network-online.target",
		"",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=" + systemdEscapeValue(state.BaseDir),
		"ExecStart=" + systemdEscapeExecWord(binaryPath) + " daemon",
		"Environment=XDG_CONFIG_HOME=" + systemdEscapeValue(configHome),
		"Environment=XDG_DATA_HOME=" + systemdEscapeValue(dataHome),
		"Environment=XDG_STATE_HOME=" + systemdEscapeValue(stateHome),
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
	_, _ = systemctlUserRunner(ctx, "disable", "--now", systemdUserServiceName)
	if err := serviceRemoveFile(state.ServiceUnitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_, err = systemctlUserRunner(ctx, "daemon-reload")
	return err
}

func systemdUserEnable(ctx context.Context) error {
	_, err := systemctlUserRunner(ctx, "enable", systemdUserServiceName)
	return err
}

func systemdUserDisable(ctx context.Context) error {
	_, err := systemctlUserRunner(ctx, "disable", systemdUserServiceName)
	return err
}

func systemdUserStart(ctx context.Context) error {
	_, err := systemctlUserRunner(ctx, "start", systemdUserServiceName)
	return err
}

func systemdUserStop(ctx context.Context) error {
	_, err := systemctlUserRunner(ctx, "stop", systemdUserServiceName)
	return err
}

func systemdUserRestart(ctx context.Context) error {
	_, err := systemctlUserRunner(ctx, "restart", systemdUserServiceName)
	return err
}

func systemdUserStatus(ctx context.Context) (string, error) {
	return systemctlUserRunner(ctx, "status", "--no-pager", "--full", systemdUserServiceName)
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
