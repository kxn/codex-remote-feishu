package launcher

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper"
)

type Options struct {
	Args    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	Version string
	Branch  string

	Runners RunnerSet
}

type RunnerSet struct {
	RunDaemon        func(context.Context, string, string) error
	RunInstall       func([]string, io.Reader, io.Writer, io.Writer, string) error
	RunLocalUpgrade  func([]string, io.Reader, io.Writer, io.Writer, string) error
	RunService       func([]string, io.Reader, io.Writer, io.Writer, string) error
	RunUpgradeHelper func([]string, io.Reader, io.Writer, io.Writer, string) error
	RunWrapper       func(context.Context, []string, io.Reader, io.Writer, io.Writer, string, string) (int, error)
}

func Main(opts Options) int {
	opts = withDefaults(opts)

	decision, err := Detect(opts.Args)
	if err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "error: %v\n\n%s", err, usageText())
		return 2
	}

	switch decision.Role {
	case RoleHelp:
		_, _ = io.WriteString(opts.Stdout, usageText())
		return 0
	case RoleVersion:
		_, _ = fmt.Fprintf(opts.Stdout, "%s\n", opts.Version)
		return 0
	}

	ctx, stop, err := newMainContext(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(opts.Stderr, "signal setup error: %v\n", err)
		return 1
	}
	defer stop()

	switch decision.Role {
	case RoleDaemon:
		if err := opts.Runners.RunDaemon(ctx, opts.Version, opts.Branch); err != nil && err != context.Canceled {
			_, _ = fmt.Fprintf(opts.Stderr, "daemon error: %v\n", err)
			return 1
		}
		return 0
	case RoleInstall:
		if err := opts.Runners.RunInstall(decision.Args, opts.Stdin, opts.Stdout, opts.Stderr, opts.Version); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "install error: %v\n", err)
			return 1
		}
		return 0
	case RoleLocalUpgrade:
		if err := opts.Runners.RunLocalUpgrade(decision.Args, opts.Stdin, opts.Stdout, opts.Stderr, opts.Version); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "local upgrade error: %v\n", err)
			return 1
		}
		return 0
	case RoleService:
		if err := opts.Runners.RunService(decision.Args, opts.Stdin, opts.Stdout, opts.Stderr, opts.Version); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "service error: %v\n", err)
			return 1
		}
		return 0
	case RoleUpgradeHelper:
		if err := opts.Runners.RunUpgradeHelper(decision.Args, opts.Stdin, opts.Stdout, opts.Stderr, opts.Version); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "upgrade helper error: %v\n", err)
			return 1
		}
		return 0
	case RoleWrapper:
		exitCode, err := opts.Runners.RunWrapper(ctx, decision.Args, opts.Stdin, opts.Stdout, opts.Stderr, opts.Version, opts.Branch)
		if err != nil && err != context.Canceled {
			_, _ = fmt.Fprintf(opts.Stderr, "wrapper error: %v\n", err)
			if exitCode == 0 {
				return 1
			}
			return exitCode
		}
		return exitCode
	default:
		_, _ = fmt.Fprintf(opts.Stderr, "error: unhandled role %q\n", decision.Role)
		return 1
	}
}

func withDefaults(opts Options) Options {
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Branch == "" {
		opts.Branch = "dev"
	}
	if opts.Runners.RunDaemon == nil {
		opts.Runners.RunDaemon = daemon.RunMain
	}
	if opts.Runners.RunInstall == nil {
		opts.Runners.RunInstall = install.RunMain
	}
	if opts.Runners.RunLocalUpgrade == nil {
		opts.Runners.RunLocalUpgrade = install.RunLocalUpgrade
	}
	if opts.Runners.RunService == nil {
		opts.Runners.RunService = install.RunService
	}
	if opts.Runners.RunUpgradeHelper == nil {
		opts.Runners.RunUpgradeHelper = install.RunUpgradeHelper
	}
	if opts.Runners.RunWrapper == nil {
		opts.Runners.RunWrapper = wrapper.RunMain
	}
	return opts
}

func usageText() string {
	return `Usage:
  codex-remote
  codex-remote daemon
  codex-remote install [flags]
  codex-remote local-upgrade [flags]
  codex-remote service <subcommand> [flags]
  codex-remote app-server [codex app-server args...]
  codex-remote wrapper app-server [codex app-server args...]
  codex-remote version
  codex-remote help

Notes:
  - no arguments defaults to daemon mode
  - wrapper role only supports Codex app-server mode
  - unknown top-level commands do not fall through to wrapper
`
}
