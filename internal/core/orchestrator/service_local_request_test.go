package orchestrator

import (
	"reflect"
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
	if len(events) != 1 || events[0].FeishuDirectRequestPrompt == nil {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := events[0].FeishuDirectRequestPrompt
	if prompt.RequestID != "req-1" || prompt.ThreadTitle != "droid · 修复登录流程" {
		t.Fatalf("unexpected request prompt: %#v", prompt)
	}
	if len(prompt.Options) != 3 || prompt.Options[0].OptionID != "accept" || prompt.Options[1].OptionID != "decline" || prompt.Options[2].OptionID != "captureFeedback" {
		t.Fatalf("unexpected request options: %#v", prompt.Options)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.RequestType != "approval" {
		t.Fatalf("expected pending request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
	if len(record.Options) != 3 {
		t.Fatalf("expected request options in state, got %#v", record)
	}
	if events[0].FeishuRequestContext == nil {
		t.Fatalf("expected feishu request context, got %#v", events[0])
	}
	if events[0].FeishuRequestContext.DTOOwner != control.FeishuUIDTOwnerDirectDTO {
		t.Fatalf("unexpected dto owner: %#v", events[0].FeishuRequestContext)
	}
	if events[0].FeishuRequestContext.RequestID != "req-1" || events[0].FeishuRequestContext.RequestType != "approval" {
		t.Fatalf("unexpected request context payload: %#v", events[0].FeishuRequestContext)
	}
	if !events[0].FeishuRequestContext.Surface.RouteMutationBlocked || events[0].FeishuRequestContext.Surface.RouteMutationBlockedBy != "pending_request" {
		t.Fatalf("expected pending request to block route mutation, got %#v", events[0].FeishuRequestContext.Surface)
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
		RequestID:        "req-1",
		RequestOptionID:  "accept",
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one agent command event, got %#v", events)
	}
	command := events[0].Command
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
		RequestID:        "req-1",
		RequestOptionID:  "decline",
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
		RequestID:        "req-1",
		RequestOptionID:  "acceptForSession",
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one agent command event, got %#v", events)
	}
	if events[0].Command.Request.Response["decision"] != "acceptForSession" {
		t.Fatalf("unexpected request decision payload: %#v", events[0].Command.Request.Response)
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
	if len(events) != 1 || events[0].FeishuDirectRequestPrompt == nil {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := events[0].FeishuDirectRequestPrompt
	if prompt.RequestType != "request_user_input" || len(prompt.Questions) != 2 {
		t.Fatalf("unexpected request prompt: %#v", prompt)
	}
	if !prompt.Questions[0].DirectResponse || !prompt.Questions[1].Secret {
		t.Fatalf("unexpected request prompt questions: %#v", prompt.Questions)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if record == nil || record.ItemID != "item-1" || len(record.Questions) != 2 {
		t.Fatalf("expected pending request state for request_user_input, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
}

func TestRespondRequestUserInputOptionDispatchesAnswersAndClearsState(t *testing.T) {
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
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"model": []string{"gpt-5.4"},
		},
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one agent command event, got %#v", events)
	}
	answers, _ := events[0].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("unexpected request user input response payload: %#v", events[0].Command.Request.Response)
	}
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected request state to clear immediately after submit, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
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
		RequestID:        "req-ui-1",
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
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_saved" {
		t.Fatalf("expected partial request_saved notice, got %#v", events)
	}
	pending := svc.root.Surfaces["surface-1"].PendingRequests["req-ui-1"]
	if pending == nil || pending.DraftAnswers["model"] != "gpt-5.4" {
		t.Fatalf("expected partial answer to persist in pending request, got %#v", pending)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"effort": {"high"},
		},
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected final answer to dispatch command, got %#v", events)
	}
	answers, _ := events[0].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("expected merged model answer in response payload, got %#v", events[0].Command.Request.Response)
	}
	if _, ok := answers["effort"]; !ok {
		t.Fatalf("expected merged effort answer in response payload, got %#v", events[0].Command.Request.Response)
	}
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected pending request to clear after complete answers, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
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
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_saved" {
		t.Fatalf("expected option-only partial submit to be saved, got %#v", events)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"notes": {"请用中文回复"},
		},
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected merged submit to dispatch command, got %#v", events)
	}
	answers, _ := events[0].Command.Request.Response["answers"].(map[string]any)
	if _, ok := answers["model"]; !ok {
		t.Fatalf("expected saved model answer in merged response payload, got %#v", events[0].Command.Request.Response)
	}
	if _, ok := answers["notes"]; !ok {
		t.Fatalf("expected notes answer in merged response payload, got %#v", events[0].Command.Request.Response)
	}
}

