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

// claudeSession wraps a live Claude CLI subprocess for integration testing.
type claudeSession struct {
	t          *testing.T
	cmd        *exec.Cmd
	stdin      interface{ Write([]byte) (int, error); Close() error }
	translator *claude.Translator
	recorder   *clauderecord.Recorder
	eventsCh   chan agentproto.Event
	outboundCh chan []byte
	cwd        string
	homePath   string
}

func startClaude(t *testing.T, ctx context.Context) *claudeSession {
	t.Helper()
	cliBin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not found on PATH, skipping integration test")
	}

	cwd, _ := os.Getwd()
	homePath, _ := os.UserHomeDir()

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
	cmd.Stderr = os.Stderr

	recorder := clauderecord.NewRecorder()
	translator := claude.NewTranslator("integration-test")
	translator.SetDebugLogger(func(format string, args ...any) {
		t.Logf("translator: "+format, args...)
	})

	if err := cmd.Start(); err != nil {
		t.Fatalf("start claude: %v", err)
	}

	eventsCh := make(chan agentproto.Event, 512)
	outboundCh := make(chan []byte, 64)

	// Read stdout in background
	go func() {
		reader := bufio.NewReaderSize(stdout, 4<<20)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				recorder.RecordRecv(line)
				var peek struct{ Type string `json:"type"` }
				json.Unmarshal(line, &peek)
				t.Logf("recv: type=%s len=%d", peek.Type, len(line))

				result, obsErr := translator.ObserveServer(line)
				if obsErr != nil {
					t.Logf("observe error: %v", obsErr)
					continue
				}
				for _, event := range result.Events {
					eventsCh <- event
				}
				for _, out := range result.OutboundToAgent {
					outboundCh <- out
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Write outbound frames back to stdin
	go func() {
		for frame := range outboundCh {
			recorder.RecordSend(frame)
			stdin.Write(frame)
		}
	}()

	// Send bootstrap
	bootstrapFrames, err := translator.BootstrapFrames("integration-test", "1.0")
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, frame := range bootstrapFrames {
		recorder.RecordSend(frame)
		stdin.Write(frame)
	}

	// Wait briefly for init handshake
	drainWithTimeout(eventsCh, &[]agentproto.Event{}, 5*time.Second)

	return &claudeSession{
		t: t, cmd: cmd, stdin: stdin,
		translator: translator, recorder: recorder,
		eventsCh: eventsCh, outboundCh: outboundCh,
		cwd: cwd, homePath: homePath,
	}
}

func (s *claudeSession) sendUserMessage(text string) {
	msg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	}
	b, _ := json.Marshal(msg)
	b = append(b, '\n')
	s.recorder.RecordSend(b)
	s.stdin.Write(b)
}

func (s *claudeSession) collectUntilTurnComplete(ctx context.Context) []agentproto.Event {
	var events []agentproto.Event
	waitForEventKind(s.t, ctx, s.eventsCh, &events, agentproto.EventTurnCompleted, "turn completion")
	return events
}

func (s *claudeSession) saveMaskedFixture(name string) {
	path := filepath.Join("testdata", name)
	if err := s.recorder.SaveMasked(path, clauderecord.MaskOptions{
		WorkspaceCWD: s.cwd,
		HomePath:     s.homePath,
	}); err != nil {
		s.t.Fatalf("save fixture: %v", err)
	}
	s.t.Logf("saved fixture to %s (%d entries)", path, len(s.recorder.Entries()))
}

func (s *claudeSession) close() {
	close(s.outboundCh)
	s.stdin.Close()
	s.cmd.Wait()
}

// --- Test: Simple hello ---

func TestClaudeCLIIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s := startClaude(t, ctx)
	defer s.close()

	s.sendUserMessage("respond with exactly the word hello and nothing else")
	events := s.collectUntilTurnComplete(ctx)

	logEvents(t, events)

	assertHasEventKind(t, events, agentproto.EventThreadDiscovered)
	assertHasEventKind(t, events, agentproto.EventTurnStarted)
	assertHasEventKind(t, events, agentproto.EventItemStarted)
	assertHasEventKind(t, events, agentproto.EventItemCompleted)

	tc := findEvent(events, agentproto.EventTurnCompleted)
	if tc == nil || tc.Status != "completed" {
		t.Fatalf("turn not completed successfully: %+v", tc)
	}

	assertDeltaContains(t, events, "hello")

	s.saveMaskedFixture("hello_simple.ndjson")
}

// --- Test: Tool use (Bash) ---

