//go:build integration

package claude_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/claude/clauderecord"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// TestClaudeCLIIntegration spawns a real Claude CLI process in SDK mode,
// sends a simple prompt, and verifies that the translator produces the
// expected agentproto event sequence. It also records the NDJSON transcript
// with privacy masking for use as a replay fixture.
//
// Run with: go test -tags integration -run TestClaudeCLIIntegration -v -timeout 120s
func TestClaudeCLIIntegration(t *testing.T) {
	cliBin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not found on PATH, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cwd, _ := os.Getwd()
	homePath, _ := os.UserHomeDir()

	// Build command
	args := []string{
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--include-partial-messages",
		"--setting-sources", "",
		"--input-format", "stream-json",
	}
	cmd := exec.CommandContext(ctx, cliBin, args...)
	cmd.Env = buildClaudeEnv(os.Environ())
	cmd.Dir = cwd

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr // let Claude's stderr show in test output

	recorder := clauderecord.NewRecorder()
	translator := claude.NewTranslator("integration-test")
	translator.SetDebugLogger(func(format string, args ...any) {
		t.Logf("translator: "+format, args...)
	})

	if err := cmd.Start(); err != nil {
		t.Fatalf("start claude: %v", err)
	}
	defer func() {
		stdin.Close()
		cmd.Wait()
	}()

	// Channels for collecting events from the reader goroutine
	eventsCh := make(chan agentproto.Event, 256)
	outboundCh := make(chan []byte, 64)
	readerDone := make(chan error, 1)

	// Read stdout in background using ReadBytes('\n') for reliable line detection
	go func() {
		reader := bufio.NewReaderSize(stdout, 4<<20) // 4MB buffer
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				recorder.RecordRecv(line)

				var peek struct{ Type string `json:"type"` }
				json.Unmarshal(line, &peek)
				t.Logf("recv: type=%s len=%d", peek.Type, len(line))

				result, obsErr := translator.ObserveServer(line)
				if obsErr != nil {
					t.Logf("observe error: %v (line: %s)", obsErr, truncate(line, 200))
					continue
				}
				for _, event := range result.Events {
					t.Logf("event: kind=%s", event.Kind)
					eventsCh <- event
				}
				for _, out := range result.OutboundToAgent {
					outboundCh <- out
				}
			}
			if err != nil {
				readerDone <- err
				return
			}
		}
	}()

	// Write outbound frames (MCP handshake responses) back to stdin
	go func() {
		for frame := range outboundCh {
			recorder.RecordSend(frame)
			stdin.Write(frame)
		}
	}()

	// Step 1: Send bootstrap (initialize handshake)
	bootstrapFrames, err := translator.BootstrapFrames("integration-test", "1.0")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, frame := range bootstrapFrames {
		recorder.RecordSend(frame)
		if _, err := stdin.Write(frame); err != nil {
			t.Fatalf("write bootstrap: %v", err)
		}
	}

	// Step 2: Wait briefly for init, then send user message.
	// Claude CLI may buffer the system init message until the first user message is sent.
	var allEvents []agentproto.Event
	drainWithTimeout(eventsCh, &allEvents, 5*time.Second)
	t.Logf("after init drain: %d events", len(allEvents))

	// Step 3: Send user message
	userMsg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": "respond with exactly the word hello and nothing else",
		},
	}
	msgBytes, _ := json.Marshal(userMsg)
	msgBytes = append(msgBytes, '\n')
	recorder.RecordSend(msgBytes)
	if _, err := stdin.Write(msgBytes); err != nil {
		t.Fatalf("write user message: %v", err)
	}

	// Step 4: Collect events until turn completion (includes system init + streaming)
	waitForEventKind(t, ctx, eventsCh, &allEvents, agentproto.EventTurnCompleted, "turn completion")

	// Step 5: Verify event sequence
	t.Logf("collected %d events", len(allEvents))
	for i, e := range allEvents {
		t.Logf("  event[%d]: kind=%s threadId=%s turnId=%s itemKind=%s delta=%s",
			i, e.Kind, e.ThreadID, e.TurnID, e.ItemKind, truncate([]byte(e.Delta), 80))
	}

	assertHasEventKind(t, allEvents, agentproto.EventThreadDiscovered)
	assertHasEventKind(t, allEvents, agentproto.EventTurnStarted)
	assertHasEventKind(t, allEvents, agentproto.EventItemStarted)
	assertHasEventKind(t, allEvents, agentproto.EventItemCompleted)

	turnCompleted := findEvent(allEvents, agentproto.EventTurnCompleted)
	if turnCompleted == nil {
		t.Fatal("missing EventTurnCompleted")
	}
	if turnCompleted.Status != "completed" {
		t.Errorf("turn status: got %q, want %q", turnCompleted.Status, "completed")
	}

	// Check that deltas contain "hello"
	var fullText string
	for _, e := range allEvents {
		if e.Kind == agentproto.EventItemDelta {
			fullText += e.Delta
		}
	}
	if !strings.Contains(strings.ToLower(fullText), "hello") {
		t.Errorf("expected delta text to contain 'hello', got %q", fullText)
	}

	// Step 6: Save masked recording
	close(outboundCh)
	stdin.Close()

	fixturePath := filepath.Join("testdata", "hello_simple.ndjson")
	if err := recorder.SaveMasked(fixturePath, clauderecord.MaskOptions{
		WorkspaceCWD: cwd,
		HomePath:     homePath,
	}); err != nil {
		t.Fatalf("save masked recording: %v", err)
	}
	t.Logf("saved masked recording to %s (%d entries)", fixturePath, len(recorder.Entries()))
}

// drainWithTimeout collects events from the channel for up to the given duration.
func drainWithTimeout(ch <-chan agentproto.Event, allEvents *[]agentproto.Event, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case event := <-ch:
			*allEvents = append(*allEvents, event)
		case <-timer.C:
			return
		}
	}
}

// waitForEventKind drains eventsCh into allEvents until the target kind appears.
func waitForEventKind(t *testing.T, ctx context.Context, ch <-chan agentproto.Event, allEvents *[]agentproto.Event, kind agentproto.EventKind, label string) {
	t.Helper()
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s (%s); collected %d events so far", kind, label, len(*allEvents))
		case event := <-ch:
			*allEvents = append(*allEvents, event)
			if event.Kind == kind {
				return
			}
		}
	}
}

func assertHasEventKind(t *testing.T, events []agentproto.Event, kind agentproto.EventKind) {
	t.Helper()
	for _, e := range events {
		if e.Kind == kind {
			return
		}
	}
	t.Errorf("expected event kind %s in sequence", kind)
}

func findEvent(events []agentproto.Event, kind agentproto.EventKind) *agentproto.Event {
	for i := range events {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}

func truncate(b []byte, max int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func buildClaudeEnv(parent []string) []string {
	filtered := make([]string, 0, len(parent)+1)
	for _, e := range parent {
		if strings.HasPrefix(e, "CLAUDECODE=") {
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered, "CLAUDE_CODE_ENTRYPOINT=sdk-go")
}
