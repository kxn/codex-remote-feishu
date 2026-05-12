package orchestrator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestLocalTurnStartedDoesNotBindUnboundAttachedSurfaceToDetectedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-3" {
		t.Fatalf("expected observed focused thread to be updated, got %q", inst.ObservedFocusedThreadID)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected unbound surface to remain unchanged, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestLocalInteractionDoesNotSwitchPinnedSurfaceBeforeTurnStarts(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to stay on prior thread until actual turn starts, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestFollowLocalSurfaceBindsOnLocalTurnStart(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "补充文档", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-3" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected follow-local surface to bind to local-ui turn thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 2 || events[1].ThreadSelection == nil || events[1].ThreadSelection.ThreadID != "thread-3" {
		t.Fatalf("expected local pause notice plus follow selection update, got %#v", events)
	}
}

func TestLocalTurnStartedDoesNotSwitchPinnedSurfaceToDetectedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-3": {ThreadID: "thread-3", Name: "补充文档", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to remain on prior thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestRenderTextItemDoesNotSwitchSurfaceToUnclaimedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.renderTextItem("inst-1", "thread-2", "turn-1", "item-1", "好的，我来整理日志。", false)

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to remain on claimed thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 0 {
		t.Fatalf("expected no projection for unclaimed local thread output, got %#v", events)
	}
}

func TestAttachReplaysUndeliveredFinalForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	finished := recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "这是离线期间的 final")
	if len(finished) != 0 {
		t.Fatalf("expected no UI events without attached surface, got %#v", finished)
	}
	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.UndeliveredReplay == nil || thread.UndeliveredReplay.Kind != state.ThreadReplayAssistantFinal {
		t.Fatalf("expected undelivered final replay to be stored, got %#v", thread.UndeliveredReplay)
	}

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	var sawBlock bool
	for _, event := range attach {
		if event.Block != nil && event.Block.Text == "这是离线期间的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected attach to replay stored final block, got %#v", attach)
	}
	if thread.UndeliveredReplay != nil {
		t.Fatalf("expected replay to clear after attach, got %#v", thread.UndeliveredReplay)
	}

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface-1"})
	again := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range again {
		if event.Block != nil {
			t.Fatalf("expected replay to be one-shot, got %#v", again)
		}
	}
}

func TestUseThreadReplaysUndeliveredFinalForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "等待 /use 的 final")
	svc.root.Instances["inst-1"].ActiveThreadID = ""
	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil {
			t.Fatalf("expected unbound attach not to replay immediately, got %#v", attach)
		}
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected attach without default thread to remain unbound, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}

	use := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	var sawBlock bool
	for _, event := range use {
		if event.Block != nil && event.Block.Text == "等待 /use 的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected /use to replay stored final block, got %#v", use)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected replay to clear after /use, got %#v", replay)
	}
}

func TestBusyAttachDefersReplayUntilThreadIsIdle(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveTurnID:            "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-0", "item-0", "旧 final")
	svc.root.Instances["inst-1"].ActiveThreadID = "thread-1"
	svc.root.Instances["inst-1"].ActiveTurnID = "turn-running"
	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil {
			t.Fatalf("expected busy attach not to replay immediately, got %#v", attach)
		}
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil {
		t.Fatalf("expected replay to remain queued while thread is busy")
	}

	svc.root.Instances["inst-1"].ActiveTurnID = ""
	use := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	var sawBlock bool
	for _, event := range use {
		if event.Block != nil && event.Block.Text == "旧 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected replay after thread becomes idle, got %#v", use)
	}
}

func TestAttachReplaysUndeliveredProblemNoticeForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 15, 0, 0, time.UTC)
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:      "stdout_parse_failed",
		Layer:     "wrapper",
		Stage:     "observe_codex_stdout",
		Operation: "codex.stdout",
		ThreadID:  "thread-1",
		Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
		Details:   "invalid character 'x' looking for beginning of value",
	}))
	if len(events) != 0 {
		t.Fatalf("expected no UI events without attached surface, got %#v", events)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil || replay.Kind != state.ThreadReplayNotice {
		t.Fatalf("expected undelivered problem notice to be stored, got %#v", replay)
	}

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	var sawNotice bool
	for _, event := range attach {
		if event.Notice != nil && event.Notice.Code == debugErrorNoticeCode && strings.Contains(event.Notice.Text, "invalid character") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatalf("expected attach to replay stored problem notice, got %#v", attach)
	}
}

