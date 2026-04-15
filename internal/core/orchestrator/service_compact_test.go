package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCompactCommandRequiresBoundThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 0, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.root.Surfaces["surface-1"].SelectedThreadID = ""

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "compact_requires_thread" {
		t.Fatalf("expected compact_requires_thread notice, got %#v", events)
	}
}

func TestCompactCommandDispatchesThreadCompactStart(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 5, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].Command == nil || events[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact agent command, got %#v", events)
	}
	if events[0].Command.Target.ThreadID != "thread-1" {
		t.Fatalf("unexpected compact target: %#v", events[0].Command.Target)
	}
	binding := svc.compactTurns["inst-1"]
	if binding == nil || binding.SurfaceSessionID != "surface-1" || binding.ThreadID != "thread-1" || binding.Status != compactTurnStatusDispatching {
		t.Fatalf("unexpected compact binding: %#v", binding)
	}
}

func TestCompactCommandRejectsWhileRegularTurnRunning(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 10, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.root.Instances["inst-1"].ActiveThreadID = "thread-1"
	svc.root.Instances["inst-1"].ActiveTurnID = "turn-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "compact_busy" {
		t.Fatalf("expected compact_busy notice, got %#v", events)
	}
}

func TestCompactPendingQueuesLaterMessageUntilTurnCompletes(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 15, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(first) != 1 || first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact command, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-1")

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-after-compact",
		Text:             "整理完以后继续",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued follow-up input, got %#v", queued)
	}
	for _, event := range queued {
		if event.Command != nil {
			t.Fatalf("expected compact pending to block immediate dispatch, got %#v", queued)
		}
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	dispatched := false
	for _, event := range completed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
		}
	}
	if !dispatched {
		t.Fatalf("expected queued input to dispatch after compact completion, got %#v", completed)
	}
	if svc.compactTurns["inst-1"] != nil {
		t.Fatalf("expected compact binding to be cleared, got %#v", svc.compactTurns["inst-1"])
	}
}

func TestCompactRunningBlocksThreadSwitchAndNewThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 17, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(first) != 1 || first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact command, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-3")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-3",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	switchEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})
	if len(switchEvents) != 1 || switchEvents[0].Notice == nil || switchEvents[0].Notice.Code != "thread_switch_compacting" {
		t.Fatalf("expected thread_switch_compacting notice, got %#v", switchEvents)
	}

	newEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})
	if len(newEvents) != 1 || newEvents[0].Notice == nil || newEvents[0].Notice.Code != "new_thread_blocked_compacting" {
		t.Fatalf("expected new_thread_blocked_compacting notice, got %#v", newEvents)
	}
}

func TestDetachDuringCompactWaitsForCompactCompletion(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 18, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(first) != 1 || first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact command, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-4")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-4",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	detachEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(detachEvents) != 1 || detachEvents[0].Notice == nil || detachEvents[0].Notice.Code != "detach_pending" {
		t.Fatalf("expected detach_pending notice, got %#v", detachEvents)
	}
	if !svc.root.Surfaces["surface-1"].Abandoning {
		t.Fatalf("expected surface to enter abandoning during compact detach")
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-compact-4",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	gotDetached := false
	for _, event := range completed {
		if event.Notice != nil && event.Notice.Code == "detached" {
			gotDetached = true
			break
		}
	}
	if !gotDetached {
		t.Fatalf("expected detached notice after compact completion, got %#v", completed)
	}
	if svc.root.Surfaces["surface-1"].AttachedInstanceID != "" {
		t.Fatalf("expected surface to detach after compact completion, got %#v", svc.root.Surfaces["surface-1"])
	}
}

func TestCompactStartFailureRestoresQueuedDispatch(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 20, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(first) != 1 || first[0].Command == nil {
		t.Fatalf("expected compact command, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-2")

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-after-failed-compact",
		Text:             "失败后继续",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:             "compact_start_failed",
		Layer:            "server",
		Stage:            "command_response",
		Operation:        "thread.compact.start",
		Message:          "Codex 拒绝了这次上下文整理请求。",
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	}))
	dispatched := false
	gotNotice := false
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code != "" {
			gotNotice = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
		}
	}
	if !gotNotice || !dispatched {
		t.Fatalf("expected compact failure notice plus queued dispatch, got %#v", events)
	}
	if svc.compactTurns["inst-1"] != nil {
		t.Fatalf("expected compact binding to clear after failure, got %#v", svc.compactTurns["inst-1"])
	}
}

func TestCompactDisconnectClearsBindingAndAllowsRetryAfterReconnect(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 25, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.ApplyInstanceDisconnected("inst-1")
	if svc.compactTurns["inst-1"] != nil {
		t.Fatalf("expected disconnect to clear compact binding, got %#v", svc.compactTurns["inst-1"])
	}

	svc.ApplyInstanceConnected("inst-1")
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
		ThreadID:         "thread-1",
	})

	retry := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(retry) != 1 || retry[0].Command == nil || retry[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact retry after reconnect, got %#v", retry)
	}
}

func TestCompactTransportDegradedClearsBindingAndAllowsRetryAfterReconnect(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 26, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.ApplyInstanceTransportDegraded("inst-1", false)
	if svc.compactTurns["inst-1"] != nil {
		t.Fatalf("expected transport degraded to clear compact binding, got %#v", svc.compactTurns["inst-1"])
	}

	svc.ApplyInstanceConnected("inst-1")
	retry := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(retry) != 1 || retry[0].Command == nil || retry[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact retry after reconnect, got %#v", retry)
	}
}

func TestCompactRemoveInstanceClearsBinding(t *testing.T) {
	now := time.Date(2026, 4, 14, 18, 27, 0, 0, time.UTC)
	svc := newCompactServiceFixture(&now)
	startCompactDispatching(t, svc)

	svc.RemoveInstance("inst-1")
	if svc.compactTurns["inst-1"] != nil {
		t.Fatalf("expected remove instance to clear compact binding, got %#v", svc.compactTurns["inst-1"])
	}
}

func startCompactDispatching(t *testing.T, svc *Service) {
	t.Helper()
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCompact,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(first) != 1 || first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadCompactStart {
		t.Fatalf("expected compact command, got %#v", first)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-compact-audit")
}

func newCompactServiceFixture(now *time.Time) *Service {
	svc := newServiceForTest(now)
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
			"thread-2": {ThreadID: "thread-2", Name: "另一个会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	return svc
}
