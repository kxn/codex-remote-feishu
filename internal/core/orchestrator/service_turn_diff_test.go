package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTurnDiffUpdatedLatestSnapshotEmbedsIntoFinalAssistantBlock(t *testing.T) {
	now := time.Date(2026, 4, 16, 14, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnDiffUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		TurnDiff: "@@ -1 +1 @@\n-old\n+mid",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnDiffUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		TurnDiff: "@@ -1 +1 @@\n-old\n+new",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成修改。"},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		if finished[i].Block != nil && finished[i].Block.Final && finished[i].Block.Text == "已完成修改。" {
			finalBlockEvent = &finished[i]
			break
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected final assistant block, got %#v", finished)
	}
	if finalBlockEvent.TurnDiffSnapshot == nil {
		t.Fatalf("expected final block to carry turn diff snapshot, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.TurnDiffSnapshot.Diff != "@@ -1 +1 @@\n-old\n+new" {
		t.Fatalf("expected latest turn diff snapshot, got %#v", finalBlockEvent.TurnDiffSnapshot)
	}
}

func TestTurnDiffUpdatedEmbedsIntoSyntheticFinalBlockWithFileSummary(t *testing.T) {
	now := time.Date(2026, 4, 16, 14, 5, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnDiffUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		TurnDiff: "@@ -1 +1 @@\n-old\n+new",
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		if finished[i].Block != nil && finished[i].Block.Final {
			finalBlockEvent = &finished[i]
			break
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected synthetic final block, got %#v", finished)
	}
	if finalBlockEvent.FileChangeSummary == nil {
		t.Fatalf("expected synthetic final block to keep file summary, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.TurnDiffSnapshot == nil || finalBlockEvent.TurnDiffSnapshot.Diff != "@@ -1 +1 @@\n-old\n+new" {
		t.Fatalf("expected synthetic final block to carry authoritative turn diff snapshot, got %#v", finalBlockEvent)
	}
}
