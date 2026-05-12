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

func TestNewThreadTurnStartedKeepsSurfaceInPreparedStateUntilCompletion(t *testing.T) {
	now := time.Date(2026, 4, 6, 11, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "开新会话"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected surface to stay prepared until first turn completes, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if surface.PreparedThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected prepared new-thread state to remain, got %#v", surface)
	}
	if claim := svc.threadClaims["thread-created"]; claim != nil {
		t.Fatalf("expected created thread claim to remain uncommitted, got %#v", claim)
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil {
			sawSelection = true
		}
	}
	if sawSelection {
		t.Fatalf("expected no selection change before first turn completion, got %#v", events)
	}
}

func TestNewThreadTurnStartedPromotesPendingRemoteByCommandWhenInitiatorBlank(t *testing.T) {
	now := time.Date(2026, 4, 6, 11, 5, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "开新会话"})
	svc.BindPendingRemoteCommand("surface-1", "cmd-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		CommandID: "cmd-1",
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected blank-initiator turn.started to keep prepared state, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if surface.ActiveTurnOrigin != agentproto.InitiatorRemoteSurface {
		t.Fatalf("expected blank initiator to normalize to remote_surface, got %q", surface.ActiveTurnOrigin)
	}
	if svc.turns.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected pending remote binding to promote, got %#v", svc.turns.pendingRemote["inst-1"])
	}
	if binding := svc.turns.activeRemote["inst-1"]; binding == nil || binding.CommandID != "cmd-1" || binding.ThreadID != "thread-created" || binding.TurnID != "turn-1" {
		t.Fatalf("expected active remote binding to capture command/thread/turn, got %#v", binding)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.Status != state.QueueItemRunning || queuedItemExecutionThreadID(item) != "thread-created" {
		t.Fatalf("expected active queue item to promote into running created thread, got %#v", item)
	}
	var sawPinnedSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-created" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawPinnedSelection = true
		}
	}
	if sawPinnedSelection {
		t.Fatalf("expected no pinned selection change after blank-initiator turn started, got %#v", events)
	}
}

func TestNewThreadCompletionCommitsSurfaceToCreatedThread(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-new",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		ThreadID:             "thread-new",
		TurnID:               "turn-1",
		Status:               "completed",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-new" {
		t.Fatalf("expected successful first turn to commit surface to created thread, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected prepared state to clear after commit, got %#v", surface)
	}
	if claim := svc.threadClaims["thread-new"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected created thread claim to belong to surface, got %#v", claim)
	}
	var sawPinnedSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-new" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawPinnedSelection = true
		}
	}
	if !sawPinnedSelection {
		t.Fatalf("expected pinned selection change on first-turn completion, got %#v", events)
	}
}

func TestNewThreadFailureRestoresPreparedRetryState(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 5, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-new",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		TurnID:               "turn-1",
		Status:               "failed",
		ErrorMessage:         "auth failed",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected failed first turn to restore prepared retry state, got %#v", surface)
	}
	if strings.TrimSpace(surface.PreparedThreadCWD) != "/data/dl/droid" {
		t.Fatalf("expected workspace cwd to remain prepared after failure, got %#v", surface)
	}
	retry := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "再试一次",
	})
	sawCreateThread := false
	for _, event := range retry {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && event.Command.Target.CreateThreadIfMissing {
			sawCreateThread = true
		}
	}
	if !sawCreateThread {
		t.Fatalf("expected retry text to create a new thread again, got %#v", retry)
	}
}

func TestNewThreadTurnStartRejectedKeepsCreatedThreadBinding(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 10, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		ThreadID:             "thread-new",
		Status:               "failed",
		ErrorMessage:         "missing model",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginTurnStartRejected,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-new" {
		t.Fatalf("expected turn/start rejection after thread creation to keep created thread binding, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected prepared state to clear after durable thread commit, got %#v", surface)
	}
	var sawPinnedSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-new" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawPinnedSelection = true
		}
	}
	if !sawPinnedSelection {
		t.Fatalf("expected rejection path to still commit selection to created thread, got %#v", events)
	}
}

func TestNewThreadRuntimeFailureAfterDurableThreadCommitKeepsCreatedThreadBinding(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 12, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		ThreadID:             "session-real",
		TurnID:               "turn-1",
		Status:               "failed",
		ErrorMessage:         "upstream auth failed",
		Initiator:            agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime,
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "session-real" {
		t.Fatalf("expected runtime failure after durable thread creation to keep created thread binding, got %#v", surface)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected prepared state to clear after durable thread commit, got %#v", surface)
	}
	var sawPinnedSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "session-real" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawPinnedSelection = true
		}
	}
	if !sawPinnedSelection {
		t.Fatalf("expected runtime-failure path to commit selection to created thread, got %#v", events)
	}
}

func TestNewThreadDispatchFailureRestoresPreparedRetryState(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 15, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})
	svc.BindPendingRemoteCommand("surface-1", "cmd-1")

	svc.HandleCommandDispatchFailure("surface-1", "cmd-1", errors.New("relay unavailable"))

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected dispatch failure to restore prepared retry state, got %#v", surface)
	}
	if strings.TrimSpace(surface.PreparedThreadCWD) != "/data/dl/droid" {
		t.Fatalf("expected workspace cwd to remain prepared after dispatch failure, got %#v", surface)
	}
}

func TestNewThreadCommandRejectedRestoresPreparedRetryState(t *testing.T) {
	now := time.Date(2026, 4, 6, 13, 20, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", WorkspaceKey: "/data/dl/droid"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "直接发一条消息"})
	svc.BindPendingRemoteCommand("surface-1", "cmd-1")

	svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-1",
		Accepted:  false,
		Error:     "translator failed",
		Problem: &agentproto.ErrorInfo{
			Code:      "translate_command_failed",
			Layer:     "wrapper",
			Stage:     "translate_command",
			Message:   "wrapper 无法把 relay 命令转换成 Codex 请求。",
			Details:   "translator failed",
			CommandID: "cmd-1",
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected command rejection to restore prepared retry state, got %#v", surface)
	}
	if strings.TrimSpace(surface.PreparedThreadCWD) != "/data/dl/droid" {
		t.Fatalf("expected workspace cwd to remain prepared after command rejection, got %#v", surface)
	}
}
