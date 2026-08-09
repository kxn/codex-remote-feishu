package acp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestCanonicalToolTaxonomyMapsOpenCodeKindsToExistingItemKinds(t *testing.T) {
	tr, _ := startPromptedSession(t)

	cases := []struct {
		name         string
		kind         string
		rawInput     map[string]any
		wantItemKind string
		wantMeta     map[string]any
	}{
		{
			name:         "bash",
			kind:         "bash",
			rawInput:     map[string]any{"cmd": "go test ./...", "cwd": "/tmp/work"},
			wantItemKind: "command_execution",
			wantMeta:     map[string]any{"tool": "bash", "command": "go test ./...", "cwd": "/tmp/work"},
		},
		{
			name:         "execute alias",
			kind:         "execute",
			rawInput:     map[string]any{"command": "npm test"},
			wantItemKind: "command_execution",
			wantMeta:     map[string]any{"tool": "execute", "command": "npm test"},
		},
		{
			name:         "read",
			kind:         "read",
			rawInput:     map[string]any{"path": "internal/app/app.go"},
			wantItemKind: "dynamic_tool_call",
			wantMeta:     map[string]any{"tool": "read", "semanticKind": "exploration"},
		},
		{
			name:         "grep",
			kind:         "grep",
			rawInput:     map[string]any{"pattern": "OpenCode", "path": "internal"},
			wantItemKind: "dynamic_tool_call",
			wantMeta:     map[string]any{"tool": "grep", "semanticKind": "exploration"},
		},
		{
			name:         "glob",
			kind:         "glob",
			rawInput:     map[string]any{"pattern": "**/*.go"},
			wantItemKind: "dynamic_tool_call",
			wantMeta:     map[string]any{"tool": "glob", "semanticKind": "exploration"},
		},
		{
			name:         "list",
			kind:         "list",
			rawInput:     map[string]any{"path": "internal/adapter"},
			wantItemKind: "dynamic_tool_call",
			wantMeta:     map[string]any{"tool": "list", "semanticKind": "exploration"},
		},
		{
			name:         "edit",
			kind:         "edit",
			rawInput:     map[string]any{"path": "main.go"},
			wantItemKind: "file_change",
			wantMeta:     map[string]any{"tool": "edit", "semanticKind": "file_change_request", "filePath": "main.go"},
		},
		{
			name:         "write",
			kind:         "write",
			rawInput:     map[string]any{"filePath": "README.md"},
			wantItemKind: "file_change",
			wantMeta:     map[string]any{"tool": "write", "semanticKind": "file_change_request", "filePath": "README.md"},
		},
		{
			name:         "apply patch",
			kind:         "apply_patch",
			rawInput:     map[string]any{"path": "internal/x.go"},
			wantItemKind: "file_change",
			wantMeta:     map[string]any{"tool": "apply_patch", "semanticKind": "file_change_request", "filePath": "internal/x.go"},
		},
		{
			name:         "task",
			kind:         "task",
			rawInput:     map[string]any{"description": "Audit the adapter", "subagent_type": "Explore"},
			wantItemKind: "delegated_task",
			wantMeta:     map[string]any{"tool": "task", "description": "Audit the adapter", "subagentType": "Explore"},
		},
		{
			name:         "mcp",
			kind:         "mcp",
			rawInput:     map[string]any{"server": "feishu", "tool": "search_messages"},
			wantItemKind: "mcp_tool_call",
			wantMeta:     map[string]any{"tool": "search_messages", "server": "feishu"},
		},
		{
			name:         "unknown",
			kind:         "something_new",
			rawInput:     map[string]any{"value": "kept"},
			wantItemKind: "dynamic_tool_call",
			wantMeta:     map[string]any{"tool": "something_new", "semanticKind": "generic_tool"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    "tool_" + strings.ReplaceAll(tc.name, " ", "_"),
				"title":         tc.name,
				"kind":          tc.kind,
				"status":        "pending",
				"rawInput":      tc.rawInput,
			})))
			if err != nil {
				t.Fatalf("ObserveServer(tool_call): %v", err)
			}
			assertEventKinds(t, result.Events, agentproto.EventItemStarted)
			event := result.Events[0]
			if event.ItemKind != tc.wantItemKind {
				t.Fatalf("ItemKind = %q, want %q; event=%#v", event.ItemKind, tc.wantItemKind, event)
			}
			for key, want := range tc.wantMeta {
				if got := event.Metadata[key]; got != want {
					t.Fatalf("metadata[%s] = %#v, want %#v; metadata=%#v", key, got, want, event.Metadata)
				}
			}
			if _, ok := event.Metadata["arguments"].(map[string]any); !ok {
				t.Fatalf("metadata arguments missing cloned raw input: %#v", event.Metadata)
			}
		})
	}
}

