package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestThreadRuntimeStatusNotLoadedDoesNotBlockHeadlessRecovery(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				Name:          "修复登录流程",
				CWD:           "/data/dl/droid",
				Loaded:        false,
				State:         "not_loaded",
				RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected notLoaded thread to remain recoverable via headless, got %#v", events)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.ThreadID != "thread-1" {
		t.Fatalf("expected pending headless recovery for notLoaded thread, got %#v", snapshot)
	}
}

func TestThreadRuntimeStatusActiveMakesClaimedThreadBusyRunning(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				Name:          "修复登录流程",
				CWD:           "/data/dl/droid",
				Loaded:        true,
				State:         "running",
				RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeActive, ActiveFlags: []agentproto.ThreadActiveFlag{agentproto.ThreadActiveFlagWaitingOnApproval}},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		ProductMode:        state.ProductModeVSCode,
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_busy_running" {
		t.Fatalf("expected authoritative active runtime status to produce running-busy notice, got %#v", events)
	}
}

func TestThreadRuntimeStatusUpdateProjectsFlagsWithoutHidingSelectedThread(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadRuntimeStatusUpdated,
		ThreadID: "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{
			Type:        agentproto.ThreadRuntimeStatusTypeActive,
			ActiveFlags: []agentproto.ThreadActiveFlag{agentproto.ThreadActiveFlagWaitingOnUserInput},
		},
	})
	snapshot := svc.SurfaceSnapshot("surface-1")
	summary := requireThreadSummary(t, snapshot, "thread-1")
	if summary.RuntimeStatus != string(agentproto.ThreadRuntimeStatusTypeActive) || !summary.WaitingOnUserInput || !summary.Loaded {
		t.Fatalf("expected snapshot to project active waiting status, got %#v", summary)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:          agentproto.EventThreadRuntimeStatusUpdated,
		ThreadID:      "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded},
	})
	snapshot = svc.SurfaceSnapshot("surface-1")
	summary = requireThreadSummary(t, snapshot, "thread-1")
	if summary.RuntimeStatus != string(agentproto.ThreadRuntimeStatusTypeNotLoaded) || summary.Loaded {
		t.Fatalf("expected selected thread to remain visible but not loaded, got %#v", summary)
	}
	if snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected selected thread to remain pinned after notLoaded update, got %#v", snapshot.Attachment)
	}
}

func requireThreadSummary(t *testing.T, snapshot *control.Snapshot, threadID string) control.ThreadSummary {
	t.Helper()
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	for _, thread := range snapshot.Threads {
		if thread.ThreadID == threadID {
			return thread
		}
	}
	t.Fatalf("thread %s not found in snapshot %#v", threadID, snapshot.Threads)
	return control.ThreadSummary{}
}