func TestDeliveredLocalFinalDoesNotCreateUndeliveredReplay(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 20, 0, 0, time.UTC)
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

	finished := recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "已实时投递的 final")
	var sawBlock bool
	for _, event := range finished {
		if event.Block != nil && event.Block.Text == "已实时投递的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected local final to render on attached surface, got %#v", finished)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected delivered final not to be stored as replay, got %#v", replay)
	}
}

func TestUndeliveredReplayDoesNotSurviveServiceRebuild(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 25, 0, 0, time.UTC)
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
	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "重建前的 final")
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil {
		t.Fatal("expected replay to exist before rebuild")
	}

	rebuilt := newServiceForTest(&now)
	rebuilt.UpsertInstance(&state.InstanceRecord{
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
	attach := rebuilt.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil || (event.Notice != nil && event.Notice.Code == debugErrorNoticeCode) {
			t.Fatalf("expected rebuilt service not to recover old replay, got %#v", attach)
		}
	}
}

func TestDispatchingRemoteTurnOverridesStaleLocalClassification(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(started) == 0 || started[0].PendingInput == nil || started[0].PendingInput.Status != string(state.QueueItemRunning) {
		t.Fatalf("expected active queue item to be promoted to running despite stale local initiator, got %#v", started)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected no local pause for queued remote turn, got %q", surface.DispatchMode)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(completed) == 0 || completed[0].PendingInput == nil || !completed[0].PendingInput.TypingOff {
		t.Fatalf("expected queued remote turn to complete normally, got %#v", completed)
	}
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected surface to remain in normal mode, got %q", surface.DispatchMode)
	}
}