func TestCanonicalToolUpdatesUseHumanTextAndKeepTerminalMetadata(t *testing.T) {
	tr, _ := startPromptedSession(t)
	if _, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "shell_1",
		"title":         "go test",
		"kind":          "bash",
		"status":        "pending",
		"rawInput":      map[string]any{"cmd": "go test ./internal/adapter/acp"},
	}))); err != nil {
		t.Fatalf("ObserveServer(tool start): %v", err)
	}

	progress, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "shell_1",
		"status":        "in_progress",
		"content": []any{
			map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "PASS\n"}},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(tool progress): %v", err)
	}
	assertEventKinds(t, progress.Events, agentproto.EventItemDelta)
	if progress.Events[0].Delta != "PASS\n" {
		t.Fatalf("tool progress delta = %q, want human text", progress.Events[0].Delta)
	}

	completed, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "shell_1",
		"status":        "completed",
		"rawOutput":     map[string]any{"output": "PASS\n", "exitCode": 0},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(tool completed): %v", err)
	}
	assertEventKinds(t, completed.Events, agentproto.EventItemCompleted)
	event := completed.Events[0]
	if event.ItemKind != "command_execution" || event.Metadata["command"] != "go test ./internal/adapter/acp" || event.Metadata["exitCode"] != 0 {
		t.Fatalf("completion metadata lost canonical command fields: %#v", event)
	}
}

func TestFailedToolUpdateCompletesOnlyTheToolItem(t *testing.T) {
	tr, _ := startPromptedSession(t)
	result, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "mcp_1",
		"kind":          "mcp",
		"status":        "failed",
		"rawInput":      map[string]any{"server": "feishu", "tool": "search_messages"},
		"rawOutput":     map[string]any{"error": "MCP server unavailable"},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(failed tool): %v", err)
	}
	assertEventKinds(t, result.Events, agentproto.EventItemStarted, agentproto.EventItemCompleted)
	if result.Events[1].ItemKind != "mcp_tool_call" || result.Events[1].Status != "failed" {
		t.Fatalf("failed tool did not complete the item: %#v", result.Events)
	}
	if result.Events[1].Metadata["errorMessage"] != "MCP server unavailable" {
		t.Fatalf("failed tool error metadata missing: %#v", result.Events[1].Metadata)
	}
	for _, event := range result.Events {
		if event.Kind == agentproto.EventTurnCompleted || event.Kind == agentproto.EventSystemError {
			t.Fatalf("tool failure must not be projected as turn/system failure: %#v", result.Events)
		}
	}
}

func TestTodoWriteCompletedEmitsPlanSnapshotWithoutUserVisibleToolItem(t *testing.T) {
	tr, _ := startPromptedSession(t)
	started, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call",
		"toolCallId":    "todo_1",
		"title":         "Update todos",
		"kind":          "todowrite",
		"status":        "pending",
		"rawInput": map[string]any{
			"todos": []any{
				map[string]any{"content": "Read adapter", "status": "completed"},
				map[string]any{"content": "Add golden tests", "status": "in_progress"},
				map[string]any{"content": "Wire daemon", "status": "pending"},
			},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(todo start): %v", err)
	}
	if len(started.Events) != 0 {
		t.Fatalf("todowrite start should stay out of visible tool timeline: %#v", started.Events)
	}

	completed, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "todo_1",
		"kind":          "todowrite",
		"status":        "completed",
		"rawInput": map[string]any{
			"todos": []any{
				map[string]any{"content": "Read adapter", "status": "completed"},
				map[string]any{"content": "Add golden tests", "status": "in_progress"},
				map[string]any{"content": "Wire daemon", "status": "pending"},
			},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(todo completed): %v", err)
	}
	assertEventKinds(t, completed.Events, agentproto.EventTurnPlanUpdated)
	snapshot := completed.Events[0].PlanSnapshot
	if snapshot == nil || len(snapshot.Steps) != 3 {
		t.Fatalf("plan snapshot = %#v", snapshot)
	}
	if snapshot.Steps[1].Step != "Add golden tests" || snapshot.Steps[1].Status != agentproto.TurnPlanStepStatusInProgress {
		t.Fatalf("unexpected plan steps: %#v", snapshot.Steps)
	}
}

