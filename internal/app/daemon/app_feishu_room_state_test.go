package daemon

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestConfigureFeishuRoomStateMaterializesStore(t *testing.T) {
	stateDir := t.TempDir()
	store, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuRoomStateLocked(stateDir)
	app.mu.Unlock()

	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].PrimaryGatewayID != "app-1" {
		t.Fatalf("materialized records = %#v, want app-1", records)
	}
}

func TestSyncFeishuRoomStateDeletesClearedPrimary(t *testing.T) {
	stateDir := t.TempDir()
	store, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuRoomStateLocked(stateDir)
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary off",
	})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()

	reloaded, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := reloaded.Get("oc_room"); ok {
		t.Fatal("expected cleared primary to be deleted from store")
	}
}

func TestCoworkersSettingRejectsWhenRoomStateCannotBePersisted(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{
		{RoomID: "feishu:chat:oc_room", ChatID: "oc_room", PrimaryGatewayID: "app-1"},
	})
	app.feishuRoomState.store = feishuroomstate.NewStore(t.TempDir())
	app.feishuRoomState.status = persistedStoreStatusWritable

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionCoworkersCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/coworkers 2",
	})

	room := app.service.FeishuRoomState()
	if len(room) != 1 || state.FeishuRoomConcurrencyLimit(room[0].ConcurrencyLimit) != 1 {
		t.Fatalf("failed durable setting changed runtime state: %#v", room)
	}
	if len(gateway.operations) != 1 || !strings.Contains(operationCardText(gateway.operations[0]), "当前配置未改变") {
		t.Fatalf("failed durable setting should emit rejection notice, got %#v", gateway.operations)
	}
}

func TestSyncFeishuRoomStateKeepsWorkspaceWhenPrimaryCleared(t *testing.T) {
	stateDir := t.TempDir()
	store, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load store: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/dl/workspace",
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuRoomStateLocked(stateDir)
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary off",
	})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()

	reloaded, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	record, ok := reloaded.Get("oc_room")
	if !ok || record.PrimaryGatewayID != "" || record.WorkspaceKey != "/data/dl/workspace" {
		t.Fatalf("workspace-only record after primary clear = %#v, present=%v", record, ok)
	}
}

func TestSyncFeishuRoomStateDoesNotRewriteUnchangedState(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.mu.Lock()
	app.configureFeishuRoomStateLocked(stateDir)
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:       "feishu:chat:oc_room",
		ChatID:       "oc_room",
		WorkspaceKey: "/data/dl/workspace",
	}})
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()

	path := feishuroomstate.StatePath(stateDir)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat initial room state: %v", err)
	}
	app.mu.Lock()
	app.syncFeishuRoomStateLocked()
	app.mu.Unlock()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat unchanged room state: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("unchanged room state sync rewrote the durable file")
	}
}

func TestSetHeadlessRuntimeBackfillsRoomWorkspaceFromConsistentSurfaceResumeState(t *testing.T) {
	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	for _, gatewayID := range []string{"app-1", "app-2"} {
		putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
			SurfaceSessionID:   "feishu:" + gatewayID + ":chat:oc_room",
			GatewayID:          gatewayID,
			ChatID:             "oc_room",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-" + gatewayID,
			ResumeThreadCWD:    workspaceDir,
			ResumeWorkspaceKey: workspaceDir,
			ResumeHeadless:     true,
		})
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})

	expectedWorkspace := state.ResolveWorkspaceClaimKey(workspaceDir)
	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].WorkspaceKey != expectedWorkspace {
		t.Fatalf("materialized room state = %#v, want one backfilled workspace", records)
	}
	reloaded, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload room state: %v", err)
	}
	if record, ok := reloaded.Get("oc_room"); !ok || record.WorkspaceKey != expectedWorkspace {
		t.Fatalf("persisted room state = %#v, present=%v", record, ok)
	}
}

