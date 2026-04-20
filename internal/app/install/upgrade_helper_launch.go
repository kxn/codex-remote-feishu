package install

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type UpgradeHelperLaunchOptions struct {
	State        InstallState
	HelperBinary string
	StatePath    string
	LogPath      string
	Env          []string
	WorkDir      string
	DirectExec   bool
}

type systemdUserTransientCommandOptions struct {
	UnitName   string
	BinaryPath string
	Args       []string
	Env        []string
	WorkDir    string
	LogPath    string
}

type UpgradeHelperLaunchResult struct {
	UnitName string
}

var upgradeHelperStartDetachedCommandFunc = relayruntime.StartDetachedCommand
var upgradeHelperStartSystemdUserTransientFunc = startSystemdUserTransientCommand

func StartUpgradeHelperProcess(ctx context.Context, opts UpgradeHelperLaunchOptions) (UpgradeHelperLaunchResult, error) {
	helperBinary := filepath.Clean(strings.TrimSpace(opts.HelperBinary))
	if helperBinary == "" {
		return UpgradeHelperLaunchResult{}, fmt.Errorf("helper binary path is required")
	}
	statePath := filepath.Clean(strings.TrimSpace(opts.StatePath))
	if statePath == "" {
		return UpgradeHelperLaunchResult{}, fmt.Errorf("state path is required")
	}

	args := []string{"upgrade-helper", "-state-path", statePath}
	if opts.DirectExec {
		args = nil
	}
	if effectiveServiceManager(opts.State) == ServiceManagerSystemdUser && runtime.GOOS == "linux" {
		unitName := uniqueUpgradeHelperUnitName()
		_, err := upgradeHelperStartSystemdUserTransientFunc(ctx, systemdUserTransientCommandOptions{
			UnitName:   unitName,
			BinaryPath: helperBinary,
			Args:       args,
			Env:        append([]string(nil), opts.Env...),
			WorkDir:    strings.TrimSpace(opts.WorkDir),
			LogPath:    strings.TrimSpace(opts.LogPath),
		})
		if err != nil {
			return UpgradeHelperLaunchResult{}, err
		}
		return UpgradeHelperLaunchResult{UnitName: unitName}, nil
	}

	_, err := upgradeHelperStartDetachedCommandFunc(relayruntime.DetachedCommandOptions{
		BinaryPath: helperBinary,
		Args:       args,
		Env:        append([]string(nil), opts.Env...),
		WorkDir:    strings.TrimSpace(opts.WorkDir),
		StdoutPath: strings.TrimSpace(opts.LogPath),
		StderrPath: strings.TrimSpace(opts.LogPath),
	})
	if err != nil {
		return UpgradeHelperLaunchResult{}, err
	}
	return UpgradeHelperLaunchResult{}, nil
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

	cmd := execlaunch.CommandContext(ctx, "systemd-run", args...)
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
