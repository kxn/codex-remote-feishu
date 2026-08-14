package codexstate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTurnPatchStoragePreviewLatestAssistantTurn(t *testing.T) {
	storage, rolloutPath, originalRaw := newTurnPatchStorageFixture(t, buildLatestTurnPatchRollout("thread-1"))

	preview, err := storage.PreviewLatestAssistantTurn("thread-1")
	if err != nil {
		t.Fatalf("preview latest turn: %v", err)
	}
	if preview.ThreadID != "thread-1" || preview.TurnID != "turn-2" || preview.RolloutPath != rolloutPath {
		t.Fatalf("unexpected preview identity: %#v", preview)
	}
	if preview.RolloutDigest != sha256Hex(originalRaw) {
		t.Fatalf("unexpected preview digest: %s", preview.RolloutDigest)
	}
	if preview.ReasoningLineCount != 2 {
		t.Fatalf("expected 2 reasoning lines, got %#v", preview)
	}
	if len(preview.Messages) != 3 {
		t.Fatalf("expected 3 assistant messages, got %#v", preview.Messages)
	}
	if preview.Messages[0].MessageKey != "msg-1" || preview.Messages[0].Text != "first blocked message" || preview.Messages[0].Phase != "commentary" {
		t.Fatalf("unexpected first message: %#v", preview.Messages[0])
	}
	if !preview.Messages[2].IsFinal || preview.Messages[2].Text != "final blocked message" {
		t.Fatalf("unexpected final message: %#v", preview.Messages[2])
	}
}

func TestTurnPatchStorageApplyAndRollbackLatestTurnPatch(t *testing.T) {
	storage, rolloutPath, originalRaw := newTurnPatchStorageFixture(t, buildLatestTurnPatchRollout("thread-1"))

	preview, err := storage.PreviewLatestAssistantTurn("thread-1")
	if err != nil {
		t.Fatalf("preview latest turn: %v", err)
	}
	result, err := storage.ApplyLatestTurnPatch(ApplyLatestTurnPatchRequest{
		ThreadID:              "thread-1",
		ExpectedTurnID:        preview.TurnID,
		ExpectedRolloutDigest: preview.RolloutDigest,
		ActorUserID:           "user-1",
		SurfaceSessionID:      "surface-1",
		Replacements: []TurnPatchReplacement{
			{MessageKey: "msg-1", NewText: "patched commentary"},
			{MessageKey: "msg-3", NewText: "patched final"},
		},
	})
	if err != nil {
		t.Fatalf("apply latest turn patch: %v", err)
	}
	if result.ReplacedMessageCount != 2 || result.RemovedReasoningLine != 2 {
		t.Fatalf("unexpected apply result: %#v", result)
	}
	backupRaw, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupRaw) != string(originalRaw) {
		t.Fatalf("backup contents drifted")
	}
	updatedRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read updated rollout: %v", err)
	}
	updated := string(updatedRaw)
	if strings.Contains(updated, "\"type\":\"reasoning\"") || strings.Contains(updated, "\"type\":\"agent_reasoning\"") {
		t.Fatalf("expected reasoning lines removed, got %s", updated)
	}
	if !strings.Contains(updated, "patched commentary") || !strings.Contains(updated, "patched final") || !strings.Contains(updated, "\"last_agent_message\":\"patched final\"") {
		t.Fatalf("expected patched content, got %s", updated)
	}
	if !strings.Contains(updated, "second normal message") {
		t.Fatalf("expected untouched middle message, got %s", updated)
	}

	rolledBack, err := storage.RollbackLatestTurnPatch(RollbackLatestTurnPatchRequest{
		ThreadID:    "thread-1",
		PatchID:     result.PatchID,
		ActorUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("rollback latest turn patch: %v", err)
	}
	if rolledBack.PatchID != result.PatchID {
		t.Fatalf("unexpected rollback result: %#v", rolledBack)
	}
	restoredRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read restored rollout: %v", err)
	}
	if string(restoredRaw) != string(originalRaw) {
		t.Fatalf("expected original rollout restored")
	}
}

func TestTurnPatchRolloutPathIdentityUsesPlatformCanonicalComparison(t *testing.T) {
	if !sameTurnPatchRolloutPath(`\\?\C:\Users\Codex\sessions\rollout.jsonl`, `c:/users/codex/sessions/rollout.jsonl`) {
		t.Fatal("expected Windows-like rollout paths to compare by canonical identity")
	}
	if sameTurnPatchRolloutPath("", `c:/users/codex/sessions/rollout.jsonl`) {
		t.Fatal("did not expect empty rollout path to match a real path")
	}
}

