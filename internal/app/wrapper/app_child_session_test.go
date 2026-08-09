package wrapper

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
)

type blockingReader struct {
	done chan struct{}
	err  error
}

func newBlockingReader(err error) *blockingReader {
	return &blockingReader{
		done: make(chan struct{}),
		err:  err,
	}
}

func (r *blockingReader) Read(_ []byte) (int, error) {
	<-r.done
	return 0, r.err
}

func (r *blockingReader) Close() {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
}

func TestRestoreChildSessionContextClearsPendingStateOnCancelBeforeQueue(t *testing.T) {
	app := New(Config{
		InstanceID: "inst-1",
	})
	runtime, ok := app.runtime.(*codexBackendRuntime)
	if !ok {
		t.Fatalf("expected codex runtime, got %T", app.runtime)
	}
	if _, err := runtime.ObserveClient([]byte(`{"method":"thread/resume","params":{"threadId":"thread-1","cwd":"/tmp/project"}}`)); err != nil {
		t.Fatalf("seed thread/resume: %v", err)
	}

	writeCh := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- app.restoreChildSessionContext(ctx, "cmd-restart-1", writeCh, &relayws.Client{}, nil)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected restore cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for restore cancellation")
	}
}

func TestStdoutLoopIgnoresClosedPipeAfterSessionCancel(t *testing.T) {
	reader := newBlockingReader(errors.New("read |0: file already closed"))
	runtime, ok := newBackendRuntime(Config{InstanceID: "inst-1"}).(*codexBackendRuntime)
	if !ok {
		t.Fatal("expected codex runtime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		stdoutLoop(ctx, reader, io.Discard, make(chan []byte, 1), runtime, nil, newCommandResponseTracker(), newRuntimeTurnTracker(), nil, 0, errCh, nil, nil, nil, done)
	}()

	cancel()
	reader.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stdoutLoop to exit")
	}

	select {
	case err := <-errCh:
		t.Fatalf("expected canceled session to suppress closed-pipe error, got %v", err)
	default:
	}
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

type restartOrderFakeRuntime struct {
	launchSawCurrentStopped bool
	currentStopped          <-chan struct{}
	next                    *childSession
}

func (r *restartOrderFakeRuntime) Backend() agentproto.Backend {
	return agentproto.BackendClaude
}

func (r *restartOrderFakeRuntime) Capabilities() agentproto.Capabilities {
	return agentproto.Capabilities{}
}

func (r *restartOrderFakeRuntime) Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error) {
	select {
	case <-r.currentStopped:
		r.launchSawCurrentStopped = true
	default:
	}
	return r.next, nil
}

func (r *restartOrderFakeRuntime) ObserveClient([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{}, nil
}

func (r *restartOrderFakeRuntime) ObserveServer([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{}, nil
}

func (r *restartOrderFakeRuntime) TranslateCommand(agentproto.Command) (runtimeCommandResult, error) {
	return runtimeCommandResult{}, nil
}

func (r *restartOrderFakeRuntime) PrepareChildRestart(string, agentproto.PromptDispatchPlan, *agentproto.CodexResumePolicy) error {
	return nil
}

func (r *restartOrderFakeRuntime) BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error) {
	return nil, "", false, nil
}

func (r *restartOrderFakeRuntime) CancelChildRestartRestore(string) {}

type stdoutTestRuntime struct {
	backend       agentproto.Backend
	observeServer func([]byte) (runtimeObserveResult, error)
}

func (r stdoutTestRuntime) Backend() agentproto.Backend {
	return r.backend
}

func (r stdoutTestRuntime) Capabilities() agentproto.Capabilities {
	return agentproto.Capabilities{}
}

func (r stdoutTestRuntime) Launch(context.Context, *App, *debuglog.RawLogger, func(agentproto.ErrorInfo)) (*childSession, error) {
	return nil, nil
}

func (r stdoutTestRuntime) ObserveClient([]byte) (runtimeObserveResult, error) {
	return runtimeObserveResult{}, nil
}

func (r stdoutTestRuntime) ObserveServer(line []byte) (runtimeObserveResult, error) {
	if r.observeServer != nil {
		return r.observeServer(line)
	}
	return runtimeObserveResult{}, nil
}

func (r stdoutTestRuntime) TranslateCommand(agentproto.Command) (runtimeCommandResult, error) {
	return runtimeCommandResult{}, nil
}

func (r stdoutTestRuntime) PrepareChildRestart(string, agentproto.PromptDispatchPlan, *agentproto.CodexResumePolicy) error {
	return nil
}

func (r stdoutTestRuntime) BuildChildRestartRestoreFrame(string) ([]byte, string, bool, error) {
	return nil, "", false, nil
}

func (r stdoutTestRuntime) CancelChildRestartRestore(string) {}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestStdoutLoopReportsOpenCodeParseFailureWithBackendSpecificContext(t *testing.T) {
	var reported agentproto.ErrorInfo
	reportProblem := func(problem agentproto.ErrorInfo) {
		reported = problem
	}
	ctx := context.Background()
	done := make(chan struct{})
	errCh := make(chan error, 1)
	stdoutLoop(
		ctx,
		strings.NewReader("not-json\n"),
		io.Discard,
		make(chan []byte, 1),
		stdoutTestRuntime{
			backend: agentproto.BackendOpenCode,
			observeServer: func([]byte) (runtimeObserveResult, error) {
				return runtimeObserveResult{}, errors.New("native parse failed")
			},
		},
		nil,
		newCommandResponseTracker(),
		newRuntimeTurnTracker(),
		nil,
		0,
		errCh,
		nil,
		nil,
		reportProblem,
		done,
	)

	if reported.Code != "stdout_parse_failed" {
		t.Fatalf("reported code = %q, want stdout_parse_failed", reported.Code)
	}
	if reported.Stage != "observe_opencode_stdout" {
		t.Fatalf("reported stage = %q, want observe_opencode_stdout", reported.Stage)
	}
	if reported.Operation != "opencode.stdout" {
		t.Fatalf("reported operation = %q, want opencode.stdout", reported.Operation)
	}
	if strings.Contains(reported.Message, "Codex") {
		t.Fatalf("opencode parse failure message must not mention Codex, got %q", reported.Message)
	}
	select {
	case err := <-errCh:
		t.Fatalf("stdoutLoop reported unexpected async error: %v", err)
	default:
	}
}

func TestStdoutLoopReportsOpenCodeParentWriteFailureWithBackendSpecificMessage(t *testing.T) {
	writeErr := errors.New("parent pipe closed")
	var reported agentproto.ErrorInfo
	reportProblem := func(problem agentproto.ErrorInfo) {
		reported = problem
	}
	ctx := context.Background()
	done := make(chan struct{})
	errCh := make(chan error, 1)
	stdoutLoop(
		ctx,
		strings.NewReader("not-json\n"),
		failingWriter{err: writeErr},
		make(chan []byte, 1),
		stdoutTestRuntime{backend: agentproto.BackendOpenCode},
		nil,
		newCommandResponseTracker(),
		newRuntimeTurnTracker(),
		nil,
		0,
		errCh,
		nil,
		nil,
		reportProblem,
		done,
	)

	if reported.Code != "write_parent_stdout_failed" {
		t.Fatalf("reported code = %q, want write_parent_stdout_failed", reported.Code)
	}
	if strings.Contains(reported.Message, "Codex") {
		t.Fatalf("opencode write failure message must not mention Codex, got %q", reported.Message)
	}
	if !strings.Contains(reported.Message, "OpenCode") {
		t.Fatalf("opencode write failure message should mention OpenCode, got %q", reported.Message)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, writeErr) {
			t.Fatalf("stdoutLoop error = %v, want %v", err, writeErr)
		}
	default:
		t.Fatal("expected stdoutLoop to report parent write error")
	}
}

