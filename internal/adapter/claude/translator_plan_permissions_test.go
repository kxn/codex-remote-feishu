package claude

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeTranslatorPlanConfirmationScopedSessionSelectionBuildsSessionRules(t *testing.T) {
	tr := NewTranslator("inst-1")
	threadID, turnID := startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-updates-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-updates-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-updates-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-updates-1",
			"input": map[string]any{
				"plan": "1. Edit internal/adapter/claude/commands.go\n2. Add shared spec note",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-updates-1",
			Response: map[string]any{
				"decision": "accept",
				"permissionSelection": map[string]any{
					"scope":       "session",
					"grant_level": "scoped_rules",
					"directories": []any{
						"/data/dl/droid/internal/adapter/claude",
						"/data/dl/shared-specs",
					},
					"rule_classes": []any{
						"edit_existing_files",
						"create_new_files",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan scoped permissions: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	if lookupStringFromAny(body["behavior"]) != "allow" {
		t.Fatalf("unexpected response body: %#v", body)
	}
	updates := testSliceMapValue(body["updatedPermissions"])
	if len(updates) != 2 {
		t.Fatalf("expected addRules + addDirectories updates, got %#v", updates)
	}

	rulesUpdate := permissionUpdateByType(t, updates, "addRules")
	if lookupStringFromAny(rulesUpdate["destination"]) != "session" || lookupStringFromAny(rulesUpdate["behavior"]) != "allow" {
		t.Fatalf("unexpected addRules update: %#v", rulesUpdate)
	}
	assertPermissionRule(t, testSliceMapValue(rulesUpdate["rules"]), "Edit", "./internal/adapter/claude/**")
	assertPermissionRule(t, testSliceMapValue(rulesUpdate["rules"]), "Write", "./internal/adapter/claude/**")
	assertPermissionRule(t, testSliceMapValue(rulesUpdate["rules"]), "Edit", "//data/dl/shared-specs/**")
	assertPermissionRule(t, testSliceMapValue(rulesUpdate["rules"]), "Write", "//data/dl/shared-specs/**")

	dirsUpdate := permissionUpdateByType(t, updates, "addDirectories")
	if lookupStringFromAny(dirsUpdate["destination"]) != "session" {
		t.Fatalf("unexpected addDirectories update: %#v", dirsUpdate)
	}
	if got := testStringList(dirsUpdate["directories"]); len(got) != 1 || got[0] != "/data/dl/shared-specs" {
		t.Fatalf("unexpected addDirectories payload: %#v", dirsUpdate)
	}

	resolved := observeClaude(t, tr, map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "call-plan-updates-1",
					"content":     "ok",
					"is_error":    false,
				},
			},
		},
		"tool_use_result": map[string]any{"text": "ok"},
	})
	if len(resolved.Events) != 1 || resolved.Events[0].Kind != agentproto.EventRequestResolved || resolved.Events[0].ThreadID != threadID || resolved.Events[0].TurnID != turnID {
		t.Fatalf("unexpected resolved event: %#v", resolved.Events)
	}
}

func TestClaudeTranslatorPlanConfirmationAggressiveWorkspaceSelectionUsesSessionAcceptEdits(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-accept-edits-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-accept-edits-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-accept-edits-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-accept-edits-1",
			"input": map[string]any{
				"plan": "1. Update files across the workspace\n2. Move files\n3. Delete obsolete files",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-accept-edits-1",
			Response: map[string]any{
				"decision": "accept",
				"permissionSelection": map[string]any{
					"scope":       "session",
					"grant_level": "session_file_edits_and_fs_ops",
					"directories": []any{"/data/dl/droid"},
					"rule_classes": []any{
						"edit_existing_files",
						"create_new_files",
						"rename_or_move_files",
						"delete_plan_files",
						"run_common_fs_commands",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan session acceptEdits: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	updates := testSliceMapValue(body["updatedPermissions"])
	if len(updates) != 1 {
		t.Fatalf("expected single setMode update, got %#v", updates)
	}
	update := permissionUpdateByType(t, updates, "setMode")
	if lookupStringFromAny(update["mode"]) != "acceptEdits" || lookupStringFromAny(update["destination"]) != "session" {
		t.Fatalf("unexpected acceptEdits update: %#v", update)
	}
}

func TestClaudeTranslatorPlanConfirmationNarrowScopeAvoidsAcceptEditsAndExplainsFallback(t *testing.T) {
	tr := NewTranslator("inst-1")
	startClaudeTurn(t, tr, "plan")

	observeClaude(t, tr, map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"id":    "msg-plan-narrow-1",
			"type":  "message",
			"role":  "assistant",
			"model": "mimo-v2.5-pro",
			"content": []any{
				map[string]any{
					"type":  "tool_use",
					"id":    "call-plan-narrow-1",
					"name":  "ExitPlanMode",
					"input": map[string]any{},
				},
			},
		},
	})
	observeClaude(t, tr, map[string]any{
		"type":       "control_request",
		"request_id": "req-plan-narrow-1",
		"request": map[string]any{
			"subtype":     "can_use_tool",
			"tool_name":   "ExitPlanMode",
			"tool_use_id": "call-plan-narrow-1",
			"input": map[string]any{
				"plan": "1. Move files inside internal/adapter/claude\n2. Delete obsolete snapshots",
			},
		},
	})

	payloads, err := tr.TranslateCommand(agentproto.Command{
		Kind: agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "req-plan-narrow-1",
			Response: map[string]any{
				"decision": "accept",
				"permissionSelection": map[string]any{
					"scope":       "session",
					"grant_level": "session_file_edits_and_fs_ops",
					"directories": []any{"/data/dl/droid/internal/adapter/claude"},
					"rule_classes": []any{
						"edit_existing_files",
						"create_new_files",
						"rename_or_move_files",
						"delete_plan_files",
						"run_common_fs_commands",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("translate plan narrow fallback: %v", err)
	}
	body := testMapValue(testMapValue(decodeFrame(t, payloads[0])["response"])["response"])
	updates := testSliceMapValue(body["updatedPermissions"])
	if len(updates) == 0 {
		t.Fatalf("expected narrowed additive updates, got %#v", body)
	}
	if hasPermissionUpdateType(updates, "setMode") {
		t.Fatalf("expected narrow directory selection to avoid acceptEdits, got %#v", updates)
	}
	updatedInput := testMapValue(body["updatedInput"])
	feedback := lookupStringFromAny(updatedInput["feedback"])
	if !strings.Contains(feedback, "still require approval") {
		t.Fatalf("expected fallback feedback note, got %q", feedback)
	}
}

func permissionUpdateByType(t *testing.T, updates []map[string]any, want string) map[string]any {
	t.Helper()
	for _, update := range updates {
		if lookupStringFromAny(update["type"]) == want {
			return update
		}
	}
	t.Fatalf("permission update %q not found in %#v", want, updates)
	return nil
}

func hasPermissionUpdateType(updates []map[string]any, want string) bool {
	for _, update := range updates {
		if lookupStringFromAny(update["type"]) == want {
			return true
		}
	}
	return false
}

func assertPermissionRule(t *testing.T, rules []map[string]any, toolName, ruleContent string) {
	t.Helper()
	for _, rule := range rules {
		if lookupStringFromAny(rule["toolName"]) == toolName && lookupStringFromAny(rule["ruleContent"]) == ruleContent {
			return
		}
	}
	t.Fatalf("permission rule %s(%s) not found in %#v", toolName, ruleContent, rules)
}

func testStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(item.(string)); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
