package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestRemoteRequestPromptCarriesTurnReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	setupAutoContinueSurface(t, svc)
	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	if len(events) != 1 || events[0].FeishuDirectRequestPrompt == nil {
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
	surface := setupAutoContinueSurface(t, svc)
	startRemoteTurnForAutoContinueTest(t, svc, "msg-1", "处理一下", "turn-1")

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
	var finalEvent *control.UIEvent
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
