package wrapper

import (
	"context"
	"io"
	"os/exec"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/execlaunch"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type childSession struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.Reader
	stderr      io.Reader
	generation  int64
	waitErr     chan error
	cancel      context.CancelFunc
	ioCancel    context.CancelFunc
	writeCancel context.CancelFunc
}

func (a *App) launchCodexChildSession(ctx context.Context, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (*childSession, error) {
	childCtx, childCancel := context.WithCancel(ctx)
	childArgs, childEnv := a.buildCodexChildLaunch(a.config.Args)
	cmd := execlaunch.CommandContext(childCtx, a.config.CodexRealBinary, childArgs...)
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
	a.debugf("child started: binary=%s pid=%d cwd=%s", a.config.CodexRealBinary, cmd.Process.Pid, a.config.WorkspaceRoot)

	bootstrappedStdout, err := a.bootstrapHeadlessCodex(childStdin, childStdout, rawLogger, reportProblem)
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

func startChildSessionIO(ctx context.Context, session *childSession, parentStdout, parentStderr io.Writer, writeCh chan []byte, runtime backendRuntime, client *relayws.Client, commandResponses *commandResponseTracker, turnTracker *runtimeTurnTracker, activeGeneration *int64, generation int64, errCh chan<- error, debugf func(string, ...any), rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) {
	if session == nil {
		return
	}
	session.generation = generation
	if activeGeneration != nil {
		atomic.StoreInt64(activeGeneration, generation)
	}
	ioCtx, ioCancel := context.WithCancel(ctx)
	session.ioCancel = ioCancel
	writeCtx, writeCancel := context.WithCancel(ioCtx)
	session.writeCancel = writeCancel
	go writeLoop(writeCtx, session.stdin, writeCh, errCh, debugf, rawLogger, reportProblem)
	go stdoutLoop(ioCtx, session.stdout, parentStdout, writeCh, runtime, client, commandResponses, turnTracker, activeGeneration, generation, errCh, debugf, rawLogger, reportProblem)
	go streamCopy(session.stderr, parentStderr, errCh)
}

func stopChildSession(session *childSession, debugf func(string, ...any)) {
	if session == nil {
		return
	}
	if session.writeCancel != nil {
		session.writeCancel()
	}
	if session.ioCancel != nil {
		session.ioCancel()
	}
	if session.cmd != nil && session.cmd.Process != nil && session.cmd.Process.Pid > 0 {
		if err := relayruntime.TerminateProcess(session.cmd.Process.Pid, wrapperChildStopGrace); err != nil && debugf != nil {
			debugf("child stop failed: pid=%d err=%v", session.cmd.Process.Pid, err)
		}
	}
	if session.cancel != nil {
		session.cancel()
	}
	select {
	case <-session.waitErr:
	case <-time.After(wrapperChildWaitTimeout):
	}
}

func (a *App) restartChildSession(ctx context.Context, request restartRequest, current *childSession, parentStdout, parentStderr io.Writer, writeCh chan []byte, client *relayws.Client, commandResponses *commandResponseTracker, turnTracker *runtimeTurnTracker, activeGeneration *int64, generation int64, errCh chan<- error, rawLogger *debuglog.RawLogger, reportProblem func(agentproto.ErrorInfo)) (*childSession, error) {
	if err := a.runtime.PrepareChildRestart(request.CommandID, derefRestartTarget(request.Target)); err != nil {
		return nil, err
	}
	next, err := a.runtime.Launch(ctx, a, rawLogger, reportProblem)
	if err != nil {
		return nil, agentproto.ErrorInfo{
			Code:      "child_restart_launch_failed",
			Layer:     "wrapper",
			Stage:     "restart_child_launch",
			Operation: string(agentproto.CommandProcessChildRestart),
			Message:   "wrapper 无法拉起新的 Codex 子进程。",
			Details:   err.Error(),
		}
	}
	if next == nil {
		return nil, agentproto.ErrorInfo{
			Code:      "child_restart_launch_failed",
			Layer:     "wrapper",
			Stage:     "restart_child_launch",
			Operation: string(agentproto.CommandProcessChildRestart),
			Message:   "wrapper 无法为当前 backend 启动新的 provider child。",
			CommandID: request.CommandID,
		}
	}
	stopChildSession(current, a.debugf)
	startChildSessionIO(ctx, next, parentStdout, parentStderr, writeCh, a.runtime, client, commandResponses, turnTracker, activeGeneration, generation, errCh, a.debugf, rawLogger, reportProblem)
	restoreClient := client
	if !request.EmitEvent {
		restoreClient = nil
	}
	if err := a.restoreChildSessionContext(ctx, request.CommandID, writeCh, restoreClient, reportProblem); err != nil {
		return next, err
	}
	return next, nil
}

func derefRestartTarget(target *agentproto.Target) agentproto.Target {
	if target == nil {
		return agentproto.Target{}
	}
	return *target
}

func (a *App) restoreChildSessionContext(ctx context.Context, commandID string, writeCh chan []byte, client *relayws.Client, reportProblem func(agentproto.ErrorInfo)) error {
	frame, requestID, ok, err := a.runtime.BuildChildRestartRestoreFrame(commandID)
	if err != nil {
		emitChildRestartOutcome(client, commandID, "", agentproto.ChildRestartStatusFailed, &agentproto.ErrorInfo{
			Code:      "child_restart_restore_build_failed",
			Layer:     "wrapper",
			Stage:     "restart_child_restore_build",
			Operation: string(agentproto.CommandProcessChildRestart),
			Message:   "wrapper 无法构造重启后的 thread 恢复请求。",
			Details:   err.Error(),
			CommandID: commandID,
		}, reportProblem)
		return nil
	}
	if !ok {
		emitChildRestartOutcome(client, commandID, "", agentproto.ChildRestartStatusSucceeded, nil, reportProblem)
		return nil
	}
	select {
	case writeCh <- frame:
		return nil
	case <-ctx.Done():
		a.runtime.CancelChildRestartRestore(requestID)
		return ctx.Err()
	}
}

func emitChildRestartOutcome(client *relayws.Client, commandID, threadID string, status agentproto.ChildRestartStatus, problem *agentproto.ErrorInfo, reportProblem func(agentproto.ErrorInfo)) {
	if client == nil {
		return
	}
	event := agentproto.NewChildRestartUpdatedEvent(commandID, threadID, status, problem)
	if err := client.SendEvents([]agentproto.Event{event}); err != nil && reportProblem != nil {
		reportProblem(agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
			Code:      "relay_send_restart_outcome_failed",
			Layer:     "wrapper",
			Stage:     "forward_server_events",
			Operation: string(agentproto.CommandProcessChildRestart),
			Message:   "wrapper 无法把 child restart outcome 发送到 relay。",
			CommandID: commandID,
			ThreadID:  threadID,
			Retryable: true,
		}))
	}
}
