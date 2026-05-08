package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorRequestInsideTaskCarriesSourceContextLabel(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "default")

	assistant := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-task-req-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-task-1",
					"name":  "Task",
					"input": map[string]any{"subagent_type": "Explore", "description": "Audit the repository"},
				},
			},
		},
	})
	if len(assistant.Events) != 1 || assistant.Events[0].ItemKind != "delegated_task" {
		t.Fatalf("expected delegated task start, got %#v", assistant.Events)
	}

	internal := observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":                 "msg-task-ask-1",
			"type":               "message",
			"role":               "assistant",
			"model":              "mimo-v2.5-pro",
			"parent_tool_use_id": "call-task-1",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-ask-1",
					"name":  "AskUserQuestion",
					"input": map[string]any{"questions": []any{map[string]any{"id": "approach", "header": "Approach", "question": "Which approach should I take?"}}},
				},
			},
		},
		"parent_tool_use_id": "call-task-1",
	})
	if len(internal.Events) != 0 {
		t.Fatalf("internal AskUserQuestion should stay hidden, got %#v", internal.Events)
	}

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-task-ask-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "AskUserQuestion",
			"tool_use_id": "call-ask-1",
			"input": map[string]any{
				"questions": []any{
					map[string]any{
						"id":       "approach",
						"header":   "Approach",
						"question": "Which approach should I take?",
					},
				},
			},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	event := requestStarted.Events[0]
	if event.Kind != agentproto.EventRequestStarted || event.ThreadID != threadID || event.TurnID != turnID {
		t.Fatalf("unexpected request.started event: %#v", event)
	}
	if got := lookupStringFromAny(event.Metadata["sourceContextLabel"]); got != "来自 Task (Explore)" {
		t.Fatalf("expected request source context label from parent task, got %#v", event.Metadata)
	}
}
