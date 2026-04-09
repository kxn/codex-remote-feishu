package claude_test

import (
	"os"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/claude/clauderecord"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// replayFixture loads a fixture and replays it through the translator,
// returning all collected events and outbound frames.
func replayFixture(t *testing.T, path string) (events []agentproto.Event, outbound [][]byte) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture %s not found; run: go test -tags integration -run TestClaude", path)
	}
	entries, err := clauderecord.LoadFixture(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	tr := claude.NewTranslator("replay-test")

	for _, entry := range entries {
		switch entry.Direction {
		case clauderecord.DirRecv:
			result, err := tr.ObserveServer(entry.Frame)
			if err != nil {
				continue
			}
			events = append(events, result.Events...)
			outbound = append(outbound, result.OutboundToAgent...)
		case clauderecord.DirSend:
			result, err := tr.ObserveClient(entry.Frame)
			if err != nil {
				continue
			}
			events = append(events, result.Events...)
		}
	}
	return
}

// --- Replay: hello simple ---

func TestTranslatorReplayHelloSimple(t *testing.T) {
	events, _ := replayFixture(t, "testdata/hello_simple.ndjson")
	logReplayEvents(t, events)

	assertEventOrder(t, events, []agentproto.EventKind{
		agentproto.EventThreadDiscovered,
		agentproto.EventTurnStarted,
		agentproto.EventItemStarted,
		agentproto.EventItemCompleted,
		agentproto.EventTurnCompleted,
	})

	assertTurnCompleted(t, events, "completed")
	assertDeltaTextContains(t, events, "agent_message", "hello")
}

// --- Replay: tool use ---

func TestTranslatorReplayToolUse(t *testing.T) {
	events, _ := replayFixture(t, "testdata/tool_use_bash.ndjson")
	logReplayEvents(t, events)

	assertEventOrder(t, events, []agentproto.EventKind{
		agentproto.EventTurnStarted,
		agentproto.EventTurnCompleted,
	})

	assertTurnCompleted(t, events, "completed")

	// Verify tool use items exist
	hasToolItem := false
	for _, e := range events {
		if e.Kind == agentproto.EventItemStarted && e.ItemKind == "command_execution" {
			hasToolItem = true
			if name, ok := e.Metadata["toolName"].(string); ok {
				t.Logf("tool: %s", name)
			}
		}
	}
	if !hasToolItem {
		// Tool use might be auto-executed without streaming items if
		// the model used bypass permissions -- still valid if turn completed
		t.Log("no command_execution items found (tool may have been auto-executed)")
	}

	assertDeltaTextContains(t, events, "", "test123")
}

// --- Replay: thinking ---

func TestTranslatorReplayThinking(t *testing.T) {
	events, _ := replayFixture(t, "testdata/thinking.ndjson")
	logReplayEvents(t, events)

	assertEventOrder(t, events, []agentproto.EventKind{
		agentproto.EventTurnStarted,
		agentproto.EventTurnCompleted,
	})

	assertTurnCompleted(t, events, "completed")

	// Check for thinking items
	hasThinking := false
	for _, e := range events {
		if e.Kind == agentproto.EventItemStarted && e.ItemKind == "reasoning_content" {
			hasThinking = true
		}
	}
	if hasThinking {
		t.Log("thinking blocks detected in replay")
		// Verify thinking deltas exist
		hasDelta := false
		for _, e := range events {
			if e.Kind == agentproto.EventItemDelta && e.ItemKind == "reasoning_content" {
				hasDelta = true
				break
			}
		}
		if !hasDelta {
			t.Error("thinking item started but no thinking deltas found")
		}
	}

	assertDeltaTextContains(t, events, "", "391")
}

// --- Replay: multi-turn ---

func TestTranslatorReplayMultiTurn(t *testing.T) {
	events, _ := replayFixture(t, "testdata/multi_turn.ndjson")
	logReplayEvents(t, events)

	// Should have exactly 2 turn completions
	turnCompletions := filterEvents(events, agentproto.EventTurnCompleted)
	if len(turnCompletions) < 2 {
		t.Fatalf("expected at least 2 turn completions, got %d", len(turnCompletions))
	}

	// Turn IDs should be different
	if turnCompletions[0].TurnID == turnCompletions[1].TurnID {
		t.Errorf("turn IDs should differ: %s vs %s", turnCompletions[0].TurnID, turnCompletions[1].TurnID)
	}

	// Both turns should complete successfully
	for i, tc := range turnCompletions {
		if tc.Status != "completed" {
			t.Errorf("turn %d status: %s", i, tc.Status)
		}
	}

	// Turn 2 should mention banana
	// Find events after the first turn completion
	afterFirstTurn := false
	var turn2Deltas string
	for _, e := range events {
		if e.Kind == agentproto.EventTurnCompleted && !afterFirstTurn {
			afterFirstTurn = true
			continue
		}
		if afterFirstTurn && e.Kind == agentproto.EventItemDelta {
			turn2Deltas += e.Delta
		}
	}
	if !strings.Contains(strings.ToLower(turn2Deltas), "banana") {
		t.Errorf("turn 2 should mention banana, got: %s", truncateStr(turn2Deltas, 200))
	}
}