func TestPermissionResponseUsesOptionKindForWriteApproval(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "main.go")
	tr, _ := startPromptedSessionWithWorkspace(t, workspace)

	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-kind-once",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall":  map[string]any{"toolCallId": "tool_edit", "title": "Edit file", "kind": "edit", "status": "pending"},
			"options": []any{
				map[string]any{"optionId": "approve-this-change", "kind": "allow_once", "name": "Allow this change"},
				map[string]any{"optionId": "deny", "kind": "reject_once", "name": "Reject"},
			},
		},
	})); err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve-kind",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-kind-once",
			Response:  map[string]any{"optionId": "approve-this-change"},
		},
	}); err != nil {
		t.Fatalf("TranslateCommand(approve by kind): %v", err)
	}
	written, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "write-kind-once",
		"method":  "fs/write_text_file",
		"params":  map[string]any{"sessionId": "ses_1", "path": target, "content": "package main\n"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(write): %v", err)
	}
	frame := decodeFrame(t, written.OutboundToChild[0])
	if frame["error"] != nil {
		t.Fatalf("allow_once option kind did not grant write approval: %#v", frame)
	}
}

func TestPermissionAlwaysKindAllowsMultipleWorkspaceWrites(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "main.go")
	tr, _ := startPromptedSessionWithWorkspace(t, workspace)

	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "perm-kind-always",
		"method":  "session/request_permission",
		"params": map[string]any{
			"sessionId": "ses_1",
			"toolCall":  map[string]any{"toolCallId": "tool_edit", "title": "Edit file", "kind": "edit", "status": "pending"},
			"options": []any{
				map[string]any{"optionId": "approve-session", "kind": "allow_always", "name": "Allow always"},
				map[string]any{"optionId": "deny", "kind": "reject_once", "name": "Reject"},
			},
		},
	})); err != nil {
		t.Fatalf("ObserveServer(permission): %v", err)
	}
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-approve-always",
		Kind:      agentproto.CommandRequestRespond,
		Request: agentproto.Request{
			RequestID: "perm-kind-always",
			Response:  map[string]any{"optionId": "approve-session"},
		},
	}); err != nil {
		t.Fatalf("TranslateCommand(approve always): %v", err)
	}
	for i, content := range []string{"package main\n", "package main\n\nfunc main() {}\n"} {
		result, err := tr.ObserveServer(mustLine(t, map[string]any{
			"jsonrpc": "2.0",
			"id":      fmt.Sprintf("write-always-%d", i),
			"method":  "fs/write_text_file",
			"params":  map[string]any{"sessionId": "ses_1", "path": target, "content": content},
		}))
		if err != nil {
			t.Fatalf("ObserveServer(write %d): %v", i, err)
		}
		frame := decodeFrame(t, result.OutboundToChild[0])
		if frame["error"] != nil {
			t.Fatalf("allow_always write %d failed: %#v", i, frame)
		}
	}
}

func TestToolUpdateFirstFrameEmitsStartedBeforeDeltaAndDedupesTerminal(t *testing.T) {
	tr, _ := startPromptedSession(t)
	progress, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "read_1",
		"kind":          "read",
		"status":        "in_progress",
		"rawInput":      map[string]any{"path": "README.md"},
		"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "# README\n"}}},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(update first): %v", err)
	}
	assertEventKinds(t, progress.Events, agentproto.EventItemStarted, agentproto.EventItemDelta)
	if progress.Events[0].ItemKind != "dynamic_tool_call" || progress.Events[1].ItemID != progress.Events[0].ItemID {
		t.Fatalf("unexpected update-first events: %#v", progress.Events)
	}

	completed, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "read_1",
		"status":        "completed",
	})))
	if err != nil {
		t.Fatalf("ObserveServer(completed): %v", err)
	}
	assertEventKinds(t, completed.Events, agentproto.EventItemCompleted)

	duplicate, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "read_1",
		"status":        "completed",
	})))
	if err != nil {
		t.Fatalf("ObserveServer(duplicate completed): %v", err)
	}
	if len(duplicate.Events) != 0 {
		t.Fatalf("duplicate terminal update emitted events: %#v", duplicate.Events)
	}
}

func TestPromptResponseCompletesOpenTextItemsBeforeTurnCompletion(t *testing.T) {
	tr, promptID := startPromptedSession(t)
	if _, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_thought_chunk",
		"messageId":     "msg_thought",
		"content":       map[string]any{"type": "text", "text": "thinking"},
	}))); err != nil {
		t.Fatalf("ObserveServer(thought): %v", err)
	}
	if _, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "msg_text",
		"content":       map[string]any{"type": "text", "text": "hello"},
	}))); err != nil {
		t.Fatalf("ObserveServer(message): %v", err)
	}

	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      promptID,
		"result":  map[string]any{"stopReason": "end_turn"},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(prompt response): %v", err)
	}
	assertEventKinds(t, result.Events,
		agentproto.EventItemCompleted,
		agentproto.EventItemCompleted,
		agentproto.EventTurnCompleted,
	)
	if result.Events[0].ItemKind != "reasoning_summary" || result.Events[1].ItemKind != "agent_message" {
		t.Fatalf("open text items completed in unexpected order: %#v", result.Events)
	}
}

