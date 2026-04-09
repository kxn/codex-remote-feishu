package install

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type UpgradeHelperLaunchOptions struct {
	State        InstallState
	HelperBinary string
	StatePath    string
	LogPath      string
	Env          []string
	WorkDir      string
}

type systemdUserTransientCommandOptions struct {
	UnitName   string
	BinaryPath string
	Args       []string
	Env        []string
	WorkDir    string
	LogPath    string
}

var upgradeHelperStartDetachedCommandFunc = relayruntime.StartDetachedCommand
var upgradeHelperStartSystemdUserTransientFunc = startSystemdUserTransientCommand

func StartUpgradeHelperProcess(ctx context.Context, opts UpgradeHelperLaunchOptions) error {
	helperBinary := filepath.Clean(strings.TrimSpace(opts.HelperBinary))
	if helperBinary == "" {
		return fmt.Errorf("helper binary path is required")
	}
	statePath := filepath.Clean(strings.TrimSpace(opts.StatePath))
	if statePath == "" {
		return fmt.Errorf("state path is required")
	}

	args := []string{"upgrade-helper", "-state-path", statePath}
	if effectiveServiceManager(opts.State) == ServiceManagerSystemdUser && runtime.GOOS == "linux" {
		_, err := upgradeHelperStartSystemdUserTransientFunc(ctx, systemdUserTransientCommandOptions{
			UnitName:   uniqueUpgradeHelperUnitName(),
			BinaryPath: helperBinary,
			Args:       args,
			Env:        append([]string(nil), opts.Env...),
			WorkDir:    strings.TrimSpace(opts.WorkDir),
			LogPath:    strings.TrimSpace(opts.LogPath),
		})
		return err
	}

	_, err := upgradeHelperStartDetachedCommandFunc(relayruntime.DetachedCommandOptions{
		BinaryPath: helperBinary,
		Args:       args,
		Env:        append([]string(nil), opts.Env...),
		WorkDir:    strings.TrimSpace(opts.WorkDir),
		StdoutPath: strings.TrimSpace(opts.LogPath),
		StderrPath: strings.TrimSpace(opts.LogPath),
	})
	return err
}

func startSystemdUserTransientCommand(ctx context.Context, opts systemdUserTransientCommandOptions) (string, error) {
	args := []string{
		"--user",
		"--no-block",
		"--collect",
		"--quiet",
		"--service-type=exec",
		"--unit", strings.TrimSpace(opts.UnitName),
		"--description", "codex-remote upgrade helper",
	}
	if strings.TrimSpace(opts.WorkDir) != "" {
		args = append(args, "--working-directory", strings.TrimSpace(opts.WorkDir))
	}
	if strings.TrimSpace(opts.LogPath) != "" {
		logPath := strings.TrimSpace(opts.LogPath)
		args = append(args,
			"--property", "StandardOutput=append:"+logPath,
			"--property", "StandardError=append:"+logPath,
		)
	}
	for _, entry := range opts.Env {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		args = append(args, "--setenv="+entry)
	}
	args = append(args, filepath.Clean(opts.BinaryPath))
	args = append(args, opts.Args...)

	cmd := exec.CommandContext(ctx, "systemd-run", args...)
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

func uniqueUpgradeHelperUnitName() string {
	return fmt.Sprintf("codex-remote-upgrade-helper-%d.service", time.Now().UTC().UnixNano())
}