func TestRespondRequestUserInputAllowsSubmitWithUnansweredAfterConfirm(t *testing.T) {
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
		RequestID:        "req-ui-1",
		RequestAnswers: map[string][]string{
			"model": {"gpt-5.4"},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_saved" {
		t.Fatalf("expected first step to save partial answer, got %#v", events)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		RequestID:        "req-ui-1",
		RequestOptionID:  "submit_with_unanswered",
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected submit_with_unanswered to dispatch response, got %#v", events)
	}
	answers, _ := events[0].Command.Request.Response["answers"].(map[string]any)
	model, _ := answers["model"].(map[string]any)
	modelList, _ := model["answers"].([]string)
	if len(modelList) != 1 || modelList[0] != "gpt-5.4" {
		t.Fatalf("expected answered question to keep selected answer, got %#v", answers["model"])
	}
	effort, _ := answers["effort"].(map[string]any)
	effortList, _ := effort["answers"].([]string)
	if len(effortList) != 0 {
		t.Fatalf("expected unanswered question to submit empty answers, got %#v", answers["effort"])
	}
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected pending request to clear after submit_with_unanswered, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
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

	got := requestPromptQuestionsToControl(questions, drafts)
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
		RequestID:        "req-1",
		RequestOptionID:  "captureFeedback",
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
	if item == nil || item.FrozenThreadID != "thread-1" {
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

func TestThreadTitleUsesPreviewSummaryWhenNameMissing(t *testing.T) {
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

	if title != "dl · 当前目录 `/data/dl` 下的内容如下：" {
		t.Fatalf("expected preview summary fallback, got %q", title)
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

func TestThreadTitleSkipsPlaceholderNameAndTruncatesPreview(t *testing.T) {
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

	if title != "atlas-admin · 0123456789012345678901234567890123456789..." {
		t.Fatalf("expected placeholder name to fall back to truncated preview, got %q", title)
	}
}

func TestThreadSelectionButtonLabelUsesWorkspaceSuffixAndPreviewFallback(t *testing.T) {
	label := threadSelectionButtonLabel(&state.ThreadRecord{
		ThreadID: "thread-1",
		Name:     "新会话",
		Preview:  "01234567890123456789XYZ",
		CWD:      "/data/dl/atlas-admin",
	}, "thread-1")

	if label != "atlas-admin · 01234567890123456789..." {
		t.Fatalf("expected selection label to include workspace suffix and truncated preview, got %q", label)
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

func TestLegacyPromptSelectionActionShowsExpiredNotice(t *testing.T) {
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
	useEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSelectPrompt,
		SurfaceSessionID: "surface-1",
		PromptID:         "prompt-1",
		OptionID:         "thread-1",
	})
	if len(useEvents) != 1 || useEvents[0].Notice == nil || useEvents[0].Notice.Code != "selection_expired" {
		t.Fatalf("expected selection_expired notice for legacy prompt action, got %#v", useEvents)
	}
}

func TestHelpActionBuildsCommandCatalogEvent(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandHelp,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})

	if len(events) != 1 || events[0].FeishuDirectCommandCatalog == nil {
		t.Fatalf("expected command catalog event, got %#v", events)
	}
	if events[0].Kind != control.UIEventFeishuDirectCommandCatalog {
		t.Fatalf("unexpected event kind: %#v", events[0])
	}
	if events[0].FeishuDirectCommandCatalog.Interactive {
		t.Fatalf("help catalog should be non-interactive: %#v", events[0].FeishuDirectCommandCatalog)
	}
	if events[0].FeishuDirectCommandCatalog.Title != "Slash 命令帮助" {
		t.Fatalf("unexpected help catalog title: %#v", events[0].FeishuDirectCommandCatalog)
	}
}

func TestMenuActionBuildsInteractiveCommandCatalogEvent(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected interactive command catalog event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if !catalog.Interactive {
		t.Fatalf("menu catalog should be interactive: %#v", catalog)
	}
	if catalog.DisplayStyle != control.CommandCatalogDisplayCompactButtons {
		t.Fatalf("menu catalog should use compact button display: %#v", catalog)
	}
	if catalog.Title != "命令菜单" {
		t.Fatalf("unexpected menu catalog title: %#v", catalog)
	}
	if events[0].FeishuCommandContext == nil {
		t.Fatalf("expected feishu command context, got %#v", events[0])
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Menu == nil {
		t.Fatalf("expected feishu command view menu payload, got %#v", events[0].FeishuCommandView)
	}
	if events[0].FeishuCommandContext.DTOOwner != control.FeishuUIDTOwnerCommand {
		t.Fatalf("unexpected dto owner: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.ViewKind != "menu" || events[0].FeishuCommandContext.MenuStage != "detached" {
		t.Fatalf("unexpected command context: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.Surface.CallbackPayloadOwner != control.FeishuUICallbackPayloadOwnerAdapter {
		t.Fatalf("unexpected callback payload owner: %#v", events[0].FeishuCommandContext)
	}
	if events[0].FeishuCommandContext.Surface.InlineReplaceFreshness != "daemon_lifecycle" || !events[0].FeishuCommandContext.Surface.InlineReplaceRequiresFreshness {
		t.Fatalf("unexpected inline replace context: %#v", events[0].FeishuCommandContext.Surface)
	}
	if events[0].FeishuCommandContext.Surface.InlineReplaceViewSession != "surface_state_rederived" || events[0].FeishuCommandContext.Surface.InlineReplaceRequiresViewState {
		t.Fatalf("unexpected inline replace view/session context: %#v", events[0].FeishuCommandContext.Surface)
	}
}

func TestMenuActionDetachedHomepagePrioritizesListUseStatus(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	if len(events) != 1 {
		t.Fatalf("expected command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.Sections) < 2 || catalog.Sections[0].Title != "全部分组" || catalog.Sections[1].Title != "常用操作" {
		t.Fatalf("unexpected detached home catalog: %#v", catalog)
	}
	got := firstCommands(catalog.Sections[1].Entries)
	want := []string{"/list", "/use", "/status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detached home commands = %#v, want %#v", got, want)
	}
}

func TestMenuActionNormalHomepageHidesFollow(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[1].Entries)
	want := []string{"/stop", "/new", "/reasoning", "/model", "/access"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normal home commands = %#v, want %#v", got, want)
	}
	for _, command := range got {
		if command == "/follow" {
			t.Fatalf("normal home should not expose /follow: %#v", got)
		}
	}
}

func TestMenuActionVSCodeHomepageKeepsFollowBehindSettings(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	got := firstCommands(catalog.Sections[1].Entries)
	want := []string{"/stop", "/reasoning", "/model", "/access", "/follow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vscode home commands = %#v, want %#v", got, want)
	}
}

func TestMenuSubmenuShowsReturnToPreviousLevelButton(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		GatewayID:        "app-1",
		Text:             "/menu send_settings",
	})
	if len(events) != 1 {
		t.Fatalf("expected command catalog, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.RelatedButtons) != 1 || catalog.RelatedButtons[0].CommandText != "/menu" {
		t.Fatalf("submenu should expose a back button to /menu, got %#v", catalog.RelatedButtons)
	}
}

func TestBareReasoningCommandBuildsParameterCard(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning",
	})
	if len(events) != 1 {
		t.Fatalf("expected reasoning command catalog, got %#v", events)
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandReasoning {
		t.Fatalf("expected reasoning command view, got %#v", events[0].FeishuCommandView)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Title != "推理强度" {
		t.Fatalf("unexpected reasoning catalog title: %#v", catalog)
	}
	if len(catalog.Breadcrumbs) != 3 || catalog.Breadcrumbs[1].Label != "发送设置" {
		t.Fatalf("unexpected breadcrumbs: %#v", catalog.Breadcrumbs)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 5 || buttons[0].CommandText != "/reasoning low" || buttons[4].CommandText != "/reasoning clear" {
		t.Fatalf("unexpected reasoning buttons: %#v", buttons)
	}
	if len(catalog.Sections) < 2 || catalog.Sections[1].Entries[0].Form == nil {
		t.Fatalf("expected reasoning card to expose manual form input, got %#v", catalog.Sections)
	}
}

func TestBareModelCommandBuildsPresetAndFormCard(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model",
	})
	if len(events) != 1 {
		t.Fatalf("expected model catalog, got %#v", events)
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandModel {
		t.Fatalf("expected model command view, got %#v", events[0].FeishuCommandView)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if len(catalog.Sections) != 2 {
		t.Fatalf("expected preset + manual sections, got %#v", catalog.Sections)
	}
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 2 || buttons[0].CommandText != "/model gpt-5.4" || buttons[1].CommandText != "/model gpt-5.4-mini" {
		t.Fatalf("unexpected model preset buttons: %#v", buttons)
	}
	manual := catalog.Sections[1].Entries[0]
	if manual.Form == nil || manual.Form.CommandText != "/model" {
		t.Fatalf("expected manual model form, got %#v", manual)
	}
	if svc.root.Surfaces["surface-1"].ActiveCommandCapture != nil {
		t.Fatalf("expected model catalog not to create command capture state")
	}
}

func TestLegacyModelCaptureActionReopensModelCatalogWithoutCaptureState(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStartCommandCapture,
		SurfaceSessionID: "surface-1",
		CommandID:        control.FeishuCommandModel,
	})
	if len(events) != 1 {
		t.Fatalf("expected model catalog, got %#v", events)
	}
	if events[0].FeishuCommandView == nil || events[0].FeishuCommandView.Config == nil || events[0].FeishuCommandView.Config.CommandID != control.FeishuCommandModel {
		t.Fatalf("expected model command view, got %#v", events[0].FeishuCommandView)
	}
	if svc.root.Surfaces["surface-1"].ActiveCommandCapture != nil {
		t.Fatalf("expected legacy capture action not to leave capture state behind")
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if catalog.Sections[1].Entries[0].Form == nil {
		t.Fatalf("expected legacy capture action to reopen model form, got %#v", catalog)
	}
}

func TestLegacyCapturedModelInputAppliesDirectly(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.ActiveCommandCapture = &state.CommandCaptureRecord{
		CommandID: control.FeishuCommandModel,
		CreatedAt: now,
		ExpiresAt: now.Add(10 * time.Minute),
	}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		Text:             "gpt-5.4 high",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected direct model apply notice, got %#v", events)
	}
	if surface.ActiveCommandCapture != nil {
		t.Fatalf("expected legacy command capture to be cleared after text input")
	}
	if surface.PromptOverride.Model != "gpt-5.4" || surface.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("expected captured text to apply model override, got %#v", surface.PromptOverride)
	}
}

func TestConfirmKickThreadActionRequiresThreadID(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	useEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionConfirmKickThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "",
	})
	if len(useEvents) != 1 || useEvents[0].Notice == nil || useEvents[0].Notice.Code != "selection_invalid" {
		t.Fatalf("expected selection_invalid for missing kick thread id, got %#v", useEvents)
	}
}

func TestLocalTurnCompletedWithoutLocalPauseDoesNotEnterHandoff(t *testing.T) {
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(events) != 0 {
		t.Fatalf("expected no UI events for stray local completion, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected dispatch mode to stay normal, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}

	now = now.Add(2 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no resume notice after stray local completion, got %#v", tick)
	}
}