func TestUnknownJSONRPCResponseIsDebugOnly(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	var logs []string
	tr.SetDebugLogger(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	result, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      "not-pending",
		"result":  map[string]any{"ok": true},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(unknown response): %v", err)
	}
	if len(result.Events) != 0 || len(result.OutboundToChild) != 0 {
		t.Fatalf("unknown response should stay debug-only: %#v", result)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "unknown response") {
		t.Fatalf("unknown response was not debug logged: %#v", logs)
	}
}

func TestSessionLoadErrorClearsHydrationState(t *testing.T) {
	tr := NewTranslator("inst-1", "/tmp/work")
	result, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-history",
		Kind:      agentproto.CommandThreadHistoryRead,
		Target:    agentproto.Target{ThreadID: "ses_1", CWD: "/tmp/work"},
	})
	if err != nil {
		t.Fatalf("TranslateCommand(history): %v", err)
	}
	frame := decodeFrame(t, result.OutboundToChild[0])
	if _, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "msg_replay",
		"content":       map[string]any{"type": "text", "text": "from failed load"},
	}))); err != nil {
		t.Fatalf("ObserveServer(replay): %v", err)
	}
	if _, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      frame["id"],
		"error":   map[string]any{"code": -32000, "message": "session not found"},
	})); err != nil {
		t.Fatalf("ObserveServer(load error): %v", err)
	}
	live, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "agent_message_chunk",
		"messageId":     "msg_live",
		"content":       map[string]any{"type": "text", "text": "live"},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(live after failed load): %v", err)
	}
	assertEventKinds(t, live.Events, agentproto.EventItemStarted, agentproto.EventItemDelta)
}

func TestUsageUpdateStaysDebugOnlyWhilePromptResponseUsageProjectsToTurn(t *testing.T) {
	tr, promptID := startPromptedSession(t)
	var logs []string
	tr.SetDebugLogger(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})

	contextUsage, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "usage_update",
		"used":          1200,
		"size":          64000,
	})))
	if err != nil {
		t.Fatalf("ObserveServer(usage_update): %v", err)
	}
	if len(contextUsage.Events) != 0 {
		t.Fatalf("usage_update should not emit user-visible token usage events: %#v", contextUsage.Events)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "usage_update") {
		t.Fatalf("usage_update was not recorded in debug log: %#v", logs)
	}

	promptUsage, err := tr.ObserveServer(mustLine(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      promptID,
		"result": map[string]any{
			"stopReason": "end_turn",
			"usage":      map[string]any{"inputTokens": 5, "outputTokens": 7},
		},
	}))
	if err != nil {
		t.Fatalf("ObserveServer(prompt response): %v", err)
	}
	assertEventKinds(t, promptUsage.Events, agentproto.EventThreadTokenUsageUpdated, agentproto.EventTurnCompleted)
	if promptUsage.Events[0].TokenUsage == nil || promptUsage.Events[0].TokenUsage.Last.TotalTokens != 12 {
		t.Fatalf("prompt response usage did not project to turn usage: %#v", promptUsage.Events[0])
	}
}

