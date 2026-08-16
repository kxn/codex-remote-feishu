package acp

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPermissionRequestMergesTrackedReadToolCallDetails(t *testing.T) {
	tr, _ := startPromptedSession(t)
	for _, update := range []map[string]any{
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "tool_read",
			"title":         "read",
			"kind":          "read",
			"status":        "pending",
			"rawInput":      map[string]any{},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "tool_read",
			"title":         "read",
			"kind":          "read",
			"status":        "in_progress",
			"locations": []any{
				map[string]any{"path": "/data/dl/fido2key/docs/README.md"},
			},
			"rawInput": map[string]any{
				"filePath": "/data/dl/fido2key/docs/README.md",
			},
		}),
	} {
		if _, err := tr.ObserveServer(mustLine(t, update)); err != nil {
			t.Fatalf("ObserveServer(update): %v", err)
		}
	}

	requested, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-read",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall": map[string]any{
				"toolCallId": "tool_read",
				"title":      "read",
				"kind":       "read",
				"status":     "pending",
				"locations":  []any{},
				"rawInput":   map[string]any{},
			},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	assertEventKinds(t, requested.Events, agentproto.EventRequestStarted)
	event := requested.Events[0]
	prompt := event.RequestPrompt
	if prompt == nil || !strings.Contains(prompt.Body, "/data/dl/fido2key/docs/README.md") {
		t.Fatalf("read permission body must carry tracked file path, got %#v", prompt)
	}
	if event.Metadata == nil {
		t.Fatalf("read permission must carry metadata for presentation, got %#v", event)
	}
	if event.Metadata["requestKind"] != "approval_can_use_tool" {
		t.Fatalf("read permission requestKind = %#v, want approval_can_use_tool", event.Metadata["requestKind"])
	}
	if event.Metadata["blockedPath"] != "/data/dl/fido2key/docs/README.md" {
		t.Fatalf("read permission blockedPath = %#v", event.Metadata["blockedPath"])
	}
	if prompt.Permissions == nil || len(prompt.Permissions.Permissions) != 1 {
		t.Fatalf("read permission record missing merged tool call: %#v", prompt)
	}
	rawInput := asMap(t, prompt.Permissions.Permissions[0]["rawInput"])
	if rawInput["filePath"] != "/data/dl/fido2key/docs/README.md" {
		t.Fatalf("read permission rawInput lost tracked file path: %#v", prompt.Permissions.Permissions[0])
	}
}

func TestPermissionRequestMergesTrackedExecuteToolCallDetails(t *testing.T) {
	tr, _ := startPromptedSession(t)
	for _, update := range []map[string]any{
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    "tool_exec",
			"title":         "bash",
			"kind":          "execute",
			"status":        "pending",
			"rawInput":      map[string]any{"cwd": "/data/dl/fido2key"},
		}),
		sessionUpdate("ses_1", map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    "tool_exec",
			"title":         "git status",
			"kind":          "execute",
			"status":        "in_progress",
			"rawInput": map[string]any{
				"command": "git status",
				"cwd":     "/data/dl/fido2key",
			},
		}),
	} {
		if _, err := tr.ObserveServer(mustLine(t, update)); err != nil {
			t.Fatalf("ObserveServer(update): %v", err)
		}
	}

	requested, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-exec",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall": map[string]any{
				"toolCallId": "tool_exec",
				"title":      "git status",
				"kind":       "execute",
				"status":     "pending",
				"locations":  []any{},
				"rawInput":   map[string]any{"command": "git status"},
			},
			"options": []any{
				map[string]any{"optionId": "once", "kind": "allow_once", "name": "Allow once"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	assertEventKinds(t, requested.Events, agentproto.EventRequestStarted)
	event := requested.Events[0]
	prompt := event.RequestPrompt
	if prompt == nil || !strings.Contains(prompt.Body, "git status") {
		t.Fatalf("execute permission body must carry tracked command, got %#v", prompt)
	}
	if event.Metadata == nil || event.Metadata["requestKind"] != "approval_command" {
		t.Fatalf("execute permission requestKind = %#v, want approval_command", event.Metadata)
	}
	if event.Metadata["cwd"] != "/data/dl/fido2key" {
		t.Fatalf("execute permission metadata lost tracked cwd: %#v", event.Metadata)
	}
	if prompt.Permissions == nil || len(prompt.Permissions.Permissions) != 1 {
		t.Fatalf("execute permission record missing merged tool call: %#v", prompt)
	}
	rawInput := asMap(t, prompt.Permissions.Permissions[0]["rawInput"])
	if rawInput["command"] != "git status" || rawInput["cwd"] != "/data/dl/fido2key" {
		t.Fatalf("execute permission rawInput lost tracked details: %#v", prompt.Permissions.Permissions[0])
	}
}