func TestClaudeCLIToolUse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s := startClaude(t, ctx)
	defer s.close()

	// Ask Claude to run a simple command -- it will use the Bash tool
	s.sendUserMessage("run the command 'echo test123' and tell me the output. Use the Bash tool.")
	events := s.collectUntilTurnComplete(ctx)

	logEvents(t, events)

	assertHasEventKind(t, events, agentproto.EventTurnStarted)

	// Should have tool_use content blocks (command_execution items)
	hasToolStart := false
	hasToolDelta := false
	for _, e := range events {
		if e.Kind == agentproto.EventItemStarted && e.ItemKind == "command_execution" {
			hasToolStart = true
			if meta, ok := e.Metadata["toolName"].(string); ok {
				t.Logf("tool started: %s", meta)
			}
		}
		if e.Kind == agentproto.EventItemDelta && e.ItemKind == "command_execution" {
			hasToolDelta = true
		}
	}
	if hasToolStart {
		t.Logf("tool use detected: start=%v deltas=%v", hasToolStart, hasToolDelta)
	}

	tc := findEvent(events, agentproto.EventTurnCompleted)
	if tc == nil || tc.Status != "completed" {
		t.Fatalf("turn not completed: %+v", tc)
	}

	// Check that response text mentions the command output
	assertDeltaContains(t, events, "test123")

	s.saveMaskedFixture("tool_use_bash.ndjson")
}

// --- Test: Thinking blocks ---

func TestClaudeCLIThinking(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s := startClaude(t, ctx)
	defer s.close()

	// Ask something that should trigger thinking
	s.sendUserMessage("what is 17 * 23? think through it step by step, then give the answer")
	events := s.collectUntilTurnComplete(ctx)

	logEvents(t, events)

	// Check for thinking content blocks
	hasThinking := false
	for _, e := range events {
		if e.Kind == agentproto.EventItemStarted && e.ItemKind == "reasoning_content" {
			hasThinking = true
		}
		if e.Kind == agentproto.EventItemDelta && e.ItemKind == "reasoning_content" {
			t.Logf("thinking delta: %s", truncate([]byte(e.Delta), 80))
		}
	}
	if hasThinking {
		t.Log("thinking blocks detected")
	}

	tc := findEvent(events, agentproto.EventTurnCompleted)
	if tc == nil || tc.Status != "completed" {
		t.Fatalf("turn not completed: %+v", tc)
	}

	// Should contain the answer (391)
	assertDeltaContains(t, events, "391")

	s.saveMaskedFixture("thinking.ndjson")
}

// --- Test: Multi-turn conversation ---

func TestClaudeCLIMultiTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	s := startClaude(t, ctx)
	defer s.close()

	// Turn 1
	s.sendUserMessage("remember the word 'banana'. respond with just 'ok'")
	events1 := s.collectUntilTurnComplete(ctx)
	logEvents(t, events1)

	tc1 := findEvent(events1, agentproto.EventTurnCompleted)
	if tc1 == nil || tc1.Status != "completed" {
		t.Fatalf("turn 1 not completed: %+v", tc1)
	}
	turn1ID := tc1.TurnID

	// Turn 2
	s.sendUserMessage("what word did I ask you to remember? respond with just that word")
	events2 := s.collectUntilTurnComplete(ctx)
	logEvents(t, events2)

	tc2 := findEvent(events2, agentproto.EventTurnCompleted)
	if tc2 == nil || tc2.Status != "completed" {
		t.Fatalf("turn 2 not completed: %+v", tc2)
	}
	turn2ID := tc2.TurnID

	// Turns should have different IDs
	if turn1ID == turn2ID {
		t.Errorf("turns should have different IDs: %s vs %s", turn1ID, turn2ID)
	}

	// Turn 2 should mention banana
	assertDeltaContains(t, events2, "banana")

	s.saveMaskedFixture("multi_turn.ndjson")
}

// --- Helpers ---

func logEvents(t *testing.T, events []agentproto.Event) {
	t.Helper()
	t.Logf("collected %d events", len(events))
	for i, e := range events {
		extra := ""
		if e.Delta != "" {
			extra = " delta=" + truncate([]byte(e.Delta), 60)
		}
		if e.ItemKind != "" {
			extra += " itemKind=" + e.ItemKind
		}
		if e.Status != "" {
			extra += " status=" + e.Status
		}
		t.Logf("  event[%d]: kind=%s%s", i, e.Kind, extra)
	}
}

func assertDeltaContains(t *testing.T, events []agentproto.Event, substr string) {
	t.Helper()
	var fullText string
	for _, e := range events {
		if e.Kind == agentproto.EventItemDelta {
			fullText += e.Delta
		}
	}
	if !strings.Contains(strings.ToLower(fullText), strings.ToLower(substr)) {
		t.Errorf("expected delta text to contain %q, got %q", substr, truncate([]byte(fullText), 200))
	}
}

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
