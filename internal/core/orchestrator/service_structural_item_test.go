package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStructuralItemEventsDoNotEmitFeishuMessages(t *testing.T) {
	now := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC)
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

	for _, event := range []agentproto.Event{
		{
			Kind:     agentproto.EventItemTerminalInteraction,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-1",
			ItemKind: "command_execution",
			Metadata: map[string]any{"processId": "proc-1", "stdin": "y\n"},
		},
		{
			Kind:     agentproto.EventItemReasoningSummaryPartAdded,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "reason-1",
			ItemKind: "reasoning_summary",
			Metadata: map[string]any{"summaryIndex": 1},
		},
		{
			Kind:     agentproto.EventItemFileChangePatchUpdated,
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "file-1",
			ItemKind: "file_change",
			FileChanges: []agentproto.FileChangeRecord{{
				Path: "main.go",
				Kind: agentproto.FileChangeUpdate,
				Diff: "@@ -1 +1 @@\n-old\n+new",
			}},
		},
	} {
		if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 0 {
			t.Fatalf("expected structural event %s to stay state-only, got %#v", event.Kind, events)
		}
	}
}

func TestFileChangePatchUpdatedUsesLatestSnapshotForFinalSummary(t *testing.T) {
	now := time.Date(2026, 7, 17, 13, 10, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "处理一下"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemFileChangePatchUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "main.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+mid",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemFileChangePatchUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "main.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
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
		Kind:     agentproto.EventTurnCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Status:   "completed",
	})

	var finalBlockEvent *eventcontract.Event
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
		t.Fatalf("expected final summary from latest patch snapshot, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.FileChangeSummary.AddedLines != 1 || finalBlockEvent.FileChangeSummary.RemovedLines != 1 {
		t.Fatalf("expected latest snapshot counts, not accumulated deltas, got %#v", finalBlockEvent.FileChangeSummary)
	}
}