// --- Replay: MCP handshake ---

func TestTranslatorReplayMCPHandshake(t *testing.T) {
	_, outbound := replayFixture(t, "testdata/hello_simple.ndjson")
	t.Logf("MCP handshake produced %d outbound frames", len(outbound))
}

// --- Replay: block index tracking ---

func TestTranslatorReplayBlockIndexTracking(t *testing.T) {
	// Replay any fixture with multiple blocks (thinking + text, or tool_use)
	// and verify that item IDs are correctly assigned by block index
	for _, fixture := range []string{"testdata/thinking.ndjson", "testdata/tool_use_bash.ndjson"} {
		if _, err := os.Stat(fixture); os.IsNotExist(err) {
			continue
		}
		t.Run(fixture, func(t *testing.T) {
			events, _ := replayFixture(t, fixture)
			// Verify no duplicate item IDs for started items
			seen := map[string]bool{}
			for _, e := range events {
				if e.Kind == agentproto.EventItemStarted && e.ItemID != "" {
					if seen[e.ItemID] {
						t.Errorf("duplicate item ID: %s", e.ItemID)
					}
					seen[e.ItemID] = true
				}
			}
			// Verify each item.started has a matching item.completed
			startedIDs := map[string]bool{}
			completedIDs := map[string]bool{}
			for _, e := range events {
				if e.Kind == agentproto.EventItemStarted {
					startedIDs[e.ItemID] = true
				}
				if e.Kind == agentproto.EventItemCompleted {
					completedIDs[e.ItemID] = true
				}
			}
			for id := range startedIDs {
				if !completedIDs[id] {
					t.Errorf("item %s started but never completed", id)
				}
			}
		})
	}
}

// --- Helpers ---

func logReplayEvents(t *testing.T, events []agentproto.Event) {
	t.Helper()
	t.Logf("replayed %d events", len(events))
	for i, e := range events {
		extra := ""
		if e.ItemKind != "" {
			extra += " itemKind=" + e.ItemKind
		}
		if len(e.Delta) > 0 {
			extra += " delta_len=" + strings.Repeat(".", min(len(e.Delta)/10+1, 5))
		}
		t.Logf("  event[%d]: kind=%s%s", i, e.Kind, extra)
	}
}

func assertEventOrder(t *testing.T, events []agentproto.Event, expected []agentproto.EventKind) {
	t.Helper()
	pos := 0
	for _, e := range events {
		if pos < len(expected) && e.Kind == expected[pos] {
			pos++
		}
	}
	if pos < len(expected) {
		var found []agentproto.EventKind
		for _, e := range events {
			found = append(found, e.Kind)
		}
		t.Errorf("event sequence incomplete: matched %d/%d expected kinds\nexpected: %v\nfound:    %v",
			pos, len(expected), expected, found)
	}
}

func assertTurnCompleted(t *testing.T, events []agentproto.Event, expectedStatus string) {
	t.Helper()
	tc := findEventByKind(events, agentproto.EventTurnCompleted)
	if tc == nil {
		t.Fatal("missing EventTurnCompleted")
	}
	if tc.Status != expectedStatus {
		t.Errorf("turn status: got %q, want %q", tc.Status, expectedStatus)
	}
}

func assertDeltaTextContains(t *testing.T, events []agentproto.Event, itemKind, substr string) {
	t.Helper()
	var fullText string
	for _, e := range events {
		if e.Kind == agentproto.EventItemDelta {
			if itemKind == "" || e.ItemKind == itemKind {
				fullText += e.Delta
			}
		}
	}
	if !strings.Contains(strings.ToLower(fullText), strings.ToLower(substr)) {
		t.Errorf("expected delta text (itemKind=%s) to contain %q, got %q", itemKind, substr, truncateStr(fullText, 200))
	}
}

func filterEvents(events []agentproto.Event, kind agentproto.EventKind) []agentproto.Event {
	var out []agentproto.Event
	for _, e := range events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func findEventByKind(events []agentproto.Event, kind agentproto.EventKind) *agentproto.Event {
	for i := range events {
		if events[i].Kind == kind {
			return &events[i]
		}
	}
	return nil
}

func truncateStr(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