func TestTurnPatchStorageApplyRejectsDigestMismatch(t *testing.T) {
	storage, _, _ := newTurnPatchStorageFixture(t, buildLatestTurnPatchRollout("thread-1"))

	_, err := storage.ApplyLatestTurnPatch(ApplyLatestTurnPatchRequest{
		ThreadID:              "thread-1",
		ExpectedTurnID:        "turn-2",
		ExpectedRolloutDigest: "deadbeef",
		Replacements: []TurnPatchReplacement{
			{MessageKey: "msg-1", NewText: "patched commentary"},
		},
	})
	if !errors.Is(err, ErrTurnPatchRolloutDigestMismatch) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestTurnPatchStoragePreviewRejectsDuplicateDrift(t *testing.T) {
	storage, _, _ := newTurnPatchStorageFixture(t, buildDuplicateDriftRollout("thread-1"))

	_, err := storage.PreviewLatestAssistantTurn("thread-1")
	if !errors.Is(err, ErrTurnPatchDuplicateDrift) {
		t.Fatalf("expected duplicate drift, got %v", err)
	}
}

func TestTurnPatchStorageRollbackRejectsWhenRolloutChangedAfterPatch(t *testing.T) {
	storage, rolloutPath, _ := newTurnPatchStorageFixture(t, buildLatestTurnPatchRollout("thread-1"))

	preview, err := storage.PreviewLatestAssistantTurn("thread-1")
	if err != nil {
		t.Fatalf("preview latest turn: %v", err)
	}
	result, err := storage.ApplyLatestTurnPatch(ApplyLatestTurnPatchRequest{
		ThreadID:              "thread-1",
		ExpectedTurnID:        preview.TurnID,
		ExpectedRolloutDigest: preview.RolloutDigest,
		Replacements: []TurnPatchReplacement{
			{MessageKey: "msg-1", NewText: "patched commentary"},
		},
	})
	if err != nil {
		t.Fatalf("apply latest turn patch: %v", err)
	}
	currentRaw, err := os.ReadFile(rolloutPath)
	if err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	driftedRaw := append([]byte{}, currentRaw...)
	driftedRaw = append(driftedRaw, mustMarshalJSONLine(t, taskStartedLine("turn-3"))...)
	driftedRaw = append(driftedRaw, '\n')
	if err := os.WriteFile(rolloutPath, driftedRaw, 0o644); err != nil {
		t.Fatalf("write drifted rollout: %v", err)
	}
	_, err = storage.RollbackLatestTurnPatch(RollbackLatestTurnPatchRequest{
		ThreadID: "thread-1",
		PatchID:  result.PatchID,
	})
	if !errors.Is(err, ErrTurnPatchRollbackDrift) {
		t.Fatalf("expected rollback drift, got %v", err)
	}
}

func TestTurnPatchStorageResolveThreadTargetFallsBackFromSQLiteDrift(t *testing.T) {
	baseDir := t.TempDir()
	sessionsRoot := filepath.Join(baseDir, "sessions")
	validPath, _, err := writeRolloutFixture(sessionsRoot, "rollout-valid-thread-1.jsonl", buildLatestTurnPatchRollout("thread-1"))
	if err != nil {
		t.Fatalf("write valid rollout: %v", err)
	}
	driftPath, _, err := writeRolloutFixture(sessionsRoot, "rollout-drift-thread-1.jsonl", buildLatestTurnPatchRollout("other-thread"))
	if err != nil {
		t.Fatalf("write drifted rollout: %v", err)
	}
	sqlitePath := filepath.Join(baseDir, "state_5.sqlite")
	createTurnPatchCatalogDB(t, sqlitePath, "thread-1", driftPath)
	catalog := NewSQLiteThreadCatalog(sqlitePath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	storage, err := NewTurnPatchStorage(TurnPatchStorageOptions{
		SQLiteCatalog: catalog,
		SessionsRoot:  sessionsRoot,
		PatchStateDir: filepath.Join(baseDir, "patch-state"),
		Logf:          func(string, ...any) {},
	})
	if err != nil {
		t.Fatalf("new turn patch storage: %v", err)
	}
	target, err := storage.ResolveThreadTarget("thread-1")
	if err != nil {
		t.Fatalf("resolve thread target: %v", err)
	}
	if target.RolloutPath != validPath {
		t.Fatalf("expected fallback rollout path %s, got %#v", validPath, target)
	}
}

func newTurnPatchStorageFixture(t *testing.T, lines []map[string]any) (*TurnPatchStorage, string, []byte) {
	t.Helper()
	baseDir := t.TempDir()
	sessionsRoot := filepath.Join(baseDir, "sessions")
	rolloutPath, raw, err := writeRolloutFixture(sessionsRoot, "rollout-thread-1.jsonl", lines)
	if err != nil {
		t.Fatalf("write rollout fixture: %v", err)
	}
	storage, err := NewTurnPatchStorage(TurnPatchStorageOptions{
		SessionsRoot:  sessionsRoot,
		PatchStateDir: filepath.Join(baseDir, "patch-state"),
		Logf:          func(string, ...any) {},
		Now: func() time.Time {
			return time.Date(2026, 4, 26, 4, 0, 0, 123456000, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("new turn patch storage: %v", err)
	}
	return storage, rolloutPath, raw
}

func writeRolloutFixture(root, name string, lines []map[string]any) (string, []byte, error) {
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", nil, err
	}
	var raw []byte
	for index, line := range lines {
		raw = append(raw, mustMarshalJSONLine(nil, line)...)
		if index+1 < len(lines) {
			raw = append(raw, '\n')
		}
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", nil, err
	}
	return path, raw, nil
}

func buildLatestTurnPatchRollout(threadID string) []map[string]any {
	return []map[string]any{
		sessionMetaLine(threadID),
		taskStartedLine("turn-1"),
		agentMessageLine("older turn", "final_answer"),
		assistantResponseLine("older turn", "final_answer"),
		taskCompleteLine("turn-1", "older turn"),
		taskStartedLine("turn-2"),
		agentReasoningLine("hidden reasoning"),
		responseReasoningLine("hidden reasoning"),
		agentMessageLine("first blocked message", "commentary"),
		assistantResponseLine("first blocked message", "commentary"),
		agentMessageLine("second normal message", "commentary"),
		assistantResponseLine("second normal message", "commentary"),
		agentMessageLine("final blocked message", "final_answer"),
		assistantResponseLine("final blocked message", "final_answer"),
		taskCompleteLine("turn-2", "final blocked message"),
	}
}

func buildDuplicateDriftRollout(threadID string) []map[string]any {
	return []map[string]any{
		sessionMetaLine(threadID),
		taskStartedLine("turn-1"),
		agentMessageLine("need patch", "final_answer"),
		assistantResponseLine("mismatched duplicate", "final_answer"),
		taskCompleteLine("turn-1", "need patch"),
	}
}

func sessionMetaLine(threadID string) map[string]any {
	return map[string]any{
		"type": "session_meta",
		"payload": map[string]any{
			"id":  threadID,
			"cwd": "/tmp/project",
		},
	}
}

func taskStartedLine(turnID string) map[string]any {
	return map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type":    "task_started",
			"turn_id": turnID,
		},
	}
}

func taskCompleteLine(turnID, lastMessage string) map[string]any {
	return map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type":               "task_complete",
			"turn_id":            turnID,
			"last_agent_message": lastMessage,
		},
	}
}

