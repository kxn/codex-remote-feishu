package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorExplorationToolUseAndResultActions(t *testing.T) {
	cases := []struct {
		name          string
		tool          string
		input         map[string]any
		result        any
		wantKind      agentproto.ExplorationActionKind
		wantItems     []string
		wantSummary   string
		wantSecondary string
	}{
		{
			name:      "read",
			tool:      "Read",
			input:     map[string]any{"file_path": "internal/adapter/claude/observe.go", "offset": 100, "limit": 20},
			result:    "observe.go result text must not be used as summary",
			wantKind:  agentproto.ExplorationActionRead,
			wantItems: []string{"internal/adapter/claude/observe.go"},
		},
		{
			name:        "glob",
			tool:        "Glob",
			input:       map[string]any{"path": "internal/adapter/claude", "pattern": "**/*_test.go"},
			result:      map[string]any{"filenames": []any{"wrong/result/path.go"}},
			wantKind:    agentproto.ExplorationActionList,
			wantSummary: "internal/adapter/claude",
		},
		{
			name:          "grep",
			tool:          "Grep",
			input:         map[string]any{"pattern": "ClaudeToolMetadata", "path": "internal/claudeutil", "output_mode": "content"},
			result:        "some matching output that must not replace pattern",
			wantKind:      agentproto.ExplorationActionSearch,
			wantSummary:   "ClaudeToolMetadata",
			wantSecondary: "internal/claudeutil",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTranslator("inst-1")
			startClaudeTurn(t, tr, "default")
			toolUseID := "call-" + tt.name + "-1"
			started := observeClaude(t, tr, map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"id":      "msg-" + tt.name + "-1",
					"type":    "message",
					"role":    "assistant",
					"content": []any{map[string]any{"type": "tool_use", "id": toolUseID, "name": tt.tool, "input": tt.input}},
				},
			})
			if len(started.Events) != 1 {
				t.Fatalf("expected one started event, got %#v", started.Events)
			}
			start := started.Events[0]
			assertClaudeExplorationEvent(t, start, agentproto.EventItemStarted, "", tt.wantKind, tt.wantItems, tt.wantSummary, tt.wantSecondary)

			completed := observeClaude(t, tr, map[string]any{
				"type": "user",
				"message": map[string]any{
					"role": "user",
					"content": []any{map[string]any{
						"type":        "tool_result",
						"tool_use_id": toolUseID,
						"content":     "raw result content should not shape exploration",
						"is_error":    true,
					}},
				},
				"tool_use_result": tt.result,
			})
			if len(completed.Events) != 1 {
				t.Fatalf("expected one completed event, got %#v", completed.Events)
			}
			done := completed.Events[0]
			if done.ItemID != start.ItemID {
				t.Fatalf("completion item id = %q, want start item id %q", done.ItemID, start.ItemID)
			}
			assertClaudeExplorationEvent(t, done, agentproto.EventItemCompleted, "failed", tt.wantKind, tt.wantItems, tt.wantSummary, tt.wantSecondary)
		})
	}
}

func TestClaudeTranslatorIncompleteExplorationFallsBackOnCompletion(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")
	started := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":      "msg-read-incomplete-1",
			"type":    "message",
			"role":    "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "call-read-incomplete-1", "name": "Read", "input": map[string]any{"offset": 20}}},
		},
	})
	if len(started.Events) != 1 {
		t.Fatalf("expected one started event, got %#v", started.Events)
	}
	if started.Events[0].Exploration == nil || len(started.Events[0].Exploration.Actions) != 1 || started.Events[0].Exploration.Actions[0].Kind != agentproto.ExplorationActionRead {
		t.Fatalf("expected incomplete known exploration action on start, got %#v", started.Events[0])
	}

	completed := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type":        "tool_result",
				"tool_use_id": "call-read-incomplete-1",
				"content":     "result path /tmp/from-result.txt must not complete action",
				"is_error":    false,
			}},
		},
		"tool_use_result": map[string]any{"file_path": "/tmp/from-result.txt"},
	})
	if len(completed.Events) != 1 {
		t.Fatalf("expected one completed event, got %#v", completed.Events)
	}
	if completed.Events[0].Exploration == nil || len(completed.Events[0].Exploration.Actions) != 1 || len(completed.Events[0].Exploration.Actions[0].Items) != 0 {
		t.Fatalf("completion should keep incomplete original input action for shared fallback, got %#v", completed.Events[0])
	}
}

func assertClaudeExplorationEvent(t *testing.T, event agentproto.Event, kind agentproto.EventKind, status string, actionKind agentproto.ExplorationActionKind, items []string, summary, secondary string) {
	t.Helper()
	if event.Kind != kind || event.ItemKind != "dynamic_tool_call" || event.Status != status {
		t.Fatalf("unexpected lifecycle event: %#v", event)
	}
	if event.Exploration == nil || len(event.Exploration.Actions) != 1 {
		t.Fatalf("expected one exploration action, got %#v", event.Exploration)
	}
	action := event.Exploration.Actions[0]
	if action.Kind != actionKind {
		t.Fatalf("action kind = %q, want %q", action.Kind, actionKind)
	}
	if len(action.Items) != len(items) {
		t.Fatalf("action items = %#v, want %#v", action.Items, items)
	}
	for i := range items {
		if action.Items[i] != items[i] {
			t.Fatalf("action items = %#v, want %#v", action.Items, items)
		}
	}
	if action.Summary != summary || action.Secondary != secondary {
		t.Fatalf("action summary/secondary = %q/%q, want %q/%q", action.Summary, action.Secondary, summary, secondary)
	}
}