func TestSetHeadlessRuntimeMigratesLegacyPrimaryAndWorkspaceAsOneRoomRecord(t *testing.T) {
	stateDir := t.TempDir()
	workspaceDir := t.TempDir()
	legacy := []byte(`{"version":1,"entries":{"feishu:chat:oc_room":{"RoomID":"feishu:chat:oc_room","ChatID":"oc_room","PrimaryGatewayID":"app-1","PrimaryUpdatedBy":"ou_admin"}}}`)
	if err := os.WriteFile(feishuroomstate.StatePath(stateDir), legacy, 0o600); err != nil {
		t.Fatalf("write legacy room state: %v", err)
	}
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ActorUserID:        "ou_workspace",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    workspaceDir,
		ResumeWorkspaceKey: workspaceDir,
		ResumeHeadless:     true,
	})

	app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})

	reloaded, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload migrated room state: %v", err)
	}
	expectedWorkspace := state.ResolveWorkspaceClaimKey(workspaceDir)
	record, ok := reloaded.Get("oc_room")
	if !ok || record.PrimaryGatewayID != "app-1" || record.PrimaryUpdatedBy != "ou_admin" || record.WorkspaceKey != expectedWorkspace {
		t.Fatalf("migrated room record = %#v, present=%v", record, ok)
	}
	if reloaded.Dirty() {
		t.Fatal("migrated v2 room state must not require another rewrite")
	}
}

func TestConflictingSurfaceResumeWorkspacesFailClosedBeforeGroupIngress(t *testing.T) {
	stateDir := t.TempDir()
	for index, gatewayID := range []string{"app-1", "app-2"} {
		workspaceDir := t.TempDir()
		putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
			SurfaceSessionID:   "feishu:" + gatewayID + ":chat:oc_room",
			GatewayID:          gatewayID,
			ChatID:             "oc_room",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-" + gatewayID,
			ResumeThreadCWD:    workspaceDir,
			ResumeWorkspaceKey: workspaceDir,
			ResumeHeadless:     true,
			UpdatedAt:          time.Date(2026, 7, 31, 10, index, 0, 0, time.UTC),
		})
	}

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	started := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		started = true
		return 4321, nil
	}

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		MessageID:        "om-conflict-1",
		Text:             "继续任务",
	})

	if started {
		t.Fatal("conflicting room workspaces must not select one surface resume entry")
	}
	if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].CardTitle != "群工作区状态冲突" {
		t.Fatalf("conflict diagnostic operations = %#v", gateway.operations)
	}

	for _, test := range []struct {
		name   string
		action control.Action
	}{
		{name: "list", action: control.Action{Kind: control.ActionListInstances, Text: "/list"}},
		{name: "use", action: control.Action{Kind: control.ActionUseThread, Text: "/use thread-1", ThreadID: "thread-1"}},
		{name: "menu callback", action: control.Action{Kind: control.ActionTargetPickerConfirm, PickerID: "picker-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			gateway.operations = nil
			test.action.SurfaceSessionID = "feishu:app-1:chat:oc_room"
			test.action.GatewayID = "app-1"
			test.action.ChatID = "oc_room"
			test.action.ActorUserID = "ou_user"
			test.action.Inbound = &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID}
			if result := app.HandleGatewayAction(context.Background(), test.action); result != nil {
				t.Fatalf("conflict gate inline result = %#v, want nil append-only notice", result)
			}
			if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].CardTitle != "群工作区状态冲突" {
				t.Fatalf("conflict gate operations = %#v", gateway.operations)
			}
		})
	}

	t.Run("old menu callback is rejected before conflict gate", func(t *testing.T) {
		gateway.operations = nil
		result := app.HandleGatewayAction(context.Background(), control.Action{
			Kind:             control.ActionTargetPickerConfirm,
			SurfaceSessionID: "feishu:app-1:chat:oc_room",
			GatewayID:        "app-1",
			ChatID:           "oc_room",
			ActorUserID:      "ou_user",
			PickerID:         "picker-1",
			Inbound: &control.ActionInboundMeta{
				CardDaemonLifecycleID: "old-daemon-lifecycle",
			},
		})
		if result != nil {
			t.Fatalf("old callback inline result = %#v, want nil append-only notice", result)
		}
		if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].CardTitle != "旧卡片已过期" {
			t.Fatalf("old callback operations = %#v", gateway.operations)
		}
	})
}

