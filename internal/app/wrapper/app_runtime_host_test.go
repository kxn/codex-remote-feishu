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

	claudeadapter "github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestWrapperClaudeHelloAndShutdown(t *testing.T) {
	repoRoot := wrapperTestRepoRoot(t)
	t.Setenv("CLAUDE_BIN", installMockClaudeHelper(t, "hello"))

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
	if !hello.Capabilities.ThreadsRefresh || !hello.Capabilities.RequestRespond || !hello.Capabilities.SessionCatalog || !hello.Capabilities.ResumeByThreadID || !hello.Capabilities.RequiresCWDForResume {
		t.Fatalf("claude backend should advertise catalog/history capabilities: %#v", hello.Capabilities)
	}
	if hello.Capabilities.TurnSteer || hello.Capabilities.VSCodeMode {
		t.Fatalf("claude backend unexpectedly advertised unsupported capabilities: %#v", hello.Capabilities)
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

func TestWrapperClaudeThreadsRefreshUsesLocalSessionCatalog(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workspaceRoot := filepath.Join(t.TempDir(), "ws-refresh")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeWrapperClaudeSessionFile(t, configDir, workspaceRoot, "mock-claude-session-1", []string{
		`{"type":"system","cwd":"` + workspaceRoot + `","session_id":"mock-claude-session-1","model":"mimo-v2.5-pro"}`,
		`{"type":"session-title","title":"Refresh session"}`,
		`{"type":"user","message":{"role":"user","content":"refresh prompt"}}`,
	})

	server, eventsCh, ackCh, stdout, stderr, done := startWrapperClaudeRuntimeTestAppForWorkspace(t, "hello", workspaceRoot)

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-claude-refresh",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send threads.refresh: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-claude-refresh" && ack.Accepted
	}, stdout, stderr, done)

	var snapshotEvent *agentproto.Event
	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		for i := range events {
			if events[i].Kind == agentproto.EventThreadsSnapshot {
				snapshotEvent = &events[i]
				return true
			}
		}
		return false
	}, stdout, stderr, done)
	if snapshotEvent == nil || len(snapshotEvent.Threads) != 1 {
		t.Fatalf("expected one Claude session snapshot, got %#v", snapshotEvent)
	}
	thread := snapshotEvent.Threads[0]
	if thread.ThreadID != "mock-claude-session-1" || thread.Name != "Refresh session" || thread.Preview != "refresh prompt" || thread.CWD != workspaceRoot || thread.RuntimeStatus == nil {
		t.Fatalf("unexpected Claude refresh snapshot: %#v", thread)
	}
}