func TestApprovalRequestPromptUsesAttachedSurfaceForLocalTurn(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType":  "approval",
			"title":        "需要确认",
			"body":         "本地 Codex 想执行 `git push`。",
			"acceptLabel":  "允许执行",
			"declineLabel": "拒绝",
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestID != "req-1" || prompt.ThreadTitle != "droid · 修复登录流程" {
		t.Fatalf("unexpected request prompt: %#v", prompt)
	}
	if len(prompt.Sections) != 1 || !containsPromptSectionLine(prompt.Sections[0], "本地 Codex 想执行 `git push`。") {
		t.Fatalf("expected approval body to be carried as sections, got %#v", prompt.Sections)
	}
	if len(prompt.Options) != 3 || prompt.Options[0].OptionID != "accept" || prompt.Options[1].OptionID != "decline" || prompt.Options[2].OptionID != "captureFeedback" {
		t.Fatalf("unexpected request options: %#v", prompt.Options)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.RequestType != "approval" {
		t.Fatalf("expected pending request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
	if len(record.Sections) != 1 || !containsStatePromptSectionLine(record.Sections[0], "本地 Codex 想执行 `git push`。") {
		t.Fatalf("expected approval sections in state, got %#v", record)
	}
	if len(record.Options) != 3 {
		t.Fatalf("expected request options in state, got %#v", record)
	}
	if events[0].RequestContext == nil {
		t.Fatalf("expected feishu request context, got %#v", events[0])
	}
	if events[0].RequestContext.DTOOwner != control.FeishuUIDTOwnerRequest {
		t.Fatalf("unexpected dto owner: %#v", events[0].RequestContext)
	}
	if events[0].RequestContext.RequestID != "req-1" || events[0].RequestContext.RequestType != "approval" {
		t.Fatalf("unexpected request context payload: %#v", events[0].RequestContext)
	}
	if !events[0].RequestContext.Surface.RouteMutationBlocked || events[0].RequestContext.Surface.RouteMutationBlockedBy != "pending_request" {
		t.Fatalf("expected pending request to block route mutation, got %#v", events[0].RequestContext.Surface)
	}
}

func TestUnsupportedMCPRequestStoresPendingStateWithoutRenderingApprovalCard(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-mcp-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:   agentproto.RequestTypePermissionsRequestApproval,
			Title:  "需要授予权限",
			Body:   "本地 Codex 正在等待 docs.read 权限。",
			ItemID: "item-1",
		},
		Metadata: map[string]any{
			"requestType": "permissions_request_approval",
			"title":       "需要授予权限",
			"body":        "本地 Codex 正在等待 docs.read 权限。",
			"itemId":      "item-1",
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected renderable permissions request prompt, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestType != "permissions_request_approval" {
		t.Fatalf("unexpected request prompt payload: %#v", prompt)
	}
	if len(prompt.Sections) == 0 {
		t.Fatalf("expected permissions prompt to expose structured sections, got %#v", prompt)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-mcp-1"]
	if record == nil || record.RequestType != "permissions_request_approval" {
		t.Fatalf("expected pending permissions request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
	if len(record.Sections) == 0 {
		t.Fatalf("expected permissions prompt sections in state, got %#v", record)
	}
	if record.Prompt == nil || record.Prompt.Type != agentproto.RequestTypePermissionsRequestApproval {
		t.Fatalf("expected typed request prompt to be retained in state, got %#v", record)
	}
}

func TestRespondRequestDispatchesCommandAndClearsOnResolve(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-1", "", "accept", nil, 0),
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed request replacement plus one agent command event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || prompt.Phase != "waiting_dispatch" {
		t.Fatalf("expected approval request to seal before dispatch, got %#v", prompt)
	}
	command := events[1].Command
	if command.Kind != agentproto.CommandRequestRespond || command.Request.RequestID != "req-1" {
		t.Fatalf("unexpected request respond command: %#v", command)
	}
	if command.Request.Response["decision"] != "accept" || command.Request.Response["type"] != "approval" {
		t.Fatalf("unexpected request respond payload: %#v", command.Request.Response)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected request state to be cleared, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}

	expired := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request:          testRequestAction("req-1", "", "decline", nil, 0),
	})
	if len(expired) != 1 || expired[0].Notice == nil || expired[0].Notice.Code != "request_expired" {
		t.Fatalf("expected expired notice after resolve, got %#v", expired)
	}
}

func TestRespondRequestAcceptForSessionDispatchesDecision(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
			"options": []map[string]any{
				{"id": "accept", "label": "允许一次"},
				{"id": "acceptForSession", "label": "本会话允许"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-1", "", "acceptForSession", nil, 0),
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed request replacement plus one agent command event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || prompt.Phase != "waiting_dispatch" {
		t.Fatalf("expected approval request to seal before dispatch, got %#v", prompt)
	}
	if events[1].Command.Request.Response["decision"] != "acceptForSession" {
		t.Fatalf("unexpected request decision payload: %#v", events[1].Command.Request.Response)
	}
}

func TestRequestUserInputPromptUsesQuestionsAndStoresState(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"title":       "需要补充输入",
			"itemId":      "item-1",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}}},
				{"id": "notes", "header": "备注", "question": "补充说明", "isOther": true, "isSecret": true},
			},
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestType != "request_user_input" || len(prompt.Questions) != 2 {
		t.Fatalf("unexpected request prompt: %#v", prompt)
	}
	if len(prompt.Sections) != 1 || !containsPromptSectionLine(prompt.Sections[0], "本地 Codex 正在等待你补充参数或说明。") {
		t.Fatalf("expected request_user_input prompt to expose structured sections, got %#v", prompt.Sections)
	}
	if !prompt.Questions[0].DirectResponse || !prompt.Questions[1].Secret {
		t.Fatalf("unexpected request prompt questions: %#v", prompt.Questions)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.ItemID != "item-1" || len(record.Questions) != 2 {
		t.Fatalf("expected pending request state for request_user_input, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
	if len(record.Sections) != 1 || !containsStatePromptSectionLine(record.Sections[0], "本地 Codex 正在等待你补充参数或说明。") {
		t.Fatalf("expected request_user_input sections in state, got %#v", record)
	}
}

func TestRespondRequestUserInputOptionDispatchesAnswersAndKeepsPendingStateUntilResolved(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 0),
		RequestAnswers: map[string][]string{
			"model": []string{"gpt-5.4"},
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed inline replacement plus one agent command event, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed {
		t.Fatalf("expected completed request to render sealed prompt, got %#v", prompt)
	}
	answers, _ := events[1].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("unexpected request user input response payload: %#v", events[1].Command.Request.Response)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.PendingDispatchCommandID == "" || record.PendingDispatchCommandID != events[1].Command.CommandID {
		t.Fatalf("expected request state to remain pending dispatch until resolved, got %#v", record)
	}
}

func TestRespondRequestUserInputRejectsInvalidOptionAnswer(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"not-valid"},
		}, 0),
		RequestAnswers: map[string][]string{
			"model": []string{"not-valid"},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_invalid" {
		t.Fatalf("expected request_invalid notice, got %#v", events)
	}
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 1 {
		t.Fatalf("expected invalid submit to keep pending request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
}

func TestRespondRequestUserInputSavesPartialAnswersUntilComplete(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "effort", "header": "推理强度", "question": "请选择推理强度", "options": []map[string]any{{"label": "high"}, {"label": "medium"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 0),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected partial answer save to refresh current card inline, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestRevision != 2 {
		t.Fatalf("expected partial answer save to bump prompt revision, got %#v", prompt)
	}
	if prompt.CurrentQuestionIndex != 1 {
		t.Fatalf("expected partial answer save to advance to next question, got %#v", prompt)
	}
	if !prompt.Questions[0].Answered || prompt.Questions[0].DefaultValue != "gpt-5.4" {
		t.Fatalf("expected refreshed prompt to show saved answer, got %#v", prompt.Questions[0])
	}
	pending := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if pending == nil || pending.DraftAnswers["model"] != "gpt-5.4" {
		t.Fatalf("expected partial answer to persist in pending request, got %#v", pending)
	}
	if pending.CardRevision != 2 {
		t.Fatalf("expected partial answer save to bump record revision, got %#v", pending)
	}
	if pending.PendingDispatchCommandID != "" {
		t.Fatalf("expected partial answer save to keep request editable, got %#v", pending)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"effort": {"high"},
		}, 0),
		RequestAnswers: map[string][]string{
			"effort": {"high"},
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected final answer to seal current card and dispatch command, got %#v", events)
	}
	prompt = requestPromptFromEvent(t, events[0])
	if !prompt.Sealed {
		t.Fatalf("expected final answer to render sealed prompt, got %#v", prompt)
	}
	answers, _ := events[1].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("expected merged model answer in response payload, got %#v", events[1].Command.Request.Response)
	}
	if _, ok := answers["effort"]; !ok {
		t.Fatalf("expected merged effort answer in response payload, got %#v", events[1].Command.Request.Response)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.PendingDispatchCommandID == "" {
		t.Fatalf("expected pending request to remain until response resolves, got %#v", record)
	}
}

func TestRespondRequestUserInputMergesSavedOptionWithFormTextAnswer(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "notes", "header": "备注", "question": "补充说明"},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 0),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected option-only partial submit to refresh current card inline, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.CurrentQuestionIndex != 1 {
		t.Fatalf("expected option-only partial submit to advance to form question, got %#v", prompt)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"notes": {"请用中文回复"},
		}, 0),
		RequestAnswers: map[string][]string{
			"notes": {"请用中文回复"},
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected merged submit to seal current card and dispatch command, got %#v", events)
	}
	prompt = requestPromptFromEvent(t, events[0])
	if !prompt.Sealed {
		t.Fatalf("expected merged submit to render sealed prompt, got %#v", prompt)
	}
	answers, _ := events[1].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("expected saved model answer in merged response payload, got %#v", events[1].Command.Request.Response)
	}
	if _, ok := answers["notes"]; !ok {
		t.Fatalf("expected notes answer in merged response payload, got %#v", events[1].Command.Request.Response)
	}
}