func TestDurableRoomWorkspaceRejectsMismatchedSurfaceResumeTarget(t *testing.T) {
	stateDir := t.TempDir()
	durableWorkspace := t.TempDir()
	staleWorkspace := t.TempDir()
	store, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load room state: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:       "feishu:chat:oc_room",
		ChatID:       "oc_room",
		WorkspaceKey: durableWorkspace,
	}); err != nil {
		t.Fatalf("put room state: %v", err)
	}
	putSurfaceResumeStateForTest(t, stateDir, surfaceresume.Entry{
		SurfaceSessionID:   "feishu:app-1:chat:oc_room",
		GatewayID:          "app-1",
		ChatID:             "oc_room",
		ProductMode:        "normal",
		Backend:            "codex",
		ResumeThreadID:     "thread-stale",
		ResumeThreadCWD:    staleWorkspace,
		ResumeWorkspaceKey: staleWorkspace,
		ResumeHeadless:     true,
	})

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/list",
	})

	if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationSendCard || gateway.operations[0].CardTitle != "群工作区状态冲突" {
		t.Fatalf("mismatched durable/surface state operations = %#v", gateway.operations)
	}
	expectedWorkspace := state.ResolveWorkspaceClaimKey(durableWorkspace)
	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].WorkspaceKey != expectedWorkspace {
		t.Fatalf("durable room workspace must remain authoritative, got %#v", records)
	}
}

func TestRoomWorkspaceSwitchClearsSiblingStaleResumeTargetBeforeRestart(t *testing.T) {
	stateDir := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	roomStore, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load room state: %v", err)
	}
	if err := roomStore.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     workspaceA,
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put room state: %v", err)
	}
	for _, entry := range []surfaceresume.Entry{
		{
			SurfaceSessionID:   "feishu:app-1:chat:oc_room",
			GatewayID:          "app-1",
			ChatID:             "oc_room",
			ActorUserID:        "ou_owner",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-a-1",
			ResumeThreadCWD:    workspaceA,
			ResumeWorkspaceKey: workspaceA,
			ResumeRouteMode:    "pinned",
			ResumeHeadless:     true,
		},
		{
			SurfaceSessionID:   "feishu:app-2:chat:oc_room",
			GatewayID:          "app-2",
			ChatID:             "oc_room",
			ActorUserID:        "ou_member",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-a-2",
			ResumeThreadCWD:    workspaceA,
			ResumeWorkspaceKey: workspaceA,
			ResumeRouteMode:    "pinned",
			ResumeHeadless:     true,
		},
	} {
		putSurfaceResumeStateForTest(t, stateDir, entry)
	}

	newTestApp := func() *App {
		app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
		app.service = orchestrator.NewService(time.Now, orchestrator.Config{}, renderer.NewPlanner())
		return app
	}
	app := newTestApp()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	for _, inst := range []*state.InstanceRecord{
		{
			InstanceID:    "inst-a-1",
			WorkspaceRoot: workspaceA,
			WorkspaceKey:  workspaceA,
			Backend:       agentproto.BackendCodex,
			Source:        "headless",
			Managed:       true,
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				"thread-a-1": {ThreadID: "thread-a-1", CWD: workspaceA, Loaded: true},
			},
		},
		{
			InstanceID:    "inst-a-2",
			WorkspaceRoot: workspaceA,
			WorkspaceKey:  workspaceA,
			Backend:       agentproto.BackendCodex,
			Source:        "headless",
			Managed:       true,
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				"thread-a-2": {ThreadID: "thread-a-2", CWD: workspaceA, Loaded: true},
			},
		},
		{
			InstanceID:    "inst-b",
			WorkspaceRoot: workspaceB,
			WorkspaceKey:  workspaceB,
			Backend:       agentproto.BackendCodex,
			Source:        "headless",
			Managed:       true,
			Online:        true,
			Threads: map[string]*state.ThreadRecord{
				"thread-b": {ThreadID: "thread-b", CWD: workspaceB, Loaded: true},
			},
		},
	} {
		app.service.UpsertInstance(inst)
	}
	for _, action := range []control.Action{
		{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "feishu:app-1:chat:oc_room", GatewayID: "app-1", ChatID: "oc_room", ActorUserID: "ou_owner", WorkspaceKey: workspaceA},
		{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "feishu:app-2:chat:oc_room", GatewayID: "app-2", ChatID: "oc_room", ActorUserID: "ou_member", WorkspaceKey: workspaceA},
		{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "feishu:app-1:chat:oc_room", GatewayID: "app-1", ChatID: "oc_room", ActorUserID: "ou_owner", WorkspaceKey: workspaceB},
	} {
		app.HandleAction(context.Background(), action)
	}

	siblingEntry := app.SurfaceResumeState("feishu:app-2:chat:oc_room")
	if siblingEntry == nil {
		t.Fatal("sibling surface resume entry was deleted with its surface identity")
	}
	if staleWorkspace := state.ResolveWorkspaceKey(siblingEntry.ResumeWorkspaceKey, siblingEntry.ResumeThreadCWD); staleWorkspace != "" {
		t.Fatalf("sibling stale resume workspace = %q, want cleared before restart", staleWorkspace)
	}

	restarted := newTestApp()
	restarted.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	if restarted.feishuRoomState.workspaceConflicts["feishu:chat:oc_room"] {
		t.Fatal("valid room workspace switch became a recovery conflict after restart")
	}
	expectedWorkspace := state.ResolveWorkspaceClaimKey(workspaceB)
	records := restarted.service.FeishuRoomState()
	if len(records) != 1 || records[0].WorkspaceKey != expectedWorkspace {
		t.Fatalf("restarted room state = %#v, want workspace B", records)
	}
}

