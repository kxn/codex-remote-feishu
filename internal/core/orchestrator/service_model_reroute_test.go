package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTurnModelReroutedUpdatesThreadModelAndSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 16, 15, 0, 0, 0, time.UTC)
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
			"thread-1": {
				ThreadID:      "thread-1",
				Name:          "修复登录流程",
				CWD:           "/data/dl/droid",
				ExplicitModel: "gpt-5.4",
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.root.Surfaces["surface-1"].SelectedThreadID = "thread-1"

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnModelRerouted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ModelReroute: &agentproto.TurnModelReroute{
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			FromModel: "gpt-5.4",
			ToModel:   "gpt-5.4-mini",
			Reason:    "safety_downgrade",
		},
	}); len(events) != 0 {
		t.Fatalf("expected no direct UI events, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil {
		t.Fatal("expected thread record to exist")
	}
	if thread.ExplicitModel != "gpt-5.4-mini" {
		t.Fatalf("expected effective model to switch to rerouted target, got %#v", thread)
	}
	if thread.LastModelReroute == nil {
		t.Fatalf("expected thread to retain reroute payload, got %#v", thread)
	}
	if thread.LastModelReroute.FromModel != "gpt-5.4" || thread.LastModelReroute.ToModel != "gpt-5.4-mini" || thread.LastModelReroute.TurnID != "turn-1" {
		t.Fatalf("unexpected stored reroute payload: %#v", thread.LastModelReroute)
	}

	snapshot := svc.buildSnapshot(svc.root.Surfaces["surface-1"])
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.Attachment.SelectedThreadModelReroute == nil {
		t.Fatalf("expected selected thread reroute in attachment snapshot, got %#v", snapshot.Attachment)
	}
	if snapshot.Attachment.SelectedThreadModelReroute.ToModel != "gpt-5.4-mini" {
		t.Fatalf("unexpected attachment reroute payload: %#v", snapshot.Attachment.SelectedThreadModelReroute)
	}
	if len(snapshot.Threads) != 1 {
		t.Fatalf("expected one thread in snapshot, got %#v", snapshot.Threads)
	}
	if snapshot.Threads[0].Model != "gpt-5.4-mini" {
		t.Fatalf("expected thread summary model to reflect reroute target, got %#v", snapshot.Threads[0])
	}
	if snapshot.Threads[0].LastModelReroute == nil || snapshot.Threads[0].LastModelReroute.Reason != "safety_downgrade" {
		t.Fatalf("expected thread summary reroute payload, got %#v", snapshot.Threads[0])
	}
}
