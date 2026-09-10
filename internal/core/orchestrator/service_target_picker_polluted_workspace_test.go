package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerSessionOptionsIncludesThreadsWithCrossInstancePollutedWorkspaceKey(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	// An instance serving /data/repo/svmpy
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-svm",
		DisplayName:   "svmpy",
		WorkspaceRoot: "/data/repo/svmpy",
		WorkspaceKey:  "/data/repo/svmpy",
		ShortName:     "svmpy",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-svm": {
				ThreadID:     "thread-svm",
				Name:         "svmnew0910",
				CWD:          "/data/repo/svmpy",
				WorkspaceKey: "/home/qagent/.local/state/codex-remote/headless-pool", // Cross-instance polluted WorkspaceKey
				Loaded:       true,
				LastUsedAt:   now,
			},
		},
	})

	// Attach surface to /data/repo/svmpy
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/repo/svmpy",
	})

	// Trigger ShowThreads (/use)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected 1 target picker event, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.SelectedWorkspaceKey != "/data/repo/svmpy" {
		t.Fatalf("expected selected workspace to be /data/repo/svmpy, got %q", view.SelectedWorkspaceKey)
	}

	// thread-svm should be present in SessionOptions despite having a polluted WorkspaceKey
	option, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-svm"))
	if !ok {
		t.Fatalf("expected thread-svm with polluted WorkspaceKey to be included in SessionOptions, got %#v", view.SessionOptions)
	}
	if option.Label != "svmpy · svmnew0910" {
		t.Fatalf("expected option label to be 'svmpy · svmnew0910', got %q", option.Label)
	}
}

func TestEventThreadsSnapshotDoesNotPolluteThreadWorkspaceKeyFromUnrelatedInstance(t *testing.T) {
	now := time.Date(2026, 9, 10, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	// A managed headless instance running in the state dir (e.g. headless pool preheat)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-pool",
		DisplayName:   "headless-pool",
		WorkspaceRoot: "/home/qagent/.local/state/codex-remote/headless-pool",
		WorkspaceKey:  "/home/qagent/.local/state/codex-remote/headless-pool",
		ShortName:     "headless-pool",
		Source:        "headless",
		Managed:       true,
		Online:        true,
	})

	// Headless instance emits snapshot containing threads from another workspace (e.g. /data/repo/svmpy)
	// with empty WorkspaceKey
	svc.ApplyAgentEvent("inst-headless-pool", agentproto.Event{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{
			{
				ThreadID: "thread-svm-snap",
				Name:     "svmnew0910",
				CWD:      "/data/repo/svmpy",
				Loaded:   true,
			},
		},
	})

	inst := svc.root.Instances["inst-headless-pool"]
	thread := inst.Threads["thread-svm-snap"]
	if thread == nil {
		t.Fatal("expected thread-svm-snap to exist in inst-headless-pool")
	}

	// The thread's WorkspaceKey should NOT be overwritten with inst.WorkspaceKey
	if testutil.SamePath(thread.WorkspaceKey, "/home/qagent/.local/state/codex-remote/headless-pool") {
		t.Fatalf("expected thread WorkspaceKey not to be polluted by headless-pool instance, got %q", thread.WorkspaceKey)
	}
	if !testutil.SamePath(thread.WorkspaceKey, "/data/repo/svmpy") {
		t.Fatalf("expected thread WorkspaceKey to resolve to thread CWD /data/repo/svmpy, got %q", thread.WorkspaceKey)
	}
}
