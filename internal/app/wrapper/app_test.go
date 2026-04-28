package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestWrapperBridgesRelayAndCodexProcess(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ relayws.ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex"},
		InstanceID:       "inst-wrapper",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, stdinReader, &stdout, &stderr)
		done <- err
	}()

	hello := waitForHello(t, helloCh, "inst-wrapper")
	if hello.Instance.Backend != agentproto.BackendCodex {
		t.Fatalf("wrapper hello backend = %q, want %q", hello.Instance.Backend, agentproto.BackendCodex)
	}
	if !hello.Capabilities.ThreadsRefresh || !hello.Capabilities.TurnSteer || !hello.Capabilities.RequestRespond || !hello.Capabilities.ResumeByThreadID || !hello.Capabilities.VSCodeMode {
		t.Fatalf("wrapper hello capabilities missing codex defaults: %#v", hello.Capabilities)
	}
	if hello.Capabilities.SessionCatalog || hello.Capabilities.RequiresCWDForResume {
		t.Fatalf("wrapper hello capabilities unexpectedly enabled non-codex defaults: %#v", hello.Capabilities)
	}

	if err := server.SendCommand("inst-wrapper", agentproto.Command{
		CommandID: "cmd-refresh",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send refresh: %v", err)
	}

	waitForEvent(t, eventsCh, 15*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadsSnapshot && len(event.Threads) == 1
		})
	}, &stdout, &stderr, done)

	if err := server.SendCommand("inst-wrapper", agentproto.Command{
		CommandID: "cmd-prompt",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target:    agentproto.Target{ThreadID: "thread-1", CWD: testutil.WorkspacePath("data", "dl", "droid")},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "列一下文件"}}},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	waitForObservedEvents(t, eventsCh, 15*time.Second, &stdout, &stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemDelta && event.ItemKind == "agent_message"
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventItemCompleted
		},
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnCompleted
		},
	)

	if strings.Contains(stdout.String(), "relay-turn-start") {
		t.Fatalf("internal relay request leaked to parent stdout: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"method\":\"turn/started\"") {
		t.Fatalf("expected notifications to reach parent stdout, got %s", stdout.String())
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperHeadlessBootstrapsInitializeBeforeRelayCommands(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ relayws.ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex", "--require-initialize"},
		InstanceID:       "inst-headless",
		DisplayName:      "headless",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Source:           "headless",
		Managed:          true,
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-headless")

	if err := server.SendCommand("inst-headless", agentproto.Command{
		CommandID: "cmd-refresh",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send refresh: %v", err)
	}

	waitForEvent(t, eventsCh, 15*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadsSnapshot && len(event.Threads) == 1
		})
	}, &stdout, &stderr, done)

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperRejectsSteerWhenCodexRejectsExpectedTurn(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 8)
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
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex", "--no-auto-complete"},
		InstanceID:       "inst-wrapper",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-wrapper")

	if err := server.SendCommand("inst-wrapper", agentproto.Command{
		CommandID: "cmd-prompt",
		Kind:      agentproto.CommandPromptSend,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target:    agentproto.Target{ThreadID: "thread-1", CWD: "/data/dl/droid"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "列一下文件"}}},
	}); err != nil {
		t.Fatalf("send prompt: %v", err)
	}

	waitForObservedEvents(t, eventsCh, 15*time.Second, &stdout, &stderr, done,
		func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventTurnStarted && event.TurnID == "turn-1"
		},
	)
	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-prompt" && ack.Accepted
	}, &stdout, &stderr, done)

	if err := server.SendCommand("inst-wrapper", agentproto.Command{
		CommandID: "cmd-steer",
		Kind:      agentproto.CommandTurnSteer,
		Origin:    agentproto.Origin{Surface: "feishu:app-1:chat:test"},
		Target:    agentproto.Target{ThreadID: "thread-1", TurnID: "turn-wrong"},
		Prompt:    agentproto.Prompt{Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "补充一下"}}},
	}); err != nil {
		t.Fatalf("send steer: %v", err)
	}

	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-steer" && !ack.Accepted && ack.Problem != nil &&
			strings.Contains(ack.Problem.Details, "expected active turn id")
	}, &stdout, &stderr, done)

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperKeepsEphemeralHelperTrafficOnStdoutAndAnnotatesRelay(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ relayws.ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex"},
		InstanceID:       "inst-wrapper",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, stdinReader, &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-wrapper")

	line := mustJSONLine(t, map[string]any{
		"id":     "helper-thread-1",
		"method": "thread/start",
		"params": map[string]any{
			"cwd":                    repoRoot,
			"approvalPolicy":         "never",
			"sandbox":                "read-only",
			"ephemeral":              true,
			"persistExtendedHistory": false,
		},
	})
	if _, err := io.WriteString(stdinWriter, line); err != nil {
		t.Fatalf("write helper thread start: %v", err)
	}

	waitForStdout(t, 10*time.Second, &stdout, &stderr, done, func(out string) bool {
		return strings.Contains(out, `"id":"helper-thread-1"`) && strings.Contains(out, `"method":"thread/started"`)
	})

	waitForEvent(t, eventsCh, 5*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadDiscovered &&
				event.TrafficClass == agentproto.TrafficClassInternalHelper &&
				event.Initiator.Kind == agentproto.InitiatorInternalHelper
		})
	}, &stdout, &stderr, done)

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperWritesRawFramesWhenEnabled(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rawPath := filepath.Join(t.TempDir(), "wrapper-raw.ndjson")

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex"},
		InstanceID:       "inst-wrapper-raw",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
		DebugRelayRaw:    true,
		RawLogPath:       rawPath,
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, stdinReader, &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-wrapper-raw")

	line := mustJSONLine(t, map[string]any{
		"id":     "helper-thread-1",
		"method": "thread/start",
		"params": map[string]any{
			"cwd":                    repoRoot,
			"approvalPolicy":         "never",
			"sandbox":                "read-only",
			"ephemeral":              true,
			"persistExtendedHistory": false,
		},
	})
	if _, err := io.WriteString(stdinWriter, line); err != nil {
		t.Fatalf("write helper thread start: %v", err)
	}

	waitForStdout(t, 10*time.Second, &stdout, &stderr, done, func(out string) bool {
		return strings.Contains(out, `"id":"helper-thread-1"`) && strings.Contains(out, `"method":"thread/started"`)
	})

	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"channel":"parent.stdin"`) {
		t.Fatalf("expected parent.stdin raw frame, got %s", text)
	}
	if !strings.Contains(text, `"channel":"codex.stdin"`) {
		t.Fatalf("expected codex.stdin raw frame, got %s", text)
	}
	if !strings.Contains(text, `"channel":"codex.stdout"`) {
		t.Fatalf("expected codex.stdout raw frame, got %s", text)
	}
	if !strings.Contains(text, `"channel":"relay.ws"`) {
		t.Fatalf("expected relay.ws raw frame, got %s", text)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperExitsWhenDaemonRequestsProcessExit(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex", "--no-auto-complete"},
		InstanceID:       "inst-wrapper-exit",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, strings.NewReader(""), &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-wrapper-exit")

	if err := server.SendCommand("inst-wrapper-exit", agentproto.Command{
		CommandID: "cmd-exit",
		Kind:      agentproto.CommandProcessExit,
	}); err != nil {
		t.Fatalf("send process exit: %v", err)
	}

	waitForAck(t, ackCh, 5*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-exit" && ack.Accepted
	}, &stdout, &stderr, done)

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperRestartsChildWithoutExitingAndRestoresFocusedThread(t *testing.T) {
	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	repoRoot = filepath.Clean(filepath.Join(repoRoot, "..", "..", ".."))

	helloCh := make(chan agentproto.Hello, 1)
	ackCh := make(chan agentproto.CommandAck, 8)
	eventsCh := make(chan []agentproto.Event, 8)
	server := relayws.NewServer(relayws.ServerCallbacks{
		OnHello: func(_ context.Context, _ relayws.ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnCommandAck: func(_ context.Context, _ relayws.ConnectionMeta, _ string, ack agentproto.CommandAck) {
			ackCh <- ack
		},
		OnEvents: func(_ context.Context, _ relayws.ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
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
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	rawPath := filepath.Join(t.TempDir(), "wrapper-restart.raw.ndjson")

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex", "--require-initialize"},
		InstanceID:       "inst-wrapper-restart",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Source:           "headless",
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: "/test/codex-remote",
		DebugRelayRaw:    true,
		RawLogPath:       rawPath,
	}
	app := New(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := app.Run(ctx, stdinReader, &stdout, &stderr)
		done <- err
	}()

	waitForHello(t, helloCh, "inst-wrapper-restart")

	if _, err := io.WriteString(stdinWriter, mustJSONLine(t, map[string]any{
		"id":     "local-resume-1",
		"method": "thread/resume",
		"params": map[string]any{
			"threadId": "thread-1",
			"cwd":      testutil.WorkspacePath("data", "dl", "droid"),
		},
	})); err != nil {
		t.Fatalf("write local thread/resume: %v", err)
	}
	waitForEvent(t, eventsCh, 10*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadFocused && event.ThreadID == "thread-1"
		})
	}, &stdout, &stderr, done)
	waitForStdout(t, 10*time.Second, &stdout, &stderr, done, func(out string) bool {
		return strings.Contains(out, `"id":"local-resume-1"`) && strings.Contains(out, `"method":"thread/started"`)
	})

	if err := server.SendCommand("inst-wrapper-restart", agentproto.Command{
		CommandID: "cmd-child-restart",
		Kind:      agentproto.CommandProcessChildRestart,
	}); err != nil {
		t.Fatalf("send child restart: %v", err)
	}
	waitForAck(t, ackCh, 10*time.Second, func(ack agentproto.CommandAck) bool {
		return ack.CommandID == "cmd-child-restart" && ack.Accepted
	}, &stdout, &stderr, done)

	if err := server.SendCommand("inst-wrapper-restart", agentproto.Command{
		CommandID: "cmd-refresh-after-restart",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send refresh after restart: %v", err)
	}
	waitForEvent(t, eventsCh, 15*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadsSnapshot && len(event.Threads) == 1
		})
	}, &stdout, &stderr, done)

	if count := countRawFramesByMethod(t, rawPath, "codex.stdin", "initialize"); count != 2 {
		t.Fatalf("expected two initialize frames after child restart, got %d", count)
	}
	if count := countRawFramesByMethod(t, rawPath, "codex.stdin", "thread/resume"); count != 2 {
		t.Fatalf("expected original + restore thread/resume frames on codex.stdin, got %d", count)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("wrapper run failed: %v\nstderr:\n%s", err, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for wrapper shutdown")
	}
}

func TestWrapperFailsBeforeStartingChildWhenRelayBootstrapFails(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "mockcodex.pid")
	script := filepath.Join(tempDir, "mockcodex-sleep.sh")
	if err := os.WriteFile(script, []byte("#!/usr/bin/env bash\nset -euo pipefail\necho $$ > "+pidFile+"\ntrap 'exit 0' TERM INT\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatalf("write mock codex script: %v", err)
	}

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cfg := Config{
		RelayServerURL:   "ws://127.0.0.1:1/ws/agent",
		CodexRealBinary:  script,
		InstanceID:       "inst-wrapper",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    tempDir,
		WorkspaceKey:     tempDir,
		ShortName:        filepath.Base(tempDir),
		Version:          "test",
		BuildFingerprint: "fp-test",
		BinaryPath:       "/test/codex-remote",
		DaemonBinaryPath: filepath.Join(tempDir, "missing-codex-remote"),
	}
	app := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := app.Run(ctx, stdinReader, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected wrapper run to fail when relay server is unavailable")
	}

	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("expected child codex not to start, stat err=%v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func waitForEvent(t *testing.T, eventsCh <-chan []agentproto.Event, timeout time.Duration, match func([]agentproto.Event) bool, stdout, stderr *bytes.Buffer, done <-chan error) {
	t.Helper()
	deadline := time.After(timeout)
	var exitErr error
	for {
		select {
		case events := <-eventsCh:
			if match(events) {
				return
			}
		case err := <-done:
			exitErr = err
			done = nil
		case <-deadline:
			if done == nil {
				t.Fatalf("timed out waiting for matching event after wrapper exit: %v\nstdout:\n%s\nstderr:\n%s", exitErr, stdout.String(), stderr.String())
			}
			t.Fatalf("timed out waiting for matching event\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	}
}

func batchHasEvent(events []agentproto.Event, match func(agentproto.Event) bool) bool {
	for _, event := range events {
		if match(event) {
			return true
		}
	}
	return false
}

func waitForObservedEvents(t *testing.T, eventsCh <-chan []agentproto.Event, timeout time.Duration, stdout, stderr *bytes.Buffer, done <-chan error, matchers ...func(agentproto.Event) bool) {
	t.Helper()
	if len(matchers) == 0 {
		return
	}
	seen := make([]bool, len(matchers))
	allSeen := func() bool {
		for _, current := range seen {
			if !current {
				return false
			}
		}
		return true
	}
	deadline := time.After(timeout)
	var exitErr error
	for {
		if allSeen() {
			return
		}
		select {
		case events := <-eventsCh:
			for _, event := range events {
				for i, match := range matchers {
					if !seen[i] && match(event) {
						seen[i] = true
					}
				}
			}
		case err := <-done:
			exitErr = err
			done = nil
		case <-deadline:
			if done == nil {
				t.Fatalf("timed out waiting for observed events after wrapper exit: %v\nstdout:\n%s\nstderr:\n%s", exitErr, stdout.String(), stderr.String())
			}
			t.Fatalf("timed out waiting for observed events\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	}
}

func waitForAck(t *testing.T, ackCh <-chan agentproto.CommandAck, timeout time.Duration, match func(agentproto.CommandAck) bool, stdout, stderr *bytes.Buffer, done <-chan error) {
	t.Helper()
	deadline := time.After(timeout)
	var exitErr error
	for {
		select {
		case ack := <-ackCh:
			if match(ack) {
				return
			}
		case err := <-done:
			exitErr = err
			done = nil
		case <-deadline:
			if done == nil {
				t.Fatalf("timed out waiting for matching ack after wrapper exit: %v\nstdout:\n%s\nstderr:\n%s", exitErr, stdout.String(), stderr.String())
			}
			t.Fatalf("timed out waiting for matching ack\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	}
}

func waitForStdout(t *testing.T, timeout time.Duration, stdout, stderr *bytes.Buffer, done <-chan error, match func(string) bool) {
	t.Helper()
	deadline := time.After(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("wrapper exited early: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		case <-ticker.C:
			if match(stdout.String()) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for matching stdout\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		}
	}
}

func countRawFramesByMethod(t *testing.T, rawPath, channel, method string) int {
	t.Helper()
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", rawPath, err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record debuglog.RawRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("unmarshal raw record: %v", err)
		}
		if record.Channel != channel || len(record.Frame) == 0 {
			continue
		}
		var frame map[string]any
		if err := json.Unmarshal(record.Frame, &frame); err != nil {
			t.Fatalf("unmarshal raw frame: %v", err)
		}
		if frame["method"] == method {
			count++
		}
	}
	return count
}

func mustJSONLine(t *testing.T, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal json line: %v", err)
	}
	return string(raw) + "\n"
}

func waitForHello(t *testing.T, helloCh <-chan agentproto.Hello, wantInstanceID string) agentproto.Hello {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case hello := <-helloCh:
			if hello.Instance.InstanceID == wantInstanceID {
				return hello
			}
		case <-deadline:
			t.Fatalf("timed out waiting for wrapper hello %q", wantInstanceID)
		}
	}
}
