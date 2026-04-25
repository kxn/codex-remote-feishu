package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestExplicitPauseSurfaceDispatchSkipsWatchdogUntilExplicitResume(t *testing.T) {
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	svc.PauseSurfaceDispatch("surface-1")
	if len(svc.pausedUntil) != 0 {
		t.Fatalf("expected explicit pause to avoid watchdog deadline, got %#v", svc.pausedUntil)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续执行",
	})
	for _, event := range events {
		if event.Command != nil {
			t.Fatalf("expected paused surface to avoid dispatch, got %#v", events)
		}
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil || surface.DispatchMode != state.DispatchModePausedForLocal || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected paused queued surface state, got %#v", surface)
	}

	now = now.Add(30 * time.Second)
	events = svc.Tick(now)
	if len(events) != 0 {
		t.Fatalf("expected no watchdog auto-resume for explicit pause, got %#v", events)
	}
	if surface.DispatchMode != state.DispatchModePausedForLocal || len(surface.QueuedQueueItemIDs) != 1 || surface.ActiveQueueItemID != "" {
		t.Fatalf("expected surface to remain paused after tick, got %#v", surface)
	}

	events = svc.ResumeSurfaceDispatch("surface-1", nil)
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected explicit resume to restore normal dispatch, got %#v", surface)
	}
	var dispatched bool
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
			break
		}
	}
	if !dispatched {
		t.Fatalf("expected explicit resume to dispatch queued prompt, got %#v", events)
	}
	if surface.ActiveQueueItemID == "" || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected queued item to become active after resume, got %#v", surface)
	}
}

func TestQueueDispatchSetsExplicitLegacyExecutionTargetDefaults(t *testing.T) {
	now := time.Date(2026, 4, 26, 9, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续执行",
	})
	var promptCommand *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			promptCommand = event.Command
			break
		}
	}
	if promptCommand == nil {
		t.Fatalf("expected prompt.send command, got %#v", events)
	}
	if promptCommand.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting {
		t.Fatalf("expected resume_existing mode, got %#v", promptCommand.Target)
	}
	if promptCommand.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyFollowExecutionThread {
		t.Fatalf("expected follow_execution_thread binding policy, got %#v", promptCommand.Target)
	}
	if promptCommand.Target.CreateThreadIfMissing {
		t.Fatalf("expected createThreadIfMissing=false for existing thread, got %#v", promptCommand.Target)
	}

	now = now.Add(1 * time.Minute)
	svc2 := newServiceForTest(&now)
	svc2.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "empty",
		WorkspaceRoot: "/data/dl/empty",
		WorkspaceKey:  "/data/dl/empty",
		ShortName:     "empty",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc2.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-2",
	})
	events = svc2.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		MessageID:        "msg-2",
		Text:             "开启新会话",
	})
	promptCommand = nil
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			promptCommand = event.Command
			break
		}
	}
	if promptCommand == nil {
		t.Fatalf("expected prompt.send command for new thread route, got %#v", events)
	}
	if promptCommand.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew {
		t.Fatalf("expected start_new mode, got %#v", promptCommand.Target)
	}
	if !promptCommand.Target.CreateThreadIfMissing {
		t.Fatalf("expected createThreadIfMissing=true for new thread route, got %#v", promptCommand.Target)
	}
}
