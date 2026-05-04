package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestRemoteRequestPromptCarriesTurnReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	setupAutoWhipSurface(t, svc)
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-remote-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	if events[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected request prompt to carry turn reply anchor, got %#v", events[0])
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-remote-1"]
	if record == nil || record.SourceMessageID != "msg-1" {
		t.Fatalf("expected pending request record to retain source message id, got %#v", record)
	}
}

func TestReplayFinalUsesStoredReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "处理一下", "turn-1")

	svc.storeThreadReplayText(svc.root.Instances["inst-1"], "thread-1", "turn-1", "item-1", "这是缓存的 final")
	replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay
	if replay == nil || replay.SourceMessageID != "msg-1" || replay.SourceMessagePreview == "" {
		t.Fatalf("expected replay record to retain reply anchor, got %#v", replay)
	}
	svc.root.Instances["inst-1"].ActiveTurnID = ""

	events := svc.replayThreadUpdate(surface, svc.root.Instances["inst-1"], "thread-1")
	if len(events) == 0 {
		t.Fatalf("expected replay events, got %#v", events)
	}
	var finalEvent *eventcontract.Event
	for i := range events {
		if events[i].Block != nil && events[i].Block.Final {
			finalEvent = &events[i]
			break
		}
	}
	if finalEvent == nil || finalEvent.SourceMessageID != "msg-1" {
		t.Fatalf("expected replayed final block to keep source anchor, got %#v", events)
	}
}

func TestDetachedBranchRequestPromptKeepsReplyAnchorAndSelection(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-main"})
	surface := svc.root.Surfaces["surface-1"]
	startDetachedBranchRemoteTurnForTest(t, svc, surface, "thread-main", "thread-detour", "msg-1", "顺手问个岔题", "turn-detour")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		RequestID: "req-detour-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(events) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	if events[0].SourceMessageID != "msg-1" {
		t.Fatalf("expected detached branch request prompt to keep reply anchor, got %#v", events[0])
	}
	if events[0].RequestView == nil || events[0].RequestView.DetourLabel != detourForkLabel {
		t.Fatalf("expected detached branch request prompt to carry detour label, got %#v", events[0].RequestView)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected detached branch request not to steal selection, got %q", surface.SelectedThreadID)
	}
	record := surface.PendingRequests["req-detour-1"]
	if record == nil || record.SourceMessageID != "msg-1" || record.ThreadID != "thread-detour" {
		t.Fatalf("expected detached branch request record to retain anchor and execution thread, got %#v", record)
	}
}

func TestRemoteRequestPromptCutsSharedProgressSegment(t *testing.T) {
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "go test ./...",
		},
	})
	if len(started) != 1 || started[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial shared progress event, got %#v", started)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "cmd-1", "om-progress-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-remote-1",
		Metadata: map[string]any{
			"requestType": "permissions_request_approval",
			"title":       "需要授予权限",
		},
	})
	if len(events) != 1 || events[0].RequestView == nil {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	if surface.ActiveExecProgress != nil {
		t.Fatalf("expected request prompt to terminate active shared progress segment, got %#v", surface.ActiveExecProgress)
	}

	next := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Status:   "in_progress",
		Metadata: map[string]any{
			"command": "go test ./internal/core/orchestrator",
		},
	})
	if len(next) != 1 || next[0].ExecCommandProgress == nil {
		t.Fatalf("expected new shared progress event after request boundary, got %#v", next)
	}
	progress := next[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "" {
		t.Fatalf("expected fresh shared progress after request boundary instead of patching old card, got %#v", progress)
	}
	if len(progress.Timeline) != 1 || progress.Timeline[0].ID != "cmd-2" {
		t.Fatalf("expected fresh shared progress state after request boundary, got %#v", progress)
	}
}
