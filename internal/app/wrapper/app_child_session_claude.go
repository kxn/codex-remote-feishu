package wrapper

import (
	"context"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func (a *App) launchClaudeChildSession(ctx context.Context, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo), resume *claudeLaunchResumeTarget) (*childSession, error) {
	childCtx, childCancel := context.WithCancel(ctx)
	childArgs, childEnv := a.buildClaudeChildLaunch(resume)
	claudeBinary := a.resolveClaudeBinary()
	cmd := execlaunch.CommandContext(childCtx, claudeBinary, childArgs...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Dir = a.config.WorkspaceRoot
	cmd.Env = childEnv

	childStdin, childStdout, childStderr, err := startChild(cmd)
	if err != nil {
		childCancel()
		return nil, err
	}
	a.debugf("claude child started: binary=%s pid=%d cwd=%s", claudeBinary, cmd.Process.Pid, a.config.WorkspaceRoot)

	bootstrappedStdout, err := a.bootstrapClaude(childStdin, childStdout, rawLogger, reportProblem)
	if err != nil {
		childCancel()
		_ = cmd.Wait()
		return nil, err
	}

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	return &childSession{
		cmd:     cmd,
		stdin:   childStdin,
		stdout:  bootstrappedStdout,
		stderr:  childStderr,
		waitErr: waitErr,
		cancel:  childCancel,
	}, nil
}

func (a *App) resolveClaudeBinary() string {
	if resolved, err := config.ResolveClaudeBinary(os.Environ()); err == nil && strings.TrimSpace(resolved) != "" {
		return resolved
	}
	if value := strings.TrimSpace(os.Getenv(config.ClaudeBinaryEnv)); value != "" {
		return value
	}
	return "claude"
}

func (a *App) buildClaudeChildLaunch(resume *claudeLaunchResumeTarget) ([]string, []string) {
	args := []string{
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--replay-user-messages",
		"--verbose",
		"--permission-prompt-tool", "stdio",
	}
	if resume != nil && strings.TrimSpace(resume.ThreadID) != "" {
		args = append(args, "--resume", strings.TrimSpace(resume.ThreadID))
	}
	env := config.FilterEnvWithoutProxy(append([]string{}, os.Environ()...))
	env = append(env, a.config.ChildProxyEnv...)
	args, env = a.applyClaudeRuntimeSettingsOverlay(args, env)
	args, env = a.applyClaudeFeishuMCPPublication(args, env)
	return args, env
}
