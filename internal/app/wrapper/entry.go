package wrapper

import (
	"context"
	"fmt"
	"io"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func RunMain(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, version, branch string) (int, error) {
	backend := agentproto.BackendCodex
	switch {
	case len(args) == 0:
		return 2, fmt.Errorf("wrapper role requires app-server or claude-app-server mode")
	case args[0] == "app-server":
		backend = agentproto.BackendCodex
	case args[0] == "claude-app-server":
		backend = agentproto.BackendClaude
	default:
		return 2, fmt.Errorf("wrapper role only supports app-server or claude-app-server mode")
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
