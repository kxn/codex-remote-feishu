package wrapper

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestWrapperClaudeSkeletonHelloAndShutdown(t *testing.T) {
	repoRoot := wrapperTestRepoRoot(t)

	helloCh := make(chan agentproto.Hello, 1)
	ackCh := make(chan agentproto.CommandAck, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnCommandAck: func(_ context.Context, _ relayws.ConnectionMeta, _ string, ack agentproto.CommandAck) {
			ackCh <- ack
		},
	})
	server.SetServerIdentity(agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{
			Product:          "codex-remote",
			Version:          "test",
			BuildFingerprint: "fp-test",
			BinaryPath:       "/test/codex-remote",
		},
		PID: 1,
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := New(Config{
		RelayServerURL:   "ws" + strings.TrimPrefix(httpServer.URL, "http"),
		InstanceID:       "inst-claude-skeleton",
		DisplayName:      "claude-skeleton",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Backend:          agentproto.BackendClaude,
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	hello := waitForHello(t, helloCh, "inst-claude-skeleton")
	if hello.Instance.Backend != agentproto.BackendClaude {
		t.Fatalf("wrapper hello backend = %q, want %q", hello.Instance.Backend, agentproto.BackendClaude)
	}
	if hello.Capabilities.ThreadsRefresh || hello.Capabilities.TurnSteer || hello.Capabilities.RequestRespond || hello.Capabilities.SessionCatalog || hello.Capabilities.ResumeByThreadID || hello.Capabilities.RequiresCWDForResume || hello.Capabilities.VSCodeMode {
		t.Fatalf("claude skeleton unexpectedly advertised capabilities: %#v", hello.Capabilities)
	}

	if err := server.SendCommand("inst-claude-skeleton", agentproto.Command{
		CommandID: "cmd-exit-claude",
		Kind:      agentproto.CommandProcessExit,
	}); err != nil {
		t.Fatalf("send process exit: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-exit-claude" && ack.Accepted
	}, &stdout, &stderr, done)

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for claude skeleton shutdown")
	}
}

func TestWrapperReconcilesCompletedTurnWhenChildExitsAfterFinalOutput(t *testing.T) {
	_, eventsCh, _, stdout, stderr, done := startWrapperRuntimeTestApp(t, Config{
		CodexRealBinary: "go",
		Args: []string{
			"run", "./testkit/mockcodex/cmd/mockcodex",
			"--exit-after-final-output",
		},
		InstanceID: "inst-exit-completed",
	})

	waitForObservedEvents(t, eventsCh, 15*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemCompleted && event.ItemKind == "agent_message"
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted && event.Status == "completed" && strings.TrimSpace(event.ErrorMessage) == "" && event.Problem == nil
		},
	)
}

func TestWrapperReconcilesFailedTurnWhenChildExitsAfterFinalOutputError(t *testing.T) {
	_, eventsCh, _, stdout, stderr, done := startWrapperRuntimeTestApp(t, Config{
		CodexRealBinary: "go",
		Args: []string{
			"run", "./testkit/mockcodex/cmd/mockcodex",
			"--exit-after-final-output",
			"--exit-after-final-output-code=1",
		},
		InstanceID: "inst-exit-failed",
	})

	waitForObservedEvents(t, eventsCh, 15*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemCompleted && event.ItemKind == "agent_message"
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted &&
				event.Status == "failed" &&
				event.Problem != nil &&
				event.Problem.Code == "runtime_exit_before_turn_completed"
		},
	)
}

func TestWrapperReconcilesInterruptedTurnWhenChildExitsAfterInterruptAck(t *testing.T) {
	server, eventsCh, _, stdout, stderr, done := startWrapperRuntimeTestApp(t, Config{
		CodexRealBinary: "go",
		Args: []string{
			"run", "./testkit/mockcodex/cmd/mockcodex",
			"--no-auto-complete",
			"--exit-after-interrupt",
		},
		InstanceID: "inst-exit-interrupted",
	})

	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted && event.ThreadID == "thread-1"
		})
	}, stdout, stderr, done)

	if err := server.SendCommand("inst-exit-interrupted", agentproto.Command{
		CommandID: "cmd-interrupt",
		Kind:      agentproto.CommandTurnInterrupt,
		Target: agentproto.Target{
			ThreadID: "thread-1",
		},
	}); err != nil {
		t.Fatalf("send interrupt: %v", err)
	}

	waitForObservedEvents(t, eventsCh, 10*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted && event.Status == "interrupted" && event.ThreadID == "thread-1"
		},
	)
}

func startWrapperRuntimeTestApp(t *testing.T, cfg Config) (*relayws.Server, <-chan []agentproto.Event, <-chan agentproto.CommandAck, *bytes.Buffer, *bytes.Buffer, <-chan error) {
	t.Helper()
	repoRoot := wrapperTestRepoRoot(t)

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 16)
	ackCh := make(chan agentproto.CommandAck, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ relayws.ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
		},
		OnCommandAck: func(_ context.Context, _ relayws.ConnectionMeta, _ string, ack agentproto.CommandAck) {
			ackCh <- ack
		},
	})
	server.SetServerIdentity(agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{
			Product:          "codex-remote",
			Version:          "test",
			BuildFingerprint: "fp-test",
			BinaryPath:       "/test/codex-remote",
		},
		PID: 1,
	})

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	t.Cleanup(httpServer.Close)
	t.Cleanup(func() {
		_ = server.Close()
	})

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cfg.RelayServerURL = "ws" + strings.TrimPrefix(httpServer.URL, "http")
	cfg.DisplayName = firstNonEmpty(cfg.DisplayName, "codex-remote")
	cfg.WorkspaceRoot = firstNonEmpty(cfg.WorkspaceRoot, repoRoot)
	cfg.WorkspaceKey = firstNonEmpty(cfg.WorkspaceKey, repoRoot)
	cfg.ShortName = firstNonEmpty(cfg.ShortName, filepath.Base(repoRoot))
	cfg.Version = firstNonEmpty(cfg.Version, "test")
	cfg.BuildFingerprint = firstNonEmpty(cfg.BuildFingerprint, "fp-test")
	cfg.BinaryPath = firstNonEmpty(cfg.BinaryPath, "/test/codex-remote")
	cfg.DaemonBinaryPath = firstNonEmpty(cfg.DaemonBinaryPath, "/test/codex-remote")

	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, cfg.InstanceID)

	if err := server.SendCommand(cfg.InstanceID, agentproto.Command{
		CommandID: "cmd-prompt",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target: agentproto.Target{
			ThreadID: "thread-1",
			CWD:      testutil.WorkspacePath("data", "dl", "droid"),
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "列一下文件"}},
		},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	return server, eventsCh, ackCh, &stdout, &stderr, done
}

func wrapperTestRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))
}
