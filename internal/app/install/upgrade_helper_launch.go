package install

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
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
	helperBinary := normalizeUpgradeHelperLaunchPath(opts.HelperBinary)
	if helperBinary == "" {
		return UpgradeHelperLaunchResult{}, fmt.Errorf("helper binary path is required")
	}
	statePath := normalizeUpgradeHelperLaunchPath(opts.StatePath)
	if statePath == "" {
		return UpgradeHelperLaunchResult{}, fmt.Errorf("state path is required")
	}
	workDir := normalizeUpgradeHelperLaunchPath(opts.WorkDir)
	logPath := normalizeUpgradeHelperLaunchPath(opts.LogPath)

	args := []string{"upgrade-helper", "-state-path", statePath}
	if opts.DirectExec {
		args = nil
	}
	if effectiveServiceManager(opts.State) == ServiceManagerSystemdUser && serviceRuntimeGOOS == "linux" {
		unitName := uniqueUpgradeHelperUnitName()
		_, err := upgradeHelperStartSystemdUserTransientFunc(ctx, systemdUserTransientCommandOptions{
			UnitName:   unitName,
			BinaryPath: helperBinary,
			Args:       args,
			Env:        append([]string(nil), opts.Env...),
			WorkDir:    workDir,
			LogPath:    logPath,
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
		WorkDir:    workDir,
		StdoutPath: logPath,
		StderrPath: logPath,
	})
	if err != nil {
		return UpgradeHelperLaunchResult{}, err
	}
	return UpgradeHelperLaunchResult{}, nil
}

func startSystemdUserTransientCommand(ctx context.Context, opts systemdUserTransientCommandOptions) (string, error) {
	workDir := normalizeUpgradeHelperLaunchPath(opts.WorkDir)
	logPath := normalizeUpgradeHelperLaunchPath(opts.LogPath)
	args := []string{
		"--user",
		"--no-block",
		"--collect",
		"--quiet",
		"--service-type=exec",
		"--unit", strings.TrimSpace(opts.UnitName),
		"--description", "codex-remote upgrade helper",
	}
	if workDir != "" {
		args = append(args, "--working-directory", workDir)
	}
	if logPath != "" {
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
	args = append(args, normalizeUpgradeHelperLaunchPath(opts.BinaryPath))
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

func normalizeUpgradeHelperLaunchPath(path string) string {
	return pathcanon.Native(path)
}