func agentReasoningLine(text string) map[string]any {
	return map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type": "agent_reasoning",
			"text": text,
		},
	}
}

func responseReasoningLine(text string) map[string]any {
	return map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":    "reasoning",
			"summary": []map[string]any{{"type": "summary_text", "text": text}},
		},
	}
}

func agentMessageLine(text, phase string) map[string]any {
	return map[string]any{
		"type": "event_msg",
		"payload": map[string]any{
			"type":    "agent_message",
			"message": text,
			"phase":   phase,
		},
	}
}

func assistantResponseLine(text, phase string) map[string]any {
	return map[string]any{
		"type": "response_item",
		"payload": map[string]any{
			"type":  "message",
			"role":  "assistant",
			"phase": phase,
			"content": []map[string]any{
				{"type": "output_text", "text": text},
			},
		},
	}
}

func mustMarshalJSONLine(t *testing.T, value any) []byte {
	if t != nil {
		t.Helper()
	}
	raw, err := json.Marshal(value)
	if err != nil {
		if t != nil {
			t.Fatalf("marshal json line: %v", err)
		}
		panic(err)
	}
	return raw
}

func createTurnPatchCatalogDB(t *testing.T, dbPath, threadID, rolloutPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	rollout_path TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	source TEXT NOT NULL,
	model_provider TEXT NOT NULL,
	cwd TEXT NOT NULL,
	title TEXT NOT NULL,
	sandbox_policy TEXT NOT NULL,
	approval_mode TEXT NOT NULL,
	tokens_used INTEGER NOT NULL DEFAULT 0,
	has_user_event INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	archived_at INTEGER,
	git_sha TEXT,
	git_branch TEXT,
	git_origin_url TEXT,
	cli_version TEXT NOT NULL DEFAULT '',
	first_user_message TEXT NOT NULL DEFAULT '',
	agent_nickname TEXT,
	agent_role TEXT,
	memory_mode TEXT NOT NULL DEFAULT 'enabled',
	model TEXT,
	reasoning_effort TEXT,
	agent_path TEXT
)`); err != nil {
		t.Fatalf("create threads table: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO threads (
	id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy,
	approval_mode, tokens_used, has_user_event, archived, cli_version, first_user_message, memory_mode
) VALUES (?, ?, 0, 1775710200, 'cli', 'openai', '/tmp/project', 'thread', 'workspace-write', 'never', 0, 0, 0, '', 'hello', 'enabled')
`, threadID, rolloutPath); err != nil {
		t.Fatalf("insert thread row: %v", err)
	}
}
