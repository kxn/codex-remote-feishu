package wrapper

import (
	"context"
	"fmt"
	"io"

	"github.com/kxn/codex-remote-feishu/internal/app/appserverargs"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func RunMain(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, version, branch string) (int, error) {
	backend, err := wrapperBackendFromArgs(args)
	if err != nil {
		return 2, err
	}

	cfg, err := LoadConfig(args, version, branch)
	if err != nil {
		return 1, err
	}
	cfg.Backend = backend

	app := New(cfg)
	exitCode, err := app.Run(ctx, stdin, stdout, stderr)
	if err != nil && err != context.Canceled {
		return exitCode, err
	}
	return exitCode, nil
}

func wrapperBackendFromArgs(args []string) (agentproto.Backend, error) {
	mode, ok := appserverargs.Find(args)
	if len(args) == 0 {
		return "", fmt.Errorf("wrapper role requires app-server, claude-app-server, or opencode-acp mode")
	}
	if !ok {
		return "", fmt.Errorf("wrapper role only supports app-server, claude-app-server, or opencode-acp mode")
	}
	switch mode.Mode {
	case appserverargs.ModeClaude:
		return agentproto.BackendClaude, nil
	case appserverargs.ModeOpenCode:
		return agentproto.BackendOpenCode, nil
	default:
		return agentproto.BackendCodex, nil
	}
}