func TestRespondRequestUserInputSkipOptionalDispatchesStoredAnswers(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "notes", "header": "备注", "question": "补充说明", "optional": true, "isOther": true},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 0),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected first answer to refresh current card inline, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.CurrentQuestionIndex != 1 {
		t.Fatalf("expected first answer to advance to optional question, got %#v", prompt)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionControlRequest,
		SurfaceSessionID: "surface-1",
		RequestControl: &control.ActionRequestControl{
			RequestID:       "req-ui-1",
			RequestType:     "request_user_input",
			Control:         "skip_optional",
			QuestionID:      "notes",
			RequestRevision: 2,
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected skipping optional question to seal card and dispatch response, got %#v", events)
	}
	prompt = requestPromptFromEvent(t, events[0])
	if !prompt.Sealed {
		t.Fatalf("expected skip completion to render sealed prompt, got %#v", prompt)
	}
	answers, _ := events[1].Command.Request.Response["answers"].(map[string]any)
	model, _ := answers["model"].(map[string]any)
	modelList, _ := model["answers"].([]string)
	if len(modelList) != 1 || modelList[0] != "gpt-5.4" {
		t.Fatalf("expected answered question to keep selected answer, got %#v", answers["model"])
	}
	if _, ok := answers["notes"]; ok {
		t.Fatalf("expected skipped optional question to stay absent from response, got %#v", answers)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.PendingDispatchCommandID == "" || !record.SkippedQuestionIDs["notes"] {
		t.Fatalf("expected pending request to remain sealed until resolve, got %#v", record)
	}
}

func TestRespondRequestUserInputDispatchFailureRestoresPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 1),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected submit to seal current card and dispatch a command, got %#v", events)
	}
	restore := svc.HandleCommandDispatchFailure("surface-1", events[1].Command.CommandID, errors.New("relay unavailable"))
	if len(restore) != 2 || restore[1].Notice == nil {
		t.Fatalf("expected prompt refresh and notice after dispatch failure, got %#v", restore)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.PendingDispatchCommandID != "" || record.CardRevision != 3 {
		t.Fatalf("expected request to be restored with bumped revision, got %#v", record)
	}
	prompt := requestPromptFromEvent(t, restore[0])
	if prompt.RequestRevision != 3 {
		t.Fatalf("expected refreshed prompt revision, got %#v", prompt)
	}
	if restore[1].Notice.Code != "dispatch_failed" {
		t.Fatalf("expected dispatch_failed notice, got %#v", restore[1].Notice)
	}
}