func TestRoomWorkspaceDetachClearsSiblingResumeTargetsBeforeRestart(t *testing.T) {
	stateDir := t.TempDir()
	workspaceA := t.TempDir()
	roomStore, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load room state: %v", err)
	}
	if err := roomStore.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     workspaceA,
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put room state: %v", err)
	}
	for _, entry := range []surfaceresume.Entry{
		{
			SurfaceSessionID:   "feishu:app-1:chat:oc_room",
			GatewayID:          "app-1",
			ChatID:             "oc_room",
			ActorUserID:        "ou_owner",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-a-1",
			ResumeThreadCWD:    workspaceA,
			ResumeWorkspaceKey: workspaceA,
			ResumeRouteMode:    "pinned",
			ResumeHeadless:     true,
		},
		{
			SurfaceSessionID:   "feishu:app-2:chat:oc_room",
			GatewayID:          "app-2",
			ChatID:             "oc_room",
			ActorUserID:        "ou_member",
			ProductMode:        "normal",
			Backend:            "codex",
			ResumeThreadID:     "thread-a-2",
			ResumeThreadCWD:    workspaceA,
			ResumeWorkspaceKey: workspaceA,
			ResumeRouteMode:    "pinned",
			ResumeHeadless:     true,
		},
	} {
		putSurfaceResumeStateForTest(t, stateDir, entry)
	}

	newTestApp := func() *App {
		app := New(":0", ":0", nil, agentproto.ServerIdentity{StartedAt: time.Now().UTC()})
		app.service = orchestrator.NewService(time.Now, orchestrator.Config{}, renderer.NewPlanner())
		return app
	}
	app := newTestApp()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})

	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionWorkspaceDetach,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_owner",
	})

	for _, surfaceID := range []string{"feishu:app-1:chat:oc_room", "feishu:app-2:chat:oc_room"} {
		entry := app.SurfaceResumeState(surfaceID)
		if entry == nil {
			t.Fatalf("surface resume entry %s was deleted with its identity", surfaceID)
		}
		if staleWorkspace := state.ResolveWorkspaceKey(entry.ResumeWorkspaceKey, entry.ResumeThreadCWD); staleWorkspace != "" {
			t.Fatalf("workspace detach left stale resume workspace for %s = %q", surfaceID, staleWorkspace)
		}
		if entry.ResumeInstanceID != "" || entry.ResumeThreadID != "" || entry.ResumeRouteMode != "" || entry.ResumeHeadless {
			t.Fatalf("workspace detach left stale resume target for %s: %#v", surfaceID, entry)
		}
	}
	records := app.service.FeishuRoomState()
	if len(records) != 1 || records[0].WorkspaceKey != "" || records[0].PrimaryGatewayID != "app-1" {
		t.Fatalf("workspace detach should clear only room workspace and preserve primary, got %#v", records)
	}

	restarted := newTestApp()
	restarted.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	if restarted.feishuRoomState.workspaceConflicts["feishu:chat:oc_room"] {
		t.Fatal("cleared room workspace became a recovery conflict after restart")
	}
	for _, record := range restarted.service.FeishuRoomState() {
		if record.WorkspaceKey != "" {
			t.Fatalf("restarted room state restored cleared workspace: %#v", restarted.service.FeishuRoomState())
		}
	}
}