func TestWrapperClaudeThreadHistoryReadUsesLocalTranscriptHistory(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	workspaceRoot := filepath.Join(t.TempDir(), "ws-history")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeWrapperClaudeSessionFile(t, configDir, workspaceRoot, "mock-claude-session-1", []string{
		`{"type":"system","timestamp":"2026-04-28T11:00:00Z","cwd":"` + workspaceRoot + `","session_id":"mock-claude-session-1","model":"mimo-v2.5-pro"}`,
		`{"type":"user","timestamp":"2026-04-28T11:01:00Z","promptId":"prompt-1","message":{"role":"user","content":"first input"}}`,
		`{"type":"assistant","timestamp":"2026-04-28T11:01:05Z","promptId":"prompt-1","message":{"role":"assistant","content":[{"type":"text","text":"first answer"}]}}`,
		`{"type":"user","timestamp":"2026-04-28T11:02:00Z","promptId":"prompt-2","message":{"role":"user","content":"second input"}}`,
		`{"type":"assistant","timestamp":"2026-04-28T11:02:05Z","promptId":"prompt-2","message":{"role":"assistant","content":[{"type":"tool_use","id":"tool-1","name":"Bash","input":{"command":"printf hi"}}]}}`,
	})

	server, eventsCh, ackCh, stdout, stderr, done := startWrapperClaudeRuntimeTestAppForWorkspace(t, "hello", workspaceRoot)

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-claude-history",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: "mock-claude-session-1"},
	}); err != nil {
		t.Fatalf("send thread.history.read: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-claude-history" && ack.Accepted
	}, stdout, stderr, done)

	var historyEvent *agentproto.Event
	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		for i := range events {
			if events[i].Kind == agentproto.EventThreadHistoryRead {
				historyEvent = &events[i]
				return true
			}
		}
		return false
	}, stdout, stderr, done)
	if historyEvent == nil || historyEvent.CommandID != "cmd-claude-history" || historyEvent.ThreadHistory == nil {
		t.Fatalf("expected Claude history event, got %#v", historyEvent)
	}
	history := historyEvent.ThreadHistory
	if history.Thread.ThreadID != "mock-claude-session-1" || len(history.Turns) != 2 {
		t.Fatalf("unexpected history payload: %#v", history)
	}
	if history.Turns[0].TurnID != "prompt-1" || history.Turns[1].TurnID != "prompt-2" {
		t.Fatalf("unexpected grouped turn ids: %#v", history.Turns)
	}
	if len(history.Turns[0].Items) != 2 || history.Turns[0].Items[0].Kind != "user_message" || history.Turns[0].Items[1].Kind != "agent_message" {
		t.Fatalf("unexpected first history turn: %#v", history.Turns[0])
	}
	if len(history.Turns[1].Items) != 2 || history.Turns[1].Items[1].Kind != "command_execution" {
		t.Fatalf("unexpected second history turn: %#v", history.Turns[1])
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

func TestWrapperClaudePromptAndRequestRespondMainChain(t *testing.T) {
	server, eventsCh, ackCh, stdout, stderr, done := startWrapperClaudeRuntimeTestApp(t, "tool-approval")

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-prompt-claude",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target: agentproto.Target{
			ThreadID: "thread-1",
			CWD:      testutil.WorkspacePath("data", "dl", "droid"),
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "run the mock tool flow"}},
		},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-prompt-claude" && ack.Accepted
	}, stdout, stderr, done)

	var runtimeThreadID string
	var requestID string
	sawToolStarted := false
	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		for _, event := range events {
			if event.Kind == agentproto.EventTurnStarted && runtimeThreadID == "" {
				runtimeThreadID = event.ThreadID
			}
			if event.Kind == agentproto.EventItemStarted && event.ItemKind == "dynamic_tool_call" {
				sawToolStarted = true
			}
			if event.Kind == agentproto.EventRequestStarted && requestID == "" {
				requestID = event.RequestID
			}
		}
		return runtimeThreadID != "" && requestID != "" && sawToolStarted
	}, stdout, stderr, done)
	if strings.TrimSpace(runtimeThreadID) == "" {
		t.Fatal("expected claude turn.started event to expose runtime thread id")
	}
	if strings.TrimSpace(requestID) == "" {
		t.Fatal("expected claude request.started event to carry request id")
	}

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-request-respond-claude",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: requestID,
			Response: map[string]any{
				"type":     "approval",
				"decision": "accept",
			},
		},
	}); err != nil {
		t.Fatalf("send request respond: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-request-respond-claude" && ack.Accepted
	}, stdout, stderr, done)

	waitForObservedEvents(t, eventsCh, 10*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemCompleted && event.ItemKind == "dynamic_tool_call" && event.Status == "completed" && event.ThreadID == runtimeThreadID
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemCompleted && event.ItemKind == "agent_message" && event.ThreadID == runtimeThreadID
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted && event.Status == "completed" && event.ThreadID == runtimeThreadID
		},
	)
}

