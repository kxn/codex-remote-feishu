package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestThreadLifecycleArchiveAndUnarchiveUpdateVisibilityWithoutDetaching(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(threadLifecycleInstance())
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadLifecycleUpdated,
		ThreadID: "thread-1",
		ThreadLifecycle: &agentproto.ThreadLifecycleUpdate{
			ThreadID: "thread-1",
			Action:   agentproto.ThreadLifecycleArchived,
		},
	}); len(events) != 0 {
		t.Fatalf("expected state-only archive update, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if !thread.Archived {
		t.Fatalf("expected thread to be archived, got %#v", thread)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot.Attachment.InstanceID != "inst-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("archive must not detach or rewrite selection immediately, got %#v", snapshot.Attachment)
	}
	if len(snapshot.Threads) != 0 {
		t.Fatalf("archived thread should be hidden from list, got %#v", snapshot.Threads)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadLifecycleUpdated,
		ThreadID: "thread-1",
		ThreadLifecycle: &agentproto.ThreadLifecycleUpdate{
			ThreadID: "thread-1",
			Action:   agentproto.ThreadLifecycleUnarchived,
		},
	})
	snapshot = svc.SurfaceSnapshot("surface-1")
	if len(snapshot.Threads) != 1 || snapshot.Threads[0].ThreadID != "thread-1" {
		t.Fatalf("expected unarchived thread to return to list, got %#v", snapshot.Threads)
	}
}

func TestThreadLifecycleDeletedClearsSelectedThreadAndClaim(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(threadLifecycleInstance())
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadLifecycleUpdated,
		ThreadID: "thread-1",
		ThreadLifecycle: &agentproto.ThreadLifecycleUpdate{
			ThreadID: "thread-1",
			Action:   agentproto.ThreadLifecycleDeleted,
		},
	}); len(events) != 0 {
		t.Fatalf("expected state-only delete update, got %#v", events)
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("deleted selected thread should leave surface attached but unbound, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	if svc.threadClaimSurface("thread-1") != nil {
		t.Fatal("expected deleted thread claim to be released")
	}
	if thread := svc.root.Instances["inst-1"].Threads["thread-1"]; thread == nil || !thread.Archived {
		t.Fatalf("expected deleted thread to remain as hidden tombstone, got %#v", thread)
	}
}

func TestThreadLifecycleClosedMarksNotLoadedWithoutDetaching(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(threadLifecycleInstance())
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadLifecycleUpdated,
		ThreadID: "thread-1",
		ThreadLifecycle: &agentproto.ThreadLifecycleUpdate{
			ThreadID: "thread-1",
			Action:   agentproto.ThreadLifecycleClosed,
		},
	}); len(events) != 0 {
		t.Fatalf("expected state-only closed update, got %#v", events)
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("closed must not detach or clear selection, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.RuntimeStatus == nil || thread.RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeNotLoaded || thread.Loaded {
		t.Fatalf("expected closed thread to be marked notLoaded, got %#v", thread)
	}
}

func TestThreadGoalAndSettingsStoreLatestStateOnly(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(threadLifecycleInstance())

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadGoalUpdated,
		ThreadID: "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:        "thread-1",
			Objective:       "ship it",
			Status:          "active",
			TokenBudget:     1200,
			TokensUsed:      345,
			TimeUsedSeconds: 67,
		},
	}); len(events) != 0 {
		t.Fatalf("expected state-only goal update, got %#v", events)
	}
	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.ThreadGoal == nil || thread.ThreadGoal.Objective != "ship it" {
		t.Fatalf("expected goal state, got %#v", thread.ThreadGoal)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadSettingsUpdated,
		ThreadID: "thread-1",
		ThreadSettings: &agentproto.ThreadSettingsUpdate{
			ThreadID:        "thread-1",
			Model:           "gpt-5.4",
			ReasoningEffort: "high",
			ApprovalPolicy:  "on-request",
			Sandbox:         "workspace-write",
		},
	})
	if thread.ThreadSettings == nil || thread.ThreadSettings.Model != "gpt-5.4" {
		t.Fatalf("expected settings state, got %#v", thread.ThreadSettings)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadGoalUpdated,
		ThreadID: "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID: "thread-1",
			Cleared:  true,
		},
	})
	if thread.ThreadGoal == nil || !thread.ThreadGoal.Cleared || thread.ThreadGoal.Objective != "" {
		t.Fatalf("expected cleared goal state, got %#v", thread.ThreadGoal)
	}
}

func threadLifecycleInstance() *state.InstanceRecord {
	return &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "修复登录流程",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
		},
	}
}
