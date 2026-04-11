package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestDaemonDerivesHeadlessRestoreHintFromSurfaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeThreadID:     "thread-1",
		ResumeThreadTitle:  "修复登录流程",
		ResumeThreadCWD:    "/data/dl/droid",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
	})

	app := newRestoreHintTestApp(stateDir)
	hint := app.HeadlessRestoreHint("surface-1")
	if hint == nil {
		t.Fatal("expected derived headless restore hint from surface resume state")
	}
	if hint.ThreadID != "thread-1" || hint.ThreadTitle != "修复登录流程" || hint.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("unexpected derived restore hint: %#v", hint)
	}
	if len(app.headlessRestoreState) != 1 {
		t.Fatalf("expected in-memory headless restore state derived from surface resume state, got %#v", app.headlessRestoreState)
	}
	if _, err := os.Stat(headlessRestoreHintsStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected compatible startup to remove migrated legacy hint file, stat err=%v", err)
	}
}

func TestDaemonMigratesLegacyHeadlessRestoreHintIntoSurfaceResumeState(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
		ThreadTitle:      "修复登录流程",
		ThreadCWD:        "/data/dl/droid",
	})

	app := newRestoreHintTestApp(stateDir)
	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected legacy restore hint to migrate into surface resume state")
	}
	if entry.ProductMode != "normal" || entry.ResumeThreadID != "thread-1" || entry.ResumeThreadTitle != "修复登录流程" {
		t.Fatalf("unexpected migrated surface resume entry: %#v", entry)
	}
	if entry.ResumeThreadCWD != "/data/dl/droid" || entry.ResumeWorkspaceKey != "/data/dl/droid" || !entry.ResumeHeadless {
		t.Fatalf("expected migrated headless metadata in surface resume entry, got %#v", entry)
	}
	if _, err := os.Stat(headlessRestoreHintsStatePath(stateDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected migrated legacy hint file to be removed, stat err=%v", err)
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

func TestDaemonModeSwitchToVSCodeClearsHeadlessRestoreState(t *testing.T) {
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
	if hint := app.HeadlessRestoreHint("surface-1"); hint == nil {
		t.Fatal("expected restore hint after headless attach")
	}
	if len(app.headlessRestoreState) != 1 {
		t.Fatalf("expected one in-memory restore entry before mode switch, got %#v", app.headlessRestoreState)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})

	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to clear after /mode vscode, got %#v", hint)
	}
	if len(app.headlessRestoreState) != 0 {
		t.Fatalf("expected in-memory restore state to clear after /mode vscode, got %#v", app.headlessRestoreState)
	}
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detached vscode snapshot after mode switch, got %#v", snapshot)
	}

	restarted := newRestoreHintTestApp(stateDir)
	if hint := restarted.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to stay cleared after restart, got %#v", hint)
	}
}

func TestDaemonModeSwitchToVSCodeStaysDetachedAfterNormalAutoRestore(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
		ThreadTitle:      "修复登录流程",
		ThreadCWD:        "/data/dl/droid",
	})

	app := newRestoreHintTestApp(stateDir)
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}

	base := time.Date(2026, 4, 9, 14, 0, 0, 0, time.UTC)
	app.onTick(context.Background(), base)
	pending := app.service.SurfaceSnapshot("surface-1").PendingHeadless
	if pending.InstanceID == "" {
		t.Fatalf("expected pending headless after auto-restore tick, got %#v", pending)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    pending.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected normal auto-restore to attach headless thread, got %#v", snapshot)
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionModeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode vscode",
	})
	app.onTick(context.Background(), base.Add(time.Second))

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected vscode mode switch to stay detached after prior auto-restore, got %#v", snapshot)
	}
	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected restore hint to clear after /mode vscode from auto-restored state, got %#v", hint)
	}
	if len(app.headlessRestoreState) != 0 {
		t.Fatalf("expected in-memory restore state to clear after /mode vscode from auto-restored state, got %#v", app.headlessRestoreState)
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

func TestDaemonMaterializesLatentSurfaceFromRestoreHintOnRestart(t *testing.T) {
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

	restarted := newRestoreHintTestApp(stateDir)
	snapshot := restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface to materialize from restore hint")
	}
	if snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected materialized restore surface to stay detached, got %#v", snapshot)
	}
	if restarted.service.SurfaceGatewayID("surface-1") != "app-1" || restarted.service.SurfaceChatID("surface-1") != "chat-1" || restarted.service.SurfaceActorUserID("surface-1") != "user-1" {
		t.Fatalf("unexpected materialized surface routing: gateway=%q chat=%q actor=%q", restarted.service.SurfaceGatewayID("surface-1"), restarted.service.SurfaceChatID("surface-1"), restarted.service.SurfaceActorUserID("surface-1"))
	}
	if len(restarted.headlessRestoreState) != 1 {
		t.Fatalf("expected one recovery state entry after restart, got %#v", restarted.headlessRestoreState)
	}
}

