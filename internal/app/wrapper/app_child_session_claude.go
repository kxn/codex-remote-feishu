package wrapper

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
)

func (a *App) launchClaudeChildSession(ctx context.Context, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo), resume *claudeLaunchResumeTarget) (*childSession, error) {
	childCtx, childCancel := context.WithCancel(ctx)
	childArgs, childEnv := a.buildClaudeChildLaunch(resume)
	claudeBinary := a.resolveClaudeBinary()
	cmd := execlaunch.CommandContext(childCtx, claudeBinary, childArgs...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Dir = pathcanon.Native(a.config.WorkspaceRoot)
	cmd.Env = childEnv

	childStdin, childStdout, childStderr, err := startChild(cmd)
	if err != nil {
		childCancel()
		return nil, err
	}
	a.debugf("claude child started: binary=%s pid=%d cwd=%s", claudeBinary, cmd.Process.Pid, a.config.WorkspaceRoot)

	bootstrappedStdout, err := a.runChildBootstrap(ctx, wrapperBootstrapTimeout, childCancel, func() (io.Reader, error) {
		return a.bootstrapClaude(childStdin, childStdout, rawLogger, reportProblem)
	})
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
		cmd:         cmd,
		stdin:       childStdin,
		stdout:      bootstrappedStdout,
		stderr:      childStderr,
		stdoutClose: childStdout,
		stderrClose: childStderr,
		waitErr:     waitErr,
		cancel:      childCancel,
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
		"--allow-dangerously-skip-permissions",
		"--permission-prompt-tool", "stdio",
	}
	if resume != nil && strings.TrimSpace(resume.ThreadID) != "" {
		args = append(args, "--resume", strings.TrimSpace(resume.ThreadID))
	}
	if resume != nil && resume.ForkEphemeral {
		args = append(args, "--fork-session")
	}
	if resume != nil && strings.TrimSpace(resume.ReviewerAgent) != "" {
		agents, _ := json.Marshal(map[string]any{
			strings.TrimSpace(resume.ReviewerAgent): map[string]any{
				"description": "Strict read-only code reviewer",
				"prompt":      "Review the supplied change for correctness, regressions, security, and missing tests. Do not modify files or run commands. Use only Read, Glob, and Grep when more context is needed.",
				"tools":       []string{"Read", "Glob", "Grep"},
			},
		})
		args = append(args,
			"--agents", string(agents),
			"--agent", strings.TrimSpace(resume.ReviewerAgent),
			"--tools", "Read,Glob,Grep",
			"--disallowedTools", "Edit,Write,MultiEdit,NotebookEdit,Task,Bash",
			"--permission-mode", "plan",
		)
		args = removeArgWithOptionalValue(args, "--allow-dangerously-skip-permissions")
	}
	env := config.FilterEnvWithoutProxy(append([]string{}, os.Environ()...))
	env = append(env, a.config.ChildProxyEnv...)
	args, env = a.applyClaudeRuntimeSettingsOverlay(args, env)
	args, env = a.applyClaudeFeishuMCPPublication(args, env)
	return args, env
}

func removeArgWithOptionalValue(args []string, key string) []string {
	if key == "" || len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] != key {
			filtered = append(filtered, args[i])
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
		}
	}
	return filtered
}
