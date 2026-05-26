package claude

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorToolApprovalAcceptForSessionUsesNativePermissionSuggestions(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")

	requestStarted := observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-tool-session-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-bash-session-1",
			"permission_suggestions": []any{
				map[string]any{
					"type":        "addRules",
					"behavior":    "allow",
					"destination": "session",
					"rules": []any{
						map[string]any{"toolName": "Bash", "ruleContent": "git status:*"},
					},
				},
				map[string]any{
					"type":        "addDirectories",
					"destination": "session",
					"directories": []any{"/data/dl/droid/internal"},
				},
			},
			"input": map[string]any{
				"command":     "git status --short",
				"description": "show working tree",
			},
		},
	})
	if len(requestStarted.Events) != 1 {
		t.Fatalf("expected one request.started event, got %#v", requestStarted.Events)
	}
	if got := testSliceMapValue(requestStarted.Events[0].Metadata["permissionSuggestions"]); len(got) != 2 {
		t.Fatalf("expected metadata to preserve native permission suggestions, got %#v", requestStarted.Events[0].Metadata)
	}

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-tool-session-1",
			Response: map[string]any{
				"decision": "acceptForSession",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate tool approval session grant: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	if lookupStringFromAny(body["behavior"]) != "allow" {
		t.Fatalf("unexpected allow body: %#v", body)
	}
	updates := testSliceMapValue(body["updatedPermissions"])
	if len(updates) != 2 {
		t.Fatalf("expected native suggestions to be forwarded as updatedPermissions, got %#v", body)
	}
	rules := permissionUpdateByType(t, updates, "addRules")
	if lookupStringFromAny(rules["behavior"]) != "allow" || lookupStringFromAny(rules["destination"]) != "session" {
		t.Fatalf("unexpected addRules session grant payload: %#v", rules)
	}
	addDirs := permissionUpdateByType(t, updates, "addDirectories")
	if lookupStringFromAny(addDirs["destination"]) != "session" {
		t.Fatalf("unexpected addDirectories session grant payload: %#v", addDirs)
	}
	if got := testStringList(addDirs["directories"]); len(got) != 1 || got[0] != "/data/dl/droid/internal" {
		t.Fatalf("unexpected addDirectories payload: %#v", addDirs)
	}
	updatedInput := testMapValue(body["updatedInput"])
	if lookupStringFromAny(updatedInput["command"]) != "git status --short" {
		t.Fatalf("expected updatedInput to stay unchanged on session grant, got %#v", updatedInput)
	}
}

func TestClaudeTranslatorToolApprovalAcceptForSessionFailsClosedWithoutNativeSuggestions(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")

	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-tool-session-2",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-bash-session-2",
			"input": map[string]any{
				"command": "git status --short",
			},
		},
	})

	_, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-tool-session-2",
			Response: map[string]any{
				"decision": "acceptForSession",
			},
		},
	})
	problem := expectClaudeCommandError(t, err)
	if problem.Code != "claude_can_use_tool_session_grant_unavailable" {
		t.Fatalf("unexpected session-grant failure: %#v", problem)
	}
	if problem.RequestID != "req-tool-session-2" {
		t.Fatalf("expected request-scoped failure details, got %#v", problem)
	}
}

func TestClaudeTranslatorToolApprovalPlainAcceptKeepsEmptyUpdatedPermissions(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "default")

	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-tool-plain-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "Bash",
			"tool_use_id": "call-bash-plain-1",
			"input": map[string]any{
				"command": "git status --short",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-tool-plain-1",
			Response: map[string]any{
				"decision": "accept",
			},
		},
	})
	if err != nil {
		t.Fatalf("translate tool approval plain accept: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	raw, ok := body["updatedPermissions"].([]any)
	if !ok {
		t.Fatalf("expected updatedPermissions to stay an empty array, got %#v", body["updatedPermissions"])
	}
	if len(raw) != 0 {
		t.Fatalf("expected plain accept to keep updatedPermissions empty, got %#v", raw)
	}
}
