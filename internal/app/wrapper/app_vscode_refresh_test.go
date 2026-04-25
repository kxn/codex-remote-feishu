package wrapper

import (
	"bytes"
	"context"
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
)

func TestWrapperStartupRefreshBorrowsVSCodeThreadList(t *testing.T) {
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
	rawPath := filepath.Join(t.TempDir(), "wrapper-raw.ndjson")

	cfg := Config{
		RelayServerURL:   wsURL,
		CodexRealBinary:  "go",
		Args:             []string{"run", "./testkit/mockcodex/cmd/mockcodex"},
		InstanceID:       "inst-wrapper-vscode-refresh",
		DisplayName:      "codex-remote",
		WorkspaceRoot:    repoRoot,
		WorkspaceKey:     repoRoot,
		ShortName:        filepath.Base(repoRoot),
		Source:           "vscode",
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

	waitForHello(t, helloCh, "inst-wrapper-vscode-refresh")

	if err := server.SendCommand("inst-wrapper-vscode-refresh", agentproto.Command{
		CommandID: "cmd-refresh",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send refresh: %v", err)
	}

	if _, err := io.WriteString(stdinWriter, mustJSONLine(t, map[string]any{
		"id":     "codex.chatSessionProvider:0",
		"method": "thread/list",
		"params": map[string]any{
			"limit":          50,
			"cursor":         nil,
			"sortKey":        "created_at",
			"modelProviders": []any{},
			"archived":       false,
			"sourceKinds":    []any{},
		},
	})); err != nil {
		t.Fatalf("write vscode thread/list: %v", err)
	}

	waitForEvent(t, eventsCh, 15*time.Second, func(events []agentproto.Event) bool {
		return batchHasEvent(events, func(event agentproto.Event) bool {
			return event.Kind == agentproto.EventThreadsSnapshot && len(event.Threads) == 1
		})
	}, &stdout, &stderr, done)

	waitForStdout(t, 10*time.Second, &stdout, &stderr, done, func(out string) bool {
		return strings.Contains(out, `"id":"codex.chatSessionProvider:0"`) &&
			strings.Contains(out, `"result":{"data":[`)
	})

	if count := countRawFramesByMethod(t, rawPath, "codex.stdin", "thread/list"); count != 1 {
		t.Fatalf("expected only the vscode client thread/list to reach codex.stdin, got %d", count)
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
