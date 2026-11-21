package codexupgrade

import (
	"context"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/codexupgrade/contract"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

// TrackedInstallOptions callbacks must persist before returning. A crash after
// Start but before OnStarted is deliberately unknown, never an install retry.
type TrackedInstallOptions struct {
	NPMCommand string
	Prefix     string // empty installs the actual global CLI; otherwise private prefix
	Env        []string
	OnStarted  func(hostinventory.Snapshot) error
	OnExited   func(TrackedInstallResult) error
}
type TrackedInstallResult = contract.TrackedInstallResult

func InstallTracked(ctx context.Context, version string, opts TrackedInstallOptions) (TrackedInstallResult, error) {
	var result TrackedInstallResult
	if !validVersion(version) || opts.OnStarted == nil || opts.OnExited == nil {
		return result, fmt.Errorf("tracked install requires exact version and durable lifecycle callbacks")
	}
	binary := opts.NPMCommand
	if binary == "" {
		binary = "npm"
	}
	args := []string{"install", "-g", "--no-audit", "--no-fund", "--registry=https://registry.npmjs.org", "@openai/codex@" + version}
	if opts.Prefix != "" {
		if !filepath.IsAbs(opts.Prefix) {
			return result, fmt.Errorf("private prefix must be absolute")
		}
		if err := os.MkdirAll(opts.Prefix, 0700); err != nil {
			return result, err
		}
		args = []string{"install", "--prefix", opts.Prefix, "--no-audit", "--no-fund", "--registry=https://registry.npmjs.org", "@openai/codex@" + version}
	}
	var err error
	binary, args, err = npmInvocation(binary, args)
	if err != nil {
		return result, err
	}

	cmd := execlaunch.CommandContext(ctx, binary, args...)
	cmd.Env = opts.Env
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	hostinventory.Prepare(cmd)
	if err := cmd.Start(); err != nil {
		return result, err
	}
	scope, err := hostinventory.Bind(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return result, err
	}
	defer scope.Close()
	if err = opts.OnStarted(scope.Inspect()); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return result, err
	}
	waitErr := cmd.Wait()
	scope.MarkControlledStop()
	scope.MarkWaitObserved()
	result = TrackedInstallResult{Scope: scope.Inspect(), ExitCode: cmd.ProcessState.ExitCode()}
	if err = opts.OnExited(result); err != nil {
		return result, err
	}
	if waitErr != nil {
		return result, fmt.Errorf("tracked npm install failed with exit code %d", result.ExitCode)
	}
	if !result.Scope.Complete || result.Scope.RootAlive || len(result.Scope.ProcessIDs) != 0 {
		return result, fmt.Errorf("installer descendants remain unverified")
	}
	return result, nil
}

var exactTargetVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

func ValidTargetVersion(v string) bool { return len(v) <= 128 && exactTargetVersion.MatchString(v) }
func validVersion(v string) bool       { return ValidTargetVersion(v) }