func TestDaemonAutoRestoreReconnectsWithRecoveryNoticeOnly(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	seedHeadlessInstance(app, "inst-headless-1", "thread-1")
	app.service.Instance("inst-headless-1").Threads["thread-1"].UndeliveredReplay = &state.ThreadReplayRecord{
		Kind:           state.ThreadReplayNotice,
		NoticeCode:     "problem_saved",
		NoticeTitle:    "问题提示",
		NoticeText:     "等待 headless 接手的 notice",
		NoticeThemeKey: "warning",
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-1",
	})
	gateway.operations = nil
	if hint := app.HeadlessRestoreHint("surface-1"); hint == nil || hint.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected persisted restore hint with cwd before disconnect, got %#v", hint)
	}
	if len(app.headlessRestoreState) != 1 {
		t.Fatalf("expected one in-memory restore state before disconnect, got %#v", app.headlessRestoreState)
	}

	app.onDisconnect(context.Background(), "inst-headless-1")
	gateway.operations = nil
	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected surface to detach after disconnect while keeping restore state, got %#v", snapshot)
	}

	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}
	base := time.Date(2026, 4, 8, 3, 20, 0, 0, time.UTC)
	app.onTick(context.Background(), base)
	if len(gateway.operations) != 0 {
		t.Fatalf("expected auto-restore headless start to stay silent, got %#v", gateway.operations)
	}
	pending := app.service.SurfaceSnapshot("surface-1").PendingHeadless
	if pending.InstanceID == "" {
		t.Fatalf("expected auto-restore pending headless after tick, got %#v", pending)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    pending.InstanceID,
			DisplayName:   "headless",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	snapshot = app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected recovery hello to attach restored thread, got %#v", snapshot)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected exactly one recovery notice, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardTitle, "恢复") || !strings.Contains(gateway.operations[0].CardBody, "重连成功，已恢复到之前会话") {
		t.Fatalf("expected recovery success notice, got %#v", gateway.operations[0])
	}
	if strings.Contains(gateway.operations[0].CardBody, "等待 headless 接手的 notice") || strings.Contains(gateway.operations[0].CardBody, "当前输入目标已切换到") {
		t.Fatalf("expected no stale replay or selection card on recovery, got %#v", gateway.operations[0])
	}
	if replay := app.service.Instance("inst-headless-1").Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected stale replay to be drained after recovery, got %#v", replay)
	}
	if replay := app.service.Instance(pending.InstanceID).Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected restored headless thread replay to stay empty, got %#v", replay)
	}
}

func TestDaemonAutoRestoreWaitsForFirstRefreshBeforeMissingNotice(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-missing",
		ThreadTitle:      "旧会话",
	})
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

	base := time.Date(2026, 4, 8, 3, 30, 0, 0, time.UTC)
	app.onTick(context.Background(), base)
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no recovery notice before first refresh round, got %#v", gateway.operations)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no recovery notice while first refresh is still pending, got %#v", gateway.operations)
	}

	app.onTick(context.Background(), base.Add(2*time.Second))
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no retry notice before first refresh snapshot, got %#v", gateway.operations)
	}

	app.onEvents(context.Background(), "inst-vscode-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{ThreadID: "thread-other", Name: "其他会话", CWD: "/data/dl/droid", Loaded: true}},
	}})
	if len(gateway.operations) != 1 {
		t.Fatalf("expected single missing-thread notice after first refresh settles, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardBody, "暂时无法找到之前会话") {
		t.Fatalf("expected missing-thread recovery notice, got %#v", gateway.operations[0])
	}

	app.onTick(context.Background(), base.Add(5*time.Second))
	if len(gateway.operations) != 1 {
		t.Fatalf("expected backoff to suppress repeated recovery notice, got %#v", gateway.operations)
	}
}

func TestDaemonAutoRestoreLaunchFailureUsesBackoff(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
		ThreadTitle:      "修复登录流程",
		ThreadCWD:        "/data/dl/droid",
	})
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 0, errors.New("spawn failed")
	}

	base := time.Date(2026, 4, 8, 3, 40, 0, 0, time.UTC)
	app.onTick(context.Background(), base)
	if len(gateway.operations) != 1 {
		t.Fatalf("expected one launch failure notice, got %#v", gateway.operations)
	}
	if !strings.Contains(gateway.operations[0].CardBody, "暂时无法恢复") {
		t.Fatalf("expected recovery launch failure notice, got %#v", gateway.operations[0])
	}

	app.onTick(context.Background(), base.Add(5*time.Second))
	if len(gateway.operations) != 1 {
		t.Fatalf("expected launch failure backoff to suppress retry noise, got %#v", gateway.operations)
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

func putRestoreHintForTest(t *testing.T, stateDir string, hint HeadlessRestoreHint) {
	t.Helper()
	store, err := loadHeadlessRestoreHintStore(headlessRestoreHintsStatePath(stateDir))
	if err != nil {
		t.Fatalf("load restore hint store: %v", err)
	}
	if err := store.Put(hint); err != nil {
		t.Fatalf("put restore hint: %v", err)
	}
}
