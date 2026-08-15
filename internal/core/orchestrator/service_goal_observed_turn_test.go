package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func goalActiveThread() map[string]*state.ThreadRecord {
	return map[string]*state.ThreadRecord{
		"thread-1": {
			ThreadID: "thread-1",
			Name:     "修复登录流程",
			CWD:      "/data/dl/droid",
			Loaded:   true,
			ThreadGoal: &agentproto.ThreadGoalUpdate{
				ThreadID:  "thread-1",
				Objective: "ship it",
				Status:    "active",
			},
		},
	}
}

func TestUnknownTurnStartedOnActiveGoalThreadBecomesObservedActiveTurn(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads = goalActiveThread()
	svc.UpsertInstance(inst)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-goal-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	instance := svc.root.Instances["inst-1"]
	if instance.ActiveTurnID != "turn-goal-1" || instance.ActiveThreadID != "thread-1" {
		t.Fatalf("expected backend-observed goal turn to become active turn, got %q/%q", instance.ActiveThreadID, instance.ActiveTurnID)
	}
}

func TestUnknownTurnStartedWithoutActiveGoalIsNotTracked(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{
		ThreadID:  "thread-1",
		Objective: "ship it",
		Status:    "paused",
	}
	svc.UpsertInstance(inst)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-goal-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	instance := svc.root.Instances["inst-1"]
	if instance.ActiveTurnID != "" {
		t.Fatalf("paused goal thread must not be tracked as observed goal turn, got %q", instance.ActiveTurnID)
	}
}

func TestObservedGoalTurnClearedOnCompletion(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads = goalActiveThread()
	svc.UpsertInstance(inst)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-goal-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-goal-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	instance := svc.root.Instances["inst-1"]
	if instance.ActiveTurnID != "" {
		t.Fatalf("expected observed goal turn to be cleared on completion, got %q", instance.ActiveTurnID)
	}
}
