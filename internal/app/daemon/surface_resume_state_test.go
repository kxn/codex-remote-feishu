package daemon

import (
	"context"
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
		ResumeThreadTitle:  " 修复登录流程 ",
		ResumeThreadCWD:    " /data/dl/work/../droid/ ",
		ResumeWorkspaceKey: " /data/dl/work/../droid/ ",
		ResumeRouteMode:    "follow_local",
		ResumeHeadless:     true,
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
	if entry.ResumeThreadTitle != "修复登录流程" || entry.ResumeThreadCWD != "/data/dl/droid" || !entry.ResumeHeadless {
		t.Fatalf("unexpected normalized headless metadata: %#v", entry)
	}
	if entry.ResumeWorkspaceKey != "/data/dl/droid" || entry.ResumeRouteMode != "follow_local" {
		t.Fatalf("unexpected normalized workspace or route: %#v", entry)
	}
	if !entry.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected updatedAt: %s", entry.UpdatedAt)
	}
}

func TestDaemonHeadlessAttachPersistsResumeMetadataIntoSurfaceResumeState(t *testing.T) {
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

	entry := app.SurfaceResumeState("surface-1")
	if entry == nil {
		t.Fatal("expected surface resume entry after headless attach")
	}
	if entry.ResumeInstanceID != "inst-headless-1" || entry.ResumeThreadID != "thread-1" {
		t.Fatalf("unexpected persisted headless resume target: %#v", entry)
	}
	if !strings.Contains(entry.ResumeThreadTitle, "修复登录流程") || entry.ResumeThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected persisted headless thread metadata, got %#v", entry)
	}
	if !entry.ResumeHeadless {
		t.Fatalf("expected persisted resume entry to mark headless recovery, got %#v", entry)
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
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-missing",
		ThreadTitle:      "旧会话",
		ThreadCWD:        "/data/dl/droid",
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

func TestDaemonAttachedVSCodeSurfacePersistsResumeTargetAndRecoversOnReconnect(t *testing.T) {
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

	gateway := &recordingGateway{}
	restarted := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	restarted.SetHeadlessRuntime(HeadlessRuntimeConfig{
		IdleTTL:    time.Hour,
		KillGrace:  time.Second,
		Paths:      relayruntime.Paths{StateDir: stateDir},
		BinaryPath: "codex",
	})
	restarted.sendAgentCommand = func(string, agentproto.Command) error { return nil }
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

	restarted.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-vscode-1",
			DisplayName:   "droid",
			WorkspaceRoot: "/data/dl/droid",
			WorkspaceKey:  "/data/dl/droid",
			ShortName:     "droid",
			Source:        "vscode",
		},
	})

	snapshot = restarted.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "inst-vscode-1" {
		t.Fatalf("expected vscode resume to reattach target instance, got %#v", snapshot)
	}
	if snapshot.Attachment.SelectedThreadID != "" || snapshot.Attachment.RouteMode != "follow_local" {
		t.Fatalf("expected vscode resume to re-enter follow waiting, got %#v", snapshot)
	}
	sawResumeNotice := false
	for _, operation := range gateway.operations {
		if strings.Contains(operation.CardBody, "已恢复到 VS Code 实例") && strings.Contains(operation.CardBody, "再说一句话") {
			sawResumeNotice = true
			break
		}
	}
	if !sawResumeNotice {
		t.Fatalf("expected vscode resume guidance notice, got %#v", gateway.operations)
	}
}

func TestDaemonVSCodeResumeWaitsForExactInstanceAndNeverUsesHeadless(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "vscode",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "follow_local",
	})
	putRestoreHintForTest(t, stateDir, HeadlessRestoreHint{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
		ThreadTitle:      "旧 headless 会话",
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
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	headlessStarted := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		headlessStarted = true
		return 1, nil
	}

	app.onTick(context.Background(), time.Now().UTC())
	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected vscode resume path to clear stale headless hint, got %#v", hint)
	}
	if headlessStarted {
		t.Fatal("expected vscode resume path to avoid starting headless")
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].CardBody, "请先打开 VS Code 中的 Codex") {
		t.Fatalf("expected one-time open VS Code guidance while waiting for exact instance, got %#v", gateway.operations)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-other-1",
			DisplayName:   "other",
			WorkspaceRoot: "/data/dl/other",
			WorkspaceKey:  "/data/dl/other",
			ShortName:     "other",
			Source:        "vscode",
		},
	})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected vscode resume to wait for exact instance, got %#v", snapshot)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected open VS Code guidance to stay one-shot before exact instance reconnects, got %#v", gateway.operations)
	}
	if headlessStarted {
		t.Fatal("expected unrelated instance hello to keep headless disabled")
	}
}

func TestDaemonDetachedVSCodeModePromptsOpenVSCodeAfterRestart(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID: "surface-1",
		GatewayID:        "app-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ProductMode:      "vscode",
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

	app.onTick(context.Background(), time.Now().UTC())

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "vscode" || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected detached vscode surface after restart, got %#v", snapshot)
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].CardBody, "请先打开 VS Code 中的 Codex") {
		t.Fatalf("expected one-time open VS Code guidance for detached vscode surface, got %#v", gateway.operations)
	}

	app.onTick(context.Background(), time.Now().UTC().Add(time.Second))
	if len(gateway.operations) != 1 {
		t.Fatalf("expected detached vscode guidance to stay one-shot, got %#v", gateway.operations)
	}
}