func TestRespondRequestUserInputRejectsStaleRequestRevision(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}}},
				{"id": "effort", "header": "推理强度", "question": "请选择推理强度", "options": []map[string]any{{"label": "high"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 1),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected first answer to refresh current card inline, got %#v", events)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.CardRevision != 2 || record.CurrentQuestionIndex != 1 {
		t.Fatalf("expected first answer to advance request revision and current question, got %#v", record)
	}
	stale := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"effort": {"high"},
		}, 1),
		RequestAnswers: map[string][]string{
			"effort": {"high"},
		},
	})
	if len(stale) != 1 || stale[0].Notice == nil || stale[0].Notice.Code != "request_card_expired" {
		t.Fatalf("expected stale request revision to be rejected, got %#v", stale)
	}
}

func TestRespondRequestUserInputCommandRejectedRestoresPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-ui-1", "", "", map[string][]string{
			"model": {"gpt-5.4"},
		}, 1),
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected submit to seal current card and dispatch a command, got %#v", events)
	}
	restore := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: events[1].Command.CommandID,
		Accepted:  false,
		Error:     "translator failed",
	})
	if len(restore) != 2 || restore[1].Notice == nil {
		t.Fatalf("expected prompt refresh and notice after command reject, got %#v", restore)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.PendingDispatchCommandID != "" || record.CardRevision != 3 {
		t.Fatalf("expected request to be restored after command reject, got %#v", record)
	}
	if restore[1].Notice.Code != "command_rejected" {
		t.Fatalf("expected command_rejected notice, got %#v", restore[1].Notice)
	}
}

func TestRespondRequestUserInputCancelTurnClearsRequestAndInterruptsTurn(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "notes", "header": "备注", "question": "补充说明", "optional": true, "isOther": true},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionControlRequest,
		SurfaceSessionID: "surface-1",
		RequestControl: &control.ActionRequestControl{
			RequestID:   "req-ui-1",
			RequestType: "request_user_input",
			Control:     "cancel_turn",
		},
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected cancel_turn to replace current card and send interrupt, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || !strings.Contains(prompt.StatusText, "发送停止请求") {
		t.Fatalf("expected cancel_turn to render sealed terminal prompt, got %#v", prompt)
	}
	if events[1].Command.Kind != agentproto.CommandTurnInterrupt {
		t.Fatalf("expected cancel_turn to send interrupt command, got %#v", events[1].Command)
	}
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected cancel_turn to clear pending request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
}

