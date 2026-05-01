package claude

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestReadThreadHistoryGroupsTurnsAndPatchesLatestRunningTurn(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceRoot := filepath.Join(t.TempDir(), "ws-history")
	writeClaudeSessionFile(t, configDir, workspaceRoot, "session-1", time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "timestamp": "2026-04-28T11:00:00Z", "cwd": workspaceRoot, "session_id": "session-1", "model": "mimo-v2.5-pro", "permissionMode": "plan"},
		{"type": "user", "timestamp": "2026-04-28T11:01:00Z", "promptId": "prompt-1", "message": map[string]any{"role": "user", "content": "first input"}},
		{"type": "assistant", "timestamp": "2026-04-28T11:01:05Z", "promptId": "prompt-1", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "first answer"}}}},
		{"type": "user", "timestamp": "2026-04-28T11:02:00Z", "promptId": "prompt-side", "isSidechain": true, "message": map[string]any{"role": "user", "content": "ignore me"}},
		{"type": "user", "timestamp": "2026-04-28T11:03:00Z", "promptId": "prompt-2", "message": map[string]any{"role": "user", "content": "second input"}},
		{"type": "assistant", "timestamp": "2026-04-28T11:03:10Z", "promptId": "prompt-2", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "tool-1", "name": "Bash", "input": map[string]any{"command": "printf hi", "description": "Print hi"}}}}},
		{"type": "user", "timestamp": "2026-04-28T11:03:12Z", "promptId": "prompt-2", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tool-1", "content": "hi"}}}},
	})

	history, err := readThreadHistory(workspaceRoot, "session-1", RuntimeStateSnapshot{
		SessionID:          "session-1",
		Model:              "mimo-v2.5-pro",
		PlanMode:           "on",
		ActiveTurnID:       "turn-live",
		WaitingOnApproval:  true,
		WaitingOnUserInput: false,
	})
	if err != nil {
		t.Fatalf("readThreadHistory: %v", err)
	}
	if history == nil {
		t.Fatal("expected history record")
	}
	if history.Thread.ThreadID != "session-1" || history.Thread.PlanMode != "on" {
		t.Fatalf("unexpected thread snapshot: %#v", history.Thread)
	}
	if history.Thread.RuntimeStatus == nil || history.Thread.RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeActive || !history.Thread.RuntimeStatus.HasFlag(agentproto.ThreadActiveFlagWaitingOnApproval) {
		t.Fatalf("expected active runtime status patch, got %#v", history.Thread.RuntimeStatus)
	}
	if len(history.Turns) != 2 {
		t.Fatalf("expected 2 grouped turns, got %#v", history.Turns)
	}
	if history.Turns[0].TurnID != "prompt-1" || history.Turns[0].Status != "completed" {
		t.Fatalf("unexpected first turn: %#v", history.Turns[0])
	}
	if len(history.Turns[0].Items) != 2 || history.Turns[0].Items[0].Kind != "user_message" || history.Turns[0].Items[1].Kind != "agent_message" {
		t.Fatalf("unexpected first turn items: %#v", history.Turns[0].Items)
	}
	last := history.Turns[1]
	if last.TurnID != "turn-live" || last.Status != "running" {
		t.Fatalf("expected live turn patch on latest turn, got %#v", last)
	}
	if len(last.Items) != 2 || last.Items[0].Kind != "user_message" || last.Items[1].Kind != "command_execution" {
		t.Fatalf("unexpected latest turn items: %#v", last.Items)
	}
	if last.Items[1].Command != "printf hi" {
		t.Fatalf("expected Bash command summary, got %#v", last.Items[1])
	}
}

func TestReadThreadHistoryReturnsEmptyRecordWhenTranscriptMissing(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceRoot := filepath.Join(t.TempDir(), "ws-empty")
	history, err := readThreadHistory(workspaceRoot, "missing-session", RuntimeStateSnapshot{})
	if err != nil {
		t.Fatalf("readThreadHistory missing transcript: %v", err)
	}
	if history == nil || history.Thread.ThreadID != "missing-session" || len(history.Turns) != 0 {
		t.Fatalf("expected empty history record, got %#v", history)
	}
}

func TestResolveResumeSessionRejectsCrossWorkspaceClaudeThread(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceA := filepath.Join(t.TempDir(), "ws-a")
	workspaceB := filepath.Join(t.TempDir(), "ws-b")
	expectedPath := writeClaudeSessionFile(t, configDir, workspaceA, "session-resume", time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceA, "session_id": "session-resume"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "resume me"}},
	})

	filePath, meta, err := resolveResumeSession(workspaceA, "session-resume")
	if err != nil {
		t.Fatalf("resolveResumeSession same workspace: %v", err)
	}
	if filePath != expectedPath || meta == nil || meta.CWD != workspaceA {
		t.Fatalf("unexpected same-workspace resume resolution: path=%q meta=%#v", filePath, meta)
	}

	_, crossMeta, err := resolveResumeSession(workspaceB, "session-resume")
	if err == nil {
		t.Fatal("expected cross-workspace Claude resume to be rejected")
	}
	if crossMeta == nil || crossMeta.CWD != workspaceA {
		t.Fatalf("expected cross-workspace rejection to retain source metadata, got %#v", crossMeta)
	}
}

func TestReadThreadHistoryProjectsClaudeEditAsFileChange(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceRoot := filepath.Join(t.TempDir(), "ws-file-change")
	writeClaudeSessionFile(t, configDir, workspaceRoot, "session-file-change", time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "timestamp": "2026-04-28T11:00:00Z", "cwd": workspaceRoot, "session_id": "session-file-change", "model": "mimo-v2.5-pro", "permissionMode": "default"},
		{"type": "user", "timestamp": "2026-04-28T11:01:00Z", "promptId": "prompt-1", "message": map[string]any{"role": "user", "content": "请修改文件"}},
		{"type": "assistant", "timestamp": "2026-04-28T11:01:05Z", "promptId": "prompt-1", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{
				"type": "tool_use",
				"id":   "tool-edit-1",
				"name": "Edit",
				"input": map[string]any{
					"file_path":  "internal/app/app.go",
					"old_string": "old line",
					"new_string": "new line",
				},
			},
		}}},
		{"type": "user", "timestamp": "2026-04-28T11:01:08Z", "promptId": "prompt-1", "message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "tool-edit-1", "content": "updated"},
		}}},
	})

	history, err := readThreadHistory(workspaceRoot, "session-file-change", RuntimeStateSnapshot{})
	if err != nil {
		t.Fatalf("readThreadHistory: %v", err)
	}
	if history == nil || len(history.Turns) != 1 {
		t.Fatalf("expected one history turn, got %#v", history)
	}
	items := history.Turns[0].Items
	if len(items) != 2 {
		t.Fatalf("expected user + file_change items, got %#v", items)
	}
	if items[1].Kind != "file_change" || items[1].Text != "internal/app/app.go" {
		t.Fatalf("expected Claude Edit to render as file_change, got %#v", items[1])
	}
}
