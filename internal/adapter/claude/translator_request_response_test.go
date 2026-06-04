package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorApprovalResponseWithoutDecisionFailsTranslation(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-tool-missing-decision-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-bash-missing-decision-1",
					"name":  "Bash",
					"input": map[string]any{"command": "printf ok", "description": "Print ok"},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-tool-missing-decision-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-bash-missing-decision-1",
			"input": map[string]any{
				"command":     "printf ok",
				"description": "Print ok",
			},
		},
	})

	_, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-tool-missing-decision-1",
			Response:  map[string]any{},
		},
	})
	problem := expectClaudeCommandError(t, err)
	if problem.Code != "claude_request_decision_missing" {
		t.Fatalf("unexpected problem: %#v", problem)
	}
}
