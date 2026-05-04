package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestObserveConfigClaudeThreadAccessDoesNotPersistWorkspaceDefaults(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           workspaceKey,
		WorkspaceKey:            workspaceKey,
		ShortName:               "droid",
		Backend:                 agentproto.BackendClaude,
		ClaudeProfileID:         state.DefaultClaudeProfileID,
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         workspaceKey,
		ConfigScope: "thread",
		AccessMode:  agentproto.AccessModeConfirm,
	})

	if defaults := svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
	})]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected claude observed thread access not to persist workspace defaults, got %#v", defaults)
	}
	if got := svc.root.Instances["inst-1"].Threads["thread-1"].ObservedAccessMode; got != agentproto.AccessModeConfirm {
		t.Fatalf("expected thread observed access mode, got %q", got)
	}
}

func TestClaudeHeadlessObservedThreadAccessFeedsPromptFreeze(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
	})] = state.ModelConfigRecord{AccessMode: agentproto.AccessModeFullAccess}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           workspaceKey,
		WorkspaceKey:            workspaceKey,
		ShortName:               "droid",
		Backend:                 agentproto.BackendClaude,
		ClaudeProfileID:         state.DefaultClaudeProfileID,
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         workspaceKey,
		ConfigScope: "thread",
		AccessMode:  agentproto.AccessModeConfirm,
		PlanMode:    "on",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.ObservedThreadAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected observed thread access in snapshot, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "thread" {
		t.Fatalf("expected snapshot to use observed thread access, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.ObservedThreadPlanMode != "on" {
		t.Fatalf("expected observed thread plan in snapshot, got %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected queue item to freeze observed thread access, got %#v", item.FrozenOverride)
	}
}
