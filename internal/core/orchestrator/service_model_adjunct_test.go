package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestTurnModelVerificationStoresStateOnly(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				ExplicitModel: "gpt-5.4",
			},
		},
	})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnModelVerification,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ModelVerification: &agentproto.TurnModelVerification{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Verifications: []agentproto.ModelVerificationRecord{{
				ID:      "verify-1",
				Type:    "account",
				Message: "Sign in again",
			}},
		},
	}); len(events) != 0 {
		t.Fatalf("expected no direct UI events, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.LastModelVerification == nil {
		t.Fatalf("expected verification state, got %#v", thread)
	}
	if thread.LastModelVerification.Verifications[0].ID != "verify-1" {
		t.Fatalf("unexpected verification state: %#v", thread.LastModelVerification)
	}
	if thread.ExplicitModel != "gpt-5.4" || thread.LastModelReroute != nil {
		t.Fatalf("verification must not affect reroute/effective model state: %#v", thread)
	}
}

func TestTurnModelSafetyBufferingStoresStateOnly(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID:      "thread-1",
				ExplicitModel: "gpt-5.4",
			},
		},
	})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventTurnModelSafetyBufferingUpdated,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ModelSafetyBuffering: &agentproto.TurnModelSafetyBuffering{
			ThreadID:        "thread-1",
			TurnID:          "turn-1",
			Model:           "gpt-5.4",
			UseCases:        []string{"coding"},
			Reasons:         []string{"policy"},
			ShowBufferingUI: true,
			FasterModel:     "gpt-5.4-mini",
		},
	}); len(events) != 0 {
		t.Fatalf("expected no direct UI events, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.LastModelSafetyBuffering == nil {
		t.Fatalf("expected safety buffering state, got %#v", thread)
	}
	if thread.LastModelSafetyBuffering.FasterModel != "gpt-5.4-mini" || !thread.LastModelSafetyBuffering.ShowBufferingUI {
		t.Fatalf("unexpected safety buffering state: %#v", thread.LastModelSafetyBuffering)
	}
	if thread.ExplicitModel != "gpt-5.4" || thread.LastModelReroute != nil {
		t.Fatalf("safety buffering must not affect reroute/effective model state: %#v", thread)
	}
}