func TestDaemonTickDoesNotRewriteSurfaceResumeStateWithoutStateChange(t *testing.T) {
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

	path := surfaceResumeStatePath(stateDir)
	base := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, base, base); err != nil {
		t.Fatalf("Chtimes(surface resume state): %v", err)
	}

	app.onTick(context.Background(), base.Add(time.Second))

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(surface resume state): %v", err)
	}
	if !info.ModTime().Equal(base) {
		t.Fatalf("expected idle tick to avoid rewriting surface resume state, modtime=%s want=%s", info.ModTime(), base)
	}
}

func TestDaemonNormalResumePrefersVisibleThreadOverHeadlessFallback(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-1",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
	})
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
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }

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
	app.onEvents(context.Background(), "inst-vscode-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-1",
			Name:     "修复登录流程",
			CWD:      "/data/dl/droid",
			Loaded:   true,
		}},
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-vscode-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected normal resume to reattach visible thread, got %#v", snapshot)
	}
	if snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected visible resume to avoid headless fallback, got %#v", snapshot)
	}
	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected visible resume to clear stale headless hint, got %#v", hint)
	}
	if len(gateway.operations) == 0 || !strings.Contains(gateway.operations[len(gateway.operations)-1].CardBody, "已恢复到之前会话") {
		t.Fatalf("expected recovery notice after visible resume, got %#v", gateway.operations)
	}
}

func TestDaemonNormalResumeFallsBackToWorkspace(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-missing",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
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
	app.onEvents(context.Background(), "inst-vscode-1", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.ProductMode != "normal" || snapshot.Attachment.InstanceID != "inst-vscode-1" {
		t.Fatalf("expected workspace fallback to attach workspace instance, got %#v", snapshot)
	}
	if snapshot.Attachment.SelectedThreadID != "" || snapshot.Attachment.RouteMode != "unbound" {
		t.Fatalf("expected workspace fallback to stay unbound, got %#v", snapshot)
	}
	if hint := app.HeadlessRestoreHint("surface-1"); hint != nil {
		t.Fatalf("expected workspace fallback to clear stale headless hint, got %#v", hint)
	}
	if len(gateway.operations) == 0 || !strings.Contains(gateway.operations[len(gateway.operations)-1].CardBody, "已先回到工作区") {
		t.Fatalf("expected workspace fallback notice, got %#v", gateway.operations)
	}
}

func TestDaemonNormalResumeHeadlessTargetSkipsWorkspaceFallback(t *testing.T) {
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
		ResumeWorkspaceKey: filepath.Join("/data/dl/.local/state", "codex-remote"),
		ResumeRouteMode:    "pinned",
		ResumeHeadless:     true,
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
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		return 4321, nil
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-pool",
			DisplayName:   "headless",
			WorkspaceRoot: filepath.Join("/data/dl/.local/state", "codex-remote"),
			WorkspaceKey:  filepath.Join("/data/dl/.local/state", "codex-remote"),
			ShortName:     "headless",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})
	app.onEvents(context.Background(), "inst-headless-pool", []agentproto.Event{{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected latent surface snapshot after recovery attempt")
	}
	if snapshot.Attachment.InstanceID != "" {
		if snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.Attachment.RouteMode != "pinned" {
			t.Fatalf("expected headless resume target to reattach a concrete thread instead of workspace fallback, got %#v", snapshot)
		}
		if snapshot.WorkspaceKey == filepath.Join("/data/dl/.local/state", "codex-remote") {
			t.Fatalf("expected headless resume to avoid state-dir workspace fallback, got %#v", snapshot)
		}
	} else if snapshot.PendingHeadless.InstanceID == "" || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected headless recovery to start directly from persisted resume target, got %#v", snapshot)
	}
	for _, operation := range gateway.operations {
		if strings.Contains(operation.CardBody, "已先回到工作区") {
			t.Fatalf("expected ResumeHeadless target to skip workspace fallback notice, got %#v", gateway.operations)
		}
	}
}

func TestDaemonNormalResumeFailureEmitsNoticeAfterFirstRefresh(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	putSurfaceResumeStateForTest(t, stateDir, SurfaceResumeEntry{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "app-1",
		ChatID:             "chat-1",
		ActorUserID:        "user-1",
		ProductMode:        "normal",
		ResumeInstanceID:   "inst-vscode-1",
		ResumeThreadID:     "thread-missing",
		ResumeWorkspaceKey: "/data/dl/droid",
		ResumeRouteMode:    "pinned",
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

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-other-1",
			DisplayName:   "other",
			WorkspaceRoot: "/data/dl/other",
			WorkspaceKey:  "/data/dl/other",
			ShortName:     "other",
			Source:        "vscode",
		},
	})
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no failure notice before first refresh completes, got %#v", gateway.operations)
	}

	app.onEvents(context.Background(), "inst-other-1", []agentproto.Event{{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID: "thread-other",
			Name:     "其他会话",
			CWD:      "/data/dl/other",
			Loaded:   true,
		}},
	}})

	snapshot := app.service.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected failed resume surface to remain detached, got %#v", snapshot)
	}
	if len(gateway.operations) != 1 || !strings.Contains(gateway.operations[0].CardBody, "暂时无法恢复到之前会话") {
		t.Fatalf("expected single resume failure notice after first refresh, got %#v", gateway.operations)
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
