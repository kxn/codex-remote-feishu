package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func TestClaudeTranslatorPlanReviseDoesNotInterrupt(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-revise-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-revise-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-revise-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-revise-1",
			"input":       map[string]any{"plan": "1. Update README\n2. Run tests"},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-revise-1",
			Response: map[string]any{
				"decision": "revise",
				"message":  "Add a rollback step before execution.",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan revise: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	if xutil.LookupStringFromAny(body["behavior"]) != "deny" {
		t.Fatalf("unexpected revise body: %#v", body)
	}
	if _, ok := body["interrupt"]; ok {
		t.Fatalf("expected revise deny body not to interrupt, got %#v", body)
	}
	if xutil.LookupStringFromAny(body["message"]) != "Add a rollback step before execution." {
		t.Fatalf("unexpected revise message: %#v", body)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-revise-1",
					"content":     "Add a rollback step before execution.",
					"is_error":    true,
				},
			},
		},
		"tool_use_result": "Error: Add a rollback step before execution.",
	})
	if len(resolved.Events) != 1 || resolved.Events[0].Kind != agentproto.EventRequestResolved {
		t.Fatalf("expected request.resolved on plan revise, got %#v", resolved.Events)
	}
	if resolved.Events[0].ThreadID != threadID || resolved.Events[0].TurnID != turnID {
		t.Fatalf("unexpected resolved event ids: %#v", resolved.Events[0])
	}
	if xutil.LookupStringFromAny(resolved.Events[0].Metadata["decision"]) != "revise" {
		t.Fatalf("unexpected resolved revise metadata: %#v", resolved.Events[0].Metadata)
	}
}