func TestWrapperClaudeInterruptMainChain(t *testing.T) {
	server, eventsCh, ackCh, stdout, stderr, done := startWrapperClaudeRuntimeTestApp(t, "interrupt")

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-prompt-claude-interrupt",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target: agentproto.Target{
			ThreadID: "thread-1",
			CWD:      testutil.WorkspacePath("data", "dl", "droid"),
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start a long running action"}},
		},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-prompt-claude-interrupt" && ack.Accepted
	}, stdout, stderr, done)

	var runtimeThreadID string
	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		for _, event := range events {
			if event.Kind == agentproto.EventTurnStarted {
				runtimeThreadID = event.ThreadID
				return true
			}
		}
		return false
	}, stdout, stderr, done)
	if strings.TrimSpace(runtimeThreadID) == "" {
		t.Fatal("expected claude turn.started event to expose runtime thread id")
	}

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-turn-interrupt-claude",
		Kind:      agentproto.CommandTurnInterrupt,
		Target:    agentproto.Target{ThreadID: runtimeThreadID},
	}); err != nil {
		t.Fatalf("send interrupt: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-turn-interrupt-claude" && ack.Accepted
	}, stdout, stderr, done)

	waitForObservedEvents(t, eventsCh, 10*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted && event.Status == "interrupted" && event.ThreadID == runtimeThreadID
		},
	)
}

func TestWrapperClaudeReconcilesFailedTurnWhenChildExitsWithoutResult(t *testing.T) {
	server, eventsCh, ackCh, stdout, stderr, done := startWrapperClaudeRuntimeTestApp(t, "exit-without-result")

	if err := server.SendCommand("inst-claude-runtime", agentproto.Command{
		CommandID: "cmd-prompt-claude-exit",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target: agentproto.Target{
			ThreadID: "thread-1",
			CWD:      testutil.WorkspacePath("data", "dl", "droid"),
		},
		Prompt: agentproto.Prompt{
			Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "start and then exit before sending result"}},
		},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-prompt-claude-exit" && ack.Accepted
	}, stdout, stderr, done)

	waitForObservedEvents(t, eventsCh, 10*time.Second, stdout, stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted
		},
		func(event agentproto.Event) bool {
			text, _ := event.Metadata["text"].(string)
			return event.Kind == agentproto.EventItemCompleted &&
				event.ItemKind == "agent_message" &&
				text == "Partial output before process exit."
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted &&
				event.Status == "failed" &&
				event.Problem != nil &&
				event.Problem.Code == "runtime_exit_before_turn_completed"
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

func startWrapperClaudeRuntimeTestApp(t *testing.T, scenario string) (*relayws.Server, <-chan []agentproto.Event, <-chan agentproto.CommandAck, *bytes.Buffer, *bytes.Buffer, <-chan error) {
	t.Helper()
	repoRoot := wrapperTestRepoRoot(t)
	return startWrapperClaudeRuntimeTestAppForWorkspace(t, scenario, repoRoot)
}

func startWrapperClaudeRuntimeTestAppForWorkspace(t *testing.T, scenario, workspaceRoot string) (*relayws.Server, <-chan []agentproto.Event, <-chan agentproto.CommandAck, *bytes.Buffer, *bytes.Buffer, <-chan error) {
	t.Helper()
	t.Setenv("CLAUDE_BIN", installMockClaudeHelper(t, scenario))

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
	app := New(Config{
		RelayServerURL:   "ws" + strings.TrimPrefix(httpServer.URL, "http"),
		InstanceID:       "inst-claude-runtime",
		DisplayName:      "claude-runtime",
		WorkspaceRoot:    workspaceRoot,
		WorkspaceKey:     workspaceRoot,
		ShortName:        filepath.Base(workspaceRoot),
		Backend:          agentproto.BackendClaude,
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-claude-runtime")
	return server, eventsCh, ackCh, &stdout, &stderr, done
}

func writeWrapperClaudeSessionFile(t *testing.T, configDir, workspaceRoot, sessionID string, lines []string) string {
	t.Helper()
	projectDir := filepath.Join(configDir, "projects", claudeadapter.SanitizeProjectDirName(workspaceRoot))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return filePath
}

func wrapperTestRepoRoot(t *testing.T) string {
	t.Helper()
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))
}
