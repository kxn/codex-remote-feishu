package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSurfaceResumeStoreRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	path := surfaceResumeStatePath(stateDir)
	store, err := loadSurfaceResumeStore(path)
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}

	updatedAt := time.Date(2026, 4, 9, 11, 0, 0, 0, time.UTC)
	if err := store.Put(SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "vscode",
		ResumeInstanceID:   "inst-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: " /data/dl/work/../droid/ ",
		ResumeRouteMode:    "follow_local",
		UpdatedAt:          updatedAt,
	}); err != nil {
		t.Fatalf("put surface resume entry: %v", err)
	}

	reloaded, err := loadSurfaceResumeStore(path)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	entry, ok := reloaded.Get("surface-1")
	if !ok {
		t.Fatal("expected surface resume entry after reload")
	}
	if entry.GatewayID != "app-1" || entry.ChatID != "chat-1" || entry.ActorUserID != "user-1" {
		t.Fatalf("unexpected routing fields: %#v", entry)
	}
	if entry.ProductMode != "vscode" || entry.ResumeInstanceID != "inst-1" || entry.ResumeThreadID != "thread-1" {
		t.Fatalf("unexpected resume target fields: %#v", entry)
	}
	if entry.ResumeWorkspaceKey != "/data/dl/droid" || entry.ResumeRouteMode != "follow_local" {
		t.Fatalf("unexpected normalized workspace or route: %#v", entry)
	}
	if !entry.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected updatedAt: %s", entry.UpdatedAt)
	}
}

func TestDaemonPersistsSurfaceModeAcrossRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil || entry.ProductMode != "vscode" {
		t.Fatalf("expected persisted vscode surface mode, got %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from resume state")
	}
	if snapshot.ProductMode != "vscode" {
		t.Fatalf("expected vscode mode after restart, got %#v", snapshot)
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected restarted surface to stay detached, got %#v", snapshot)
	}
	if restarted.service.SurfaceGatewayID("surface-1") != "app-1" || restarted.service.SurfaceChatID("surface-1") != "chat-1" || restarted.service.SurfaceActorUserID("surface-1") != "user-1" {
		t.Fatalf("unexpected restored routing: gateway=%q chat=%q actor=%q", restarted.service.SurfaceGatewayID("surface-1"), restarted.service.SurfaceChatID("surface-1"), restarted.service.SurfaceActorUserID("surface-1"))
	}
}

func TestDaemonMaterializesLatentSurfaceFromSurfaceResumeStateOnRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-visible-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
	})

	app := newRestoreHintTestApp(stateDir)
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from resume state")
	}
	if snapshot.ProductMode != "normal" {
		t.Fatalf("expected normal mode after restart, got %#v", snapshot)
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected restored surface to stay detached, got %#v", snapshot)
	}

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected resume entry after startup sync")
	}
	if entry.ResumeInstanceID != "inst-visible-1" || entry.ResumeThreadID != "thread-1" || entry.ResumeWorkspaceKey != "/data/dl/droid" || entry.ResumeRouteMode != "pinned" {
		t.Fatalf("expected startup materialization to preserve stored resume target, got %#v", entry)
	}
}

func TestDaemonAttachedVSCodeSurfacePersistsResumeTargetButRestartStaysDetached(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	app := newRestoreHintTestApp(stateDir)
	seedVSCodeResumeInstance(app, "inst-vscode-1", "thread-1")

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-vscode-1",
	})

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected persisted surface resume entry")
	}
	if entry.ProductMode != "vscode" || entry.ResumeInstanceID != "inst-vscode-1" || entry.ResumeThreadID != "thread-1" || entry.ResumeRouteMode != "follow_local" {
		t.Fatalf("unexpected persisted vscode resume target: %#v", entry)
	}

	restarted := newRestoreHintTestApp(stateDir)
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface after restart")
	}
	if snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detached vscode snapshot after restart, got %#v", snapshot)
	}
	reloaded := restarted.SurfaceResumeState("surface-1")
	if reloaded == nil || !sameSurfaceResumeEntryContent(*entry, *reloaded) {
		t.Fatalf("expected restart to preserve stored resume target: want=%#v got=%#v", entry, reloaded)
	}
}

func seedVSCodeResumeInstance(app *App, instanceID, threadID string) {
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:              instanceID,
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: threadID,
		Threads: map[string]*state.ThreadRecord{
			threadID: {ThreadID: threadID, Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
}

func putSurfaceResumeStateForTest(t *testing.T, stateDir string, entry SurfaceResumeEntry) {
	t.Helper()
	store, err := loadSurfaceResumeStore(surfaceResumeStatePath(stateDir))
	if err != nil {
		t.Fatalf("load surface resume store: %v", err)
	}
	if err := store.Put(entry); err != nil {
		t.Fatalf("put surface resume entry: %v", err)
	}
}
