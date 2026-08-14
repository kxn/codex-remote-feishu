package acp

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestUnknownPromptStopWithoutObservableOutputFails(t *testing.T) {
	tests := []struct {
		name       string
		stopReason any
	}{
		{name: "missing"},
		{name: "empty", stopReason: ""},
		{name: "unknown", stopReason: "unknown"},
		{name: "unrecognized", stopReason: "future_reason"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, promptID := startPromptedSession(t)
			payload := map[string]any{}
			if tc.stopReason != nil {
				payload["stopReason"] = tc.stopReason
			}
			result, err := tr.ObserveServer(mustLine(t, map[string]any{
				"jsonrpc": "2.0",
				"id":      promptID,
				"result":  payload,
			}))
			if err != nil {
				t.Fatalf("ObserveServer(prompt response): %v", err)
			}
			assertEventKinds(t, result.Events, agentproto.EventTurnCompleted)
			completed := result.Events[0]
			if completed.Status != "failed" {
				t.Fatalf("completion status = %q, want failed; event=%#v", completed.Status, completed)
			}
			if completed.Problem == nil || completed.Problem.Code != "opencode_empty_response" {
				t.Fatalf("completion problem = %#v, want opencode_empty_response", completed.Problem)
			}
			if completed.Problem.Layer != "wrapper" || completed.Problem.Stage != "observe_server" || completed.Problem.Operation != "session/prompt" {
				t.Fatalf("completion problem location = %#v", completed.Problem)
			}
			if !strings.Contains(completed.Problem.Message, "空响应") ||
				!strings.Contains(completed.Problem.Message, "端点") ||
				!strings.Contains(completed.Problem.Message, "协议") {
				t.Fatalf("completion problem is not actionable: %#v", completed.Problem)
			}
		})
	}
}

func TestUnknownPromptStopWithObservableOutputCompletes(t *testing.T) {
	tests := []struct {
		name   string
		update map[string]any
	}{
		{
			name: "assistant text",
			update: map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"messageId":     "msg-1",
				"content":       map[string]any{"type": "text", "text": "hello"},
			},
		},
		{
			name: "reasoning text",
			update: map[string]any{
				"sessionUpdate": "agent_thought_chunk",
				"messageId":     "thought-1",
				"content":       map[string]any{"type": "text", "text": "checking"},
			},
		},
		{
			name: "visible tool",
			update: map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    "read-1",
				"title":         "Read file",
				"kind":          "read",
				"status":        "completed",
				"rawInput":      map[string]any{"path": "README.md"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, promptID := startPromptedSession(t)
			output, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", tc.update)))
			if err != nil {
				t.Fatalf("ObserveServer(output): %v", err)
			}
			if len(output.Events) == 0 {
				t.Fatalf("test output did not produce any adapter events: %#v", tc.update)
			}

			result, err := tr.ObserveServer(mustLine(t, map[string]any{
				"jsonrpc": "2.0",
				"id":      promptID,
				"result":  map[string]any{"stopReason": "unknown"},
			}))
			if err != nil {
				t.Fatalf("ObserveServer(prompt response): %v", err)
			}
			completed := result.Events[len(result.Events)-1]
			if completed.Kind != agentproto.EventTurnCompleted || completed.Status != "completed" || completed.Problem != nil {
				t.Fatalf("unknown stop with visible output must complete normally: %#v", result.Events)
			}
		})
	}
}

func TestUnknownPromptStopWithProjectedPlanCompletes(t *testing.T) {
	tr, promptID := startPromptedSession(t)
	_, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "todo-1",
		"kind":          "todowrite",
		"status":        "pending",
		"rawInput": map[string]any{
			"todos": []any{map[string]any{"content": "Check endpoint", "status": "in_progress"}},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(todo start): %v", err)
	}
	plan, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "todo-1",
		"kind":          "todowrite",
		"status":        "completed",
		"rawInput": map[string]any{
			"todos": []any{map[string]any{"content": "Check endpoint", "status": "completed"}},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(todo completed): %v", err)
	}
	assertEventKinds(t, plan.Events, agentproto.EventTurnPlanUpdated)

	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      promptID,
		"result":  map[string]any{"stopReason": "unknown"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(prompt response): %v", err)
	}
	completed := result.Events[len(result.Events)-1]
	if completed.Kind != agentproto.EventTurnCompleted || completed.Status != "completed" || completed.Problem != nil {
		t.Fatalf("unknown stop with projected plan must complete normally: %#v", result.Events)
	}
}

func TestKnownPromptStopReasonsKeepExistingStatusMappingForEmptyTurns(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{reason: "end_turn", want: "completed"},
		{reason: "max_turn_requests", want: "completed"},
		{reason: "max_tokens", want: "failed"},
		{reason: "refusal", want: "failed"},
		{reason: "cancelled", want: "cancelled"},
	}

	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			tr, promptID := startPromptedSession(t)
			result, err := tr.ObserveServer(mustLine(t, map[string]any{
				"jsonrpc": "2.0",
				"id":      promptID,
				"result":  map[string]any{"stopReason": tc.reason},
			}))
			if err != nil {
				t.Fatalf("ObserveServer(prompt response): %v", err)
			}
			completed := result.Events[len(result.Events)-1]
			if completed.Kind != agentproto.EventTurnCompleted || completed.Status != tc.want || completed.Problem != nil {
				t.Fatalf("stopReason %q completion = %#v, want status %q without problem", tc.reason, completed, tc.want)
			}
		})
	}
}