func TestWriteChildFrameReportsOpenCodeFailureWithBackendSpecificContext(t *testing.T) {
	writeErr := errors.New("child pipe closed")
	var reported agentproto.ErrorInfo
	reportProblem := func(problem agentproto.ErrorInfo) {
		reported = problem
	}

	err := writeChildFrameForRuntime(
		failingWriter{err: writeErr},
		[]byte(`{"jsonrpc":"2.0","method":"session/prompt"}`+"\n"),
		stdoutTestRuntime{backend: agentproto.BackendOpenCode},
		nil,
		nil,
		reportProblem,
	)

	if !errors.Is(err, writeErr) {
		t.Fatalf("writeChildFrameForRuntime error = %v, want %v", err, writeErr)
	}
	if reported.Code != "write_opencode_stdin_failed" {
		t.Fatalf("reported code = %q, want write_opencode_stdin_failed", reported.Code)
	}
	if reported.Stage != "write_opencode_stdin" {
		t.Fatalf("reported stage = %q, want write_opencode_stdin", reported.Stage)
	}
	if reported.Operation != "opencode.stdin" {
		t.Fatalf("reported operation = %q, want opencode.stdin", reported.Operation)
	}
	if strings.Contains(reported.Message, "Codex") {
		t.Fatalf("opencode stdin failure message must not mention Codex, got %q", reported.Message)
	}
}

func TestRestartChildSessionStopsCurrentIOBeforeLaunchingReplacement(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	currentStopped := make(chan struct{})
	currentWriteDone := make(chan struct{})
	currentStdoutDone := make(chan struct{})
	currentStderrDone := make(chan struct{})
	current := &childSession{
		writeDone:  currentWriteDone,
		stdoutDone: currentStdoutDone,
		stderrDone: currentStderrDone,
		cancel: func() {
			select {
			case <-currentStopped:
			default:
				close(currentStopped)
				close(currentWriteDone)
				close(currentStdoutDone)
				close(currentStderrDone)
			}
		},
	}
	next := &childSession{
		stdin:   nopWriteCloser{Writer: io.Discard},
		stdout:  io.NopCloser(strings.NewReader("")),
		stderr:  io.NopCloser(strings.NewReader("")),
		waitErr: make(chan error, 1),
	}
	runtime := &restartOrderFakeRuntime{
		currentStopped: currentStopped,
		next:           next,
	}
	app := &App{runtime: runtime}
	writeCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	var activeGeneration int64

	restarted, err := app.restartChildSession(ctx, restartRequest{CommandID: "cmd-restart"}, current, io.Discard, io.Discard, writeCh, nil, newCommandResponseTracker(), newRuntimeTurnTracker(), &activeGeneration, 2, errCh, nil, nil)
	if err != nil {
		t.Fatalf("restartChildSession: %v", err)
	}
	if restarted != next {
		t.Fatalf("restartChildSession returned unexpected child: got %p want %p", restarted, next)
	}
	if !runtime.launchSawCurrentStopped {
		t.Fatal("expected replacement launch to start only after current child IO was fenced")
	}

	cancel()
	waitForSessionIOStopped(next, 2*time.Second)
}