func TestCanonicalRawJSONLFixtureMatchesGoldenProjection(t *testing.T) {
	tr, _ := startPromptedSession(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "canonical_session_updates.input.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var projected []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		result, err := tr.ObserveServer([]byte(line + "\n"))
		if err != nil {
			t.Fatalf("ObserveServer(%s): %v", line, err)
		}
		for _, event := range result.Events {
			projected = append(projected, stableEventProjection(event))
		}
	}
	got, err := json.MarshalIndent(projected, "", "  ")
	if err != nil {
		t.Fatalf("marshal projected events: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "canonical_session_updates.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRPCErrorNormalizerClassifiesKnownOpenCodeFailures(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    string
	}{
		{name: "missing session", message: "session ses_404 not found", want: "opencode_session_not_found"},
		{name: "invalid model", message: "invalid model provider/missing-model", want: "opencode_invalid_model"},
		{name: "mcp failure", message: "MCP server feishu failed to start", want: "opencode_mcp_failure"},
		{name: "auth required", message: "authentication required: missing API key", want: "opencode_auth_required"},
		{name: "permission denied", message: "permission denied by policy", want: "opencode_permission_denied"},
		{name: "unknown", message: "upstream returned a strange error", want: "opencode_acp_request_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewTranslator("inst-1", "/tmp/work")
			result, err := tr.TranslateCommand(agentproto.Command{
				CommandID: "cmd-history",
				Kind:      agentproto.CommandThreadHistoryRead,
				Target:    agentproto.Target{ThreadID: "ses_1", CWD: "/tmp/work"},
			})
			if err != nil {
				t.Fatalf("TranslateCommand(history): %v", err)
			}
			frame := decodeFrame(t, result.OutboundToChild[0])
			observed, err := tr.ObserveServer(mustLine(t, map[string]any{
				"jsonrpc": "2.0",
				"id":      frame["id"],
				"error":   map[string]any{"code": -32000, "message": tc.message},
			}))
			if err != nil {
				t.Fatalf("ObserveServer(error): %v", err)
			}
			assertEventKinds(t, observed.Events, agentproto.EventSystemError)
			if observed.Events[0].Problem == nil || observed.Events[0].Problem.Code != tc.want {
				t.Fatalf("problem code = %#v, want %s", observed.Events[0].Problem, tc.want)
			}
		})
	}
}

func TestConfigOptionUpdateRefreshesModelCatalogState(t *testing.T) {
	tr, _ := startPromptedSession(t)
	observed, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate": "config_option_update",
		"configOption": map[string]any{
			"id":           "model",
			"type":         "select",
			"currentValue": "test/default",
			"options": []any{
				map[string]any{"value": "test/default", "name": "Default Model"},
				map[string]any{"value": "test/large", "name": "Large Model"},
			},
		},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(config option): %v", err)
	}
	assertEventKinds(t, observed.Events, agentproto.EventThreadSettingsUpdated)
	if observed.Events[0].ThreadSettings == nil || observed.Events[0].ThreadSettings.Model != "test/default" {
		t.Fatalf("config option did not emit thread settings: %#v", observed.Events[0])
	}

	models, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-models",
		Kind:      agentproto.CommandModelList,
	})
	if err != nil {
		t.Fatalf("TranslateCommand(model.list): %v", err)
	}
	assertEventKinds(t, models.Events, agentproto.EventModelCatalogUpdated)
	catalog := models.Events[0].ModelCatalog
	if catalog == nil || catalog.Unsupported || len(catalog.Entries) != 2 || !catalog.Entries[0].IsDefault {
		t.Fatalf("model catalog = %#v", catalog)
	}
}

func TestAvailableCommandsUpdateIsDebugOnly(t *testing.T) {
	tr, _ := startPromptedSession(t)
	var logs []string
	tr.SetDebugLogger(func(format string, args ...any) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})
	result, err := tr.ObserveServer(mustLine(t, sessionUpdate("ses_1", map[string]any{
		"sessionUpdate":     "available_commands_update",
		"availableCommands": []any{map[string]any{"name": "init", "description": "Initialize project"}},
	})))
	if err != nil {
		t.Fatalf("ObserveServer(available commands): %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("available_commands_update must not replace product command menu: %#v", result.Events)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "available_commands_update") {
		t.Fatalf("available_commands_update was not recorded in debug log: %#v", logs)
	}
}

func stableEventProjection(event agentproto.Event) map[string]any {
	out := map[string]any{"kind": string(event.Kind)}
	if event.CommandID != "" {
		out["commandId"] = event.CommandID
	}
	if event.ThreadID != "" {
		out["threadId"] = event.ThreadID
	}
	if event.TurnID != "" {
		out["turnId"] = event.TurnID
	}
	if event.ItemID != "" {
		out["itemId"] = event.ItemID
	}
	if event.ItemKind != "" {
		out["itemKind"] = event.ItemKind
	}
	if event.Status != "" {
		out["status"] = event.Status
	}
	if event.Name != "" {
		out["name"] = event.Name
	}
	if event.Delta != "" {
		out["delta"] = event.Delta
	}
	if event.Initiator.Kind != "" {
		out["initiator"] = map[string]any{
			"kind":             string(event.Initiator.Kind),
			"surfaceSessionId": event.Initiator.SurfaceSessionID,
		}
	}
	if len(event.Metadata) != 0 {
		out["metadata"] = event.Metadata
	}
	if event.PlanSnapshot != nil {
		out["planSnapshot"] = event.PlanSnapshot
	}
	if event.ThreadSettings != nil {
		out["threadSettings"] = event.ThreadSettings
	}
	return out
}
