package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	acpadapter "github.com/kxn/codex-remote-feishu/internal/adapter/acp"
	"github.com/kxn/codex-remote-feishu/internal/app/appserverargs"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
)

func (a *App) launchOpenCodeChildSession(ctx context.Context, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo), translator *acpadapter.Translator) (*childSession, error) {
	if translator == nil {
		return nil, fmt.Errorf("opencode translator is nil")
	}
	childCtx, childCancel := context.WithCancel(ctx)
	childArgs, childEnv := a.buildOpenCodeChildLaunch()
	translator.SetMCPServers(a.openCodeFeishuMCPServers())
	openCodeBinary, err := a.resolveOpenCodeBinary(childEnv)
	if err != nil {
		childCancel()
		return nil, err
	}
	cmd := execlaunch.CommandContext(childCtx, openCodeBinary, childArgs...)
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
	a.debugf("opencode child started: binary=%s pid=%d cwd=%s", openCodeBinary, cmd.Process.Pid, a.config.WorkspaceRoot)

	bootstrappedStdout, err := a.bootstrapOpenCodeACP(translator, childStdin, childStdout, rawLogger, reportProblem)
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

func (a *App) buildOpenCodeChildLaunch() ([]string, []string) {
	args := openCodeChildArgs(a.config.Args, a.config.WorkspaceRoot)
	env := config.FilterEnvWithoutProxy(append([]string{}, os.Environ()...))
	env = append(env, a.config.ChildProxyEnv...)
	return args, env
}

func openCodeChildArgs(wrapperArgs []string, workspaceRoot string) []string {
	var args []string
	if match, ok := appserverargs.Find(wrapperArgs); ok && match.Mode == appserverargs.ModeOpenCode {
		args = append([]string{}, wrapperArgs[match.Index+1:]...)
	}
	if len(args) == 0 {
		args = []string{"acp"}
	}
	if args[0] != "acp" {
		args = append([]string{"acp"}, args...)
	}
	if !hasOpenCodeCWDArg(args) {
		if cwd := strings.TrimSpace(workspaceRoot); cwd != "" {
			args = append(args, "--cwd", cwd)
		}
	}
	return args
}

func hasOpenCodeCWDArg(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "--cwd" {
			return true
		}
		if strings.HasPrefix(arg, "--cwd=") {
			return true
		}
	}
	return false
}

func (a *App) resolveOpenCodeBinary(env []string) (string, error) {
	configured := ""
	if loaded, err := config.LoadAppConfigAtPath(a.config.ConfigPath); err == nil {
		configured = loaded.Config.OpenCode.BinaryPath
	} else {
		a.debugf("opencode config load skipped for binary resolution: path=%s err=%v", a.config.ConfigPath, err)
	}
	resolved, err := config.ResolveOpenCodeBinary(env, configured)
	if err != nil {
		return "", agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:      "opencode_binary_not_found",
			Layer:     "wrapper",
			Stage:     "launch",
			Operation: "opencode_acp",
			Message:   "wrapper 找不到 OpenCode 可执行文件。",
			Retryable: true,
		})
	}
	return resolved, nil
}

func (a *App) bootstrapOpenCodeACP(translator *acpadapter.Translator, childStdin io.Writer, childStdout io.Reader, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (io.Reader, error) {
	frame, err := translator.BuildInitializeFrame()
	if err != nil {
		return nil, err
	}
	requestID, err := frameRequestID(frame)
	if err != nil {
		return nil, err
	}
	if err := writeChildFrameForRuntime(childStdin, frame, a.runtime, a.debugf, rawLogger, reportProblem); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(childStdout)
	var replay bytes.Buffer
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			logRawFrame(rawLogger, "codex.stdout", "in", line, "", "")
			a.debugf("opencode bootstrap: stdout from child: %s", summarizeFrame(line))
			matched, err := matchOpenCodeBootstrapInitializeResponse(line, requestID)
			if err != nil {
				return nil, err
			}
			if matched {
				if _, err := translator.ObserveServer(line); err != nil {
					return nil, err
				}
				return io.MultiReader(bytes.NewReader(replay.Bytes()), reader), nil
			}
			replay.Write(line)
		}
		if readErr == nil {
			continue
		}
		if readErr == io.EOF {
			return nil, fmt.Errorf("opencode bootstrap: initialize response %q not received before stdout closed", requestID)
		}
		return nil, readErr
	}
}

func frameRequestID(frame []byte) (string, error) {
	var message map[string]any
	if err := json.Unmarshal(frame, &message); err != nil {
		return "", err
	}
	id := strings.TrimSpace(lookupStringFromMap(message, "id"))
	if id == "" {
		return "", fmt.Errorf("frame missing string id")
	}
	return id, nil
}

func matchOpenCodeBootstrapInitializeResponse(line []byte, requestID string) (bool, error) {
	var message map[string]any
	if err := json.Unmarshal(line, &message); err != nil {
		return false, nil
	}
	if lookupStringFromMap(message, "id") != requestID {
		return false, nil
	}
	if errMsg := strings.TrimSpace(extractJSONRPCErrorMessage(message)); errMsg != "" {
		return true, fmt.Errorf("opencode bootstrap initialize failed: %s", errMsg)
	}
	if _, ok := message["result"]; !ok {
		return true, fmt.Errorf("opencode bootstrap initialize response missing result")
	}
	return true, nil
}
