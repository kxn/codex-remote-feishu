package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListSessionThreadsFiltersWorkspaceAndIncludeAll(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	workspaceA := filepath.Join(t.TempDir(), "ws-a")
	workspaceB := filepath.Join(t.TempDir(), "ws-b")

	writeClaudeSessionFile(t, configDir, workspaceA, "session-a1", time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceA, "session_id": "session-a1", "model": "mimo-v2.5-pro", "permissionMode": "default"},
		{"type": "session-title", "title": "Workspace A title"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Workspace A prompt"}},
	})
	writeClaudeSessionFile(t, configDir, workspaceA, "shared-session", time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceA, "session_id": "shared-session"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Older shared prompt"}},
	})
	writeClaudeSessionFile(t, configDir, workspaceB, "shared-session", time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceB, "session_id": "shared-session", "permissionMode": "plan"},
		{"type": "session-title", "title": "Workspace B shared"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Newer shared prompt"}},
	})
	writeClaudeSessionFile(t, configDir, workspaceB, "session-b1", time.Date(2026, 4, 28, 9, 0, 0, 0, time.UTC), []map[string]any{
		{"type": "system", "cwd": workspaceB, "session_id": "session-b1"},
		{"type": "user", "message": map[string]any{"role": "user", "content": "Workspace B prompt"}},
	})

	localOnly, err := listSessionThreads(workspaceA, false, RuntimeStateSnapshot{})
	if err != nil {
		t.Fatalf("listSessionThreads local only: %v", err)
	}
	if len(localOnly) != 2 {
		t.Fatalf("expected 2 workspace-local sessions, got %#v", localOnly)
	}
	if localOnly[0].ThreadID != "shared-session" || localOnly[0].CWD != workspaceA {
		t.Fatalf("expected workspace A shared session first, got %#v", localOnly[0])
	}
	if localOnly[1].ThreadID != "session-a1" {
		t.Fatalf("expected session-a1 second, got %#v", localOnly[1])
	}

	includeAll, err := listSessionThreads(workspaceA, true, RuntimeStateSnapshot{})
	if err != nil {
		t.Fatalf("listSessionThreads includeAll: %v", err)
	}
	if len(includeAll) != 3 {
		t.Fatalf("expected deduped cross-workspace sessions, got %#v", includeAll)
	}
	if includeAll[0].ThreadID != "shared-session" || includeAll[0].CWD != workspaceB || includeAll[0].PlanMode != "on" {
		t.Fatalf("expected newer shared session to win dedupe, got %#v", includeAll[0])
	}
	if includeAll[1].ThreadID != "session-a1" || includeAll[2].ThreadID != "session-b1" {
		t.Fatalf("unexpected includeAll ordering: %#v", includeAll)
	}
}

func writeClaudeSessionFile(t *testing.T, configDir, workspaceRoot, sessionID string, modTime time.Time, entries []map[string]any) string {
	t.Helper()
	projectDir := filepath.Join(configDir, "projects", SanitizeProjectDirName(workspaceRoot))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project dir: %v", err)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	content := make([]byte, 0, len(entries)*64)
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal transcript line: %v", err)
		}
		content = append(content, line...)
		content = append(content, '\n')
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.Chtimes(filePath, modTime, modTime); err != nil {
		t.Fatalf("chtimes transcript: %v", err)
	}
	return filePath
}
