package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestHeadlessRestoreHintStoreRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := headlessRestoreHintsStatePath(stateDir)
	store, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}

	updatedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	if err := store.Put(HeadlessRestoreHint{
		SurfaceSessionID: "feishu:app-1:chat:chat-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
		ThreadTitle:      "修复登录流程",
		ThreadCWD:        "/data/dl/droid",
		UpdatedAt:        updatedAt,
	}); err != nil {
		t.Fatalf("put hint: %v", err)
	}

	reloaded, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	hint, ok := reloaded.Get("feishu:app-1:chat:chat-1")
	if !ok {
		t.Fatal("expected restore hint after reload")
	}
	if hint.GatewayID != "app-1" || hint.ChatID != "chat-1" || hint.ActorUserID != "user-1" {
		t.Fatalf("unexpected restored routing fields: %#v", hint)
	}
	if hint.ThreadID != "thread-1" || hint.ThreadTitle != "修复登录流程" || hint.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("unexpected restored thread fields: %#v", hint)
	}
	if !hint.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected updatedAt: %s", hint.UpdatedAt)
	}
}

func TestHeadlessRestoreHintStoreDeletePersists(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := headlessRestoreHintsStatePath(stateDir)
	store, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	if err := store.Put(HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
		UpdatedAt:        time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("put hint: %v", err)
	}
	if err := store.Delete("surface-1"); err != nil {
		t.Fatalf("delete hint: %v", err)
	}

	reloaded, err := loadHeadlessRestoreHintStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := reloaded.Get("surface-1"); ok {
		t.Fatalf("expected restore hint to be deleted, got %#v", reloaded.Entries())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty store file")
	}
}

func TestHeadlessRestoreHintsStatePath(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join("/tmp", "codex-remote-state")
	if got := headlessRestoreHintsStatePath(stateDir); got != filepath.Join(stateDir, headlessRestoreHintsStateFile) {
		t.Fatalf("unexpected state path: %s", got)
	}
	if got := headlessRestoreHintsStatePath(""); got != "" {
		t.Fatalf("expected empty state path, got %q", got)
	}
}

func TestDaemonPersistsHeadlessRestoreHintAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})

	hint := app.HeadlessRestoreHint("surface-1")
	if hint == nil {
		t.Fatal("expected restore hint after headless attach")
	}
	if hint.GatewayID != "app-1" || hint.ChatID != "chat-1" || hint.ActorUserID != "user-1" {
		t.Fatalf("unexpected restore hint routing: %#v", hint)
	}
	if hint.ThreadID != "thread-1" || hint.ThreadTitle == "" || hint.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("unexpected restore hint payload: %#v", hint)
	}

	restarted := newRestoreHintTestApp(stateDir)
	reloaded := restarted.HeadlessRestoreHint("surface-1")
	if reloaded == nil {
		t.Fatal("expected restore hint after restart")
	}
	if !sameHeadlessRestoreHintContent(*hint, *reloaded) {
		t.Fatalf("unexpected restore hint after restart: want=%#v got=%#v", hint, reloaded)
	}
}

func TestDaemonClearsHeadlessRestoreHintOnDetach(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})

	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to clear on detach, got %#v", hint)
	}
	restarted := newRestoreHintTestApp(stateDir)
	if hint := restarted.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to stay cleared after restart, got %#v", hint)
	}
}

func TestDaemonClearsHeadlessRestoreHintWhenSwitchingToVSCode(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Source:        "vscode",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "VS Code 会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})
	if hint := app.HeadlessRestoreHint("surface-1"); hint == nil {
		t.Fatal("expected initial restore hint after headless attach")
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to clear after switching to vscode, got %#v", hint)
	}
}

func TestDaemonKeepsHeadlessRestoreHintOnDisconnect(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})
	before := app.HeadlessRestoreHint("surface-1")
	if before == nil {
		t.Fatal("expected restore hint before disconnect")
	}

	app.onDisconnect(context.Background(), "inst-headless-1")

	after := app.HeadlessRestoreHint("surface-1")
	if after == nil {
		t.Fatal("expected restore hint to survive disconnect")
	}
	if !sameHeadlessRestoreHintContent(*before, *after) {
		t.Fatalf("unexpected restore hint after disconnect: want=%#v got=%#v", before, after)
	}
}

func newRestoreHintTestApp(stateDir string) *App {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	return app
}

func seedHeadlessInstance(app *App, instanceID, threadID string) {
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              instanceID,
		DisplayName:             "headless",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "headless",
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: threadID,
		Threads: map[string]*state.ThreadRecord{
			threadID: {ThreadID: threadID, Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
}