func TestRespondRequestUserInputRejectsSkipOnRequiredQuestion(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "effort", "header": "推理强度", "question": "请选择推理强度", "options": []map[string]any{{"label": "high"}, {"label": "medium"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionControlRequest,
		SurfaceSessionID: "surface-1",
		RequestControl: &control.ActionRequestControl{
			RequestID:   "req-ui-1",
			RequestType: "request_user_input",
			Control:     "skip_optional",
			QuestionID:  "model",
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_invalid" {
		t.Fatalf("expected skipping required question to be rejected, got %#v", events)
	}
}

func TestRespondRequestUserInputRejectsStaleRequestControl(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-ui-1",
		Metadata: map[string]any{
			"requestType": "request_user_input",
			"questions": []map[string]any{
				{"id": "model", "header": "模型", "question": "请选择模型", "options": []map[string]any{{"label": "gpt-5.4"}, {"label": "gpt-5.3"}}},
				{"id": "effort", "header": "推理强度", "question": "请选择推理强度", "options": []map[string]any{{"label": "high"}, {"label": "medium"}}},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionControlRequest,
		SurfaceSessionID: "surface-1",
		RequestControl: &control.ActionRequestControl{
			RequestID:       "req-ui-1",
			RequestType:     "request_user_input",
			Control:         "cancel_turn",
			RequestRevision: 99,
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_card_expired" {
		t.Fatalf("expected stale request control to be rejected, got %#v", events)
	}
}

func TestRequestPromptQuestionsToControlHidesSecretDefaultWhileKeepingAnsweredState(t *testing.T) {
	questions := []state.RequestPromptQuestionRecord{
		{
			ID:       "model",
			Header:   "模型",
			Question: "请选择模型",
		},
		{
			ID:       "token",
			Header:   "令牌",
			Question: "请输入令牌",
			Secret:   true,
		},
	}
	drafts := map[string]string{
		"model": "gpt-5.4",
		"token": "secret-token",
	}

	got := requestPromptQuestionsToControl(questions, drafts, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 converted questions, got %#v", got)
	}
	if !got[0].Answered || got[0].DefaultValue != "gpt-5.4" {
		t.Fatalf("expected non-secret question to keep answered default value, got %#v", got[0])
	}
	if !got[1].Answered || got[1].DefaultValue != "" {
		t.Fatalf("expected secret question to keep answered=true but hide default value, got %#v", got[1])
	}
}

func TestPendingRequestBlocksTextUntilHandled(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-1",
		Text:             "你先看看当前有哪些文件没有提交",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected request_pending notice, got %#v", events)
	}
	if len(svc.root.Surfaces["surface-1"].QueueItems) != 0 {
		t.Fatalf("expected no queued text while request is pending, got %#v", svc.root.Surfaces["surface-1"].QueueItems)
	}
}

func TestCaptureFeedbackQueuesFollowupAndDeclinesRequest(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveThreadID:          "thread-1",
		ActiveTurnID:            "turn-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	startCapture := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-1", "", "captureFeedback", nil, 0),
	})
	if len(startCapture) != 1 || startCapture[0].Notice == nil || startCapture[0].Notice.Code != "request_capture_started" {
		t.Fatalf("expected request_capture_started notice, got %#v", startCapture)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture == nil {
		t.Fatalf("expected active request capture to be created")
	}

	feedback := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-2",
		Text:             "不要直接执行，先列一下当前未提交文件，再继续。",
	})
	var command *agentproto.Command
	var pending *control.PendingInputState
	var gotNotice bool
	for _, event := range feedback {
		if event.Command != nil {
			command = event.Command
		}
		if event.PendingInput != nil {
			pending = event.PendingInput
		}
		if event.Notice != nil && event.Notice.Code == "request_feedback_queued" {
			gotNotice = true
		}
	}
	if command == nil || command.Request.Response["decision"] != "decline" {
		t.Fatalf("expected decline command from captured feedback, got %#v", feedback)
	}
	if pending == nil || pending.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued follow-up input, got %#v", feedback)
	}
	if !gotNotice {
		t.Fatalf("expected feedback queued notice, got %#v", feedback)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveRequestCapture != nil {
		t.Fatalf("expected capture to be cleared, got %#v", surface.ActiveRequestCapture)
	}
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected one queued follow-up item, got %#v", surface.QueuedQueueItemIDs)
	}
	item := surface.QueueItems[surface.QueuedQueueItemIDs[0]]
	if item == nil || queuedItemExecutionThreadID(item) != "thread-1" {
		t.Fatalf("expected queued follow-up to stay on original thread, got %#v", item)
	}
	if item.SourceMessageID != "om-msg-2" || len(item.Inputs) != 1 || item.Inputs[0].Text == "" {
		t.Fatalf("unexpected follow-up queue item: %#v", item)
	}
}

func TestTickExpiresRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		PendingRequests:  map[string]*state.RequestPromptRecord{},
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		ActiveRequestCapture: &state.RequestCaptureRecord{
			RequestID: "req-1",
			Mode:      requestCaptureModeDeclineWithFeedback,
			CreatedAt: now.Add(-11 * time.Minute),
			ExpiresAt: now.Add(-1 * time.Second),
		},
	}

	events := svc.Tick(now)
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_expired" {
		t.Fatalf("expected request capture expiry notice, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture != nil {
		t.Fatalf("expected expired capture to be cleared")
	}
}

func TestThreadTitleUsesUnnamedPlaceholderWhenNameAndUserMessagesMissing(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:   "dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		WorkspaceRoot: "/data/dl",
	}, &state.ThreadRecord{
		ThreadID: "019d5679-370c-7b03-b86f-15a33a017c83",
		Preview:  "当前目录 `/data/dl` 下的内容如下：",
		CWD:      "/data/dl",
	}, "019d5679-370c-7b03-b86f-15a33a017c83")

	if title != "dl · 未命名会话" {
		t.Fatalf("expected unnamed placeholder fallback, got %q", title)
	}
}

func TestThreadTitleUsesThreadWorkspaceSuffixOverInstanceShortName(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:   "dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		WorkspaceRoot: "/data/dl",
	}, &state.ThreadRecord{
		ThreadID: "thread-1",
		Name:     "修复 relay 队列背压",
		CWD:      "/data/dl/atlas-admin",
	}, "thread-1")

	if title != "atlas-admin · 修复 relay 队列背压" {
		t.Fatalf("expected thread cwd basename to drive title prefix, got %q", title)
	}
}

func TestThreadTitleSkipsPlaceholderNameWithoutUsingPreview(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:   "atlas-admin",
		WorkspaceKey:  "/data/dl/atlas-admin",
		ShortName:     "atlas-admin",
		WorkspaceRoot: "/data/dl/atlas-admin",
	}, &state.ThreadRecord{
		ThreadID: "thread-1",
		Name:     "新会话",
		Preview:  "0123456789012345678901234567890123456789XYZ",
		CWD:      "/data/dl/atlas-admin",
	}, "thread-1")

	if title != "atlas-admin · 未命名会话" {
		t.Fatalf("expected placeholder name to fall back to unnamed session, got %q", title)
	}
}

func TestThreadSelectionButtonLabelUsesUnnamedPlaceholderWithoutPreviewFallback(t *testing.T) {
	label := threadSelectionButtonLabel(&state.ThreadRecord{
		ThreadID: "thread-1",
		Name:     "新会话",
		Preview:  "01234567890123456789XYZ",
		CWD:      "/data/dl/atlas-admin",
	}, "thread-1")

	if label != "atlas-admin · 未命名会话" {
		t.Fatalf("expected selection label to use unnamed placeholder, got %q", label)
	}
}

func TestThreadDiscoveredDoesNotEraseExistingName(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid",
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.Name != "修复登录流程" {
		t.Fatalf("expected existing thread name to be preserved, got %#v", thread)
	}
}

func TestThreadFocusRequestsMetadataRefreshOnlyOnce(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
	})
	if len(first) != 1 {
		t.Fatalf("expected refresh command only, got %#v", first)
	}
	if first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected threads refresh command, got %#v", first[0])
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
	})
	if len(second) != 0 {
		t.Fatalf("expected no duplicate UI events, got %#v", second)
	}
}

func TestThreadFocusSkipsMetadataRefreshWhenInstanceLacksCapability(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "claude",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
	})
	if len(events) != 0 {
		t.Fatalf("expected no refresh command when capability is absent, got %#v", events)
	}
}

func TestDigitsTextAfterShowingThreadsIsSentAsNormalMessage(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionShowThreads, SurfaceSessionID: "surface-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "1",
	})

	if len(events) != 3 || events[0].PendingInput == nil || events[1].PendingInput == nil || events[2].Command == nil {
		t.Fatalf("expected normal queued message flow, got %#v", events)
	}
	if events[2].Command.Kind != agentproto.CommandPromptSend || events[2].Command.Prompt.Inputs[0].Text != "1" {
		t.Fatalf("expected digits to be sent as normal text, got %#v", events[2].Command)
	}
}
