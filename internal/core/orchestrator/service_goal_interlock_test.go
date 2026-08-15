package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func goalInterlockTestSetup(t *testing.T) (*Service, string, string) {
	t.Helper()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads = goalActiveThread()
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	return svc, "surface-1", "inst-1"
}

func findAgentCommand(events []eventcontract.Event, kind agentproto.CommandKind) *agentproto.Command {
	for _, event := range events {
		if event.Kind == eventcontract.KindAgentCommand && event.Command != nil && event.Command.Kind == kind {
			return event.Command
		}
	}
	return nil
}

func TestGoalInterlockPauseOnFirstQueueMessage(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil || pause.Goal.Status != "paused" || pause.Goal.Purpose != "queue_interlock" {
		t.Fatalf("expected goal pause command on first queue message, got %#v", events)
	}
	lease := svc.goalInterlockLease(instanceID, "thread-1")
	if lease == nil || lease.Phase != GoalInterlockPausePending || lease.PauseCommandID != pause.CommandID {
		t.Fatalf("unexpected interlock lease: %#v", lease)
	}
	surface := svc.root.Surfaces[surfaceID]
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected queue item to stay queued, got %#v", surface.QueuedQueueItemIDs)
	}
}

func TestGoalInterlockPauseAckQuiescesThenDrainsOnIdle(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("expected pause command")
	}

	resultEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: pause.CommandID,
		ThreadID:  "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:  "thread-1",
			Status:    "paused",
			CreatedAt: 1710000000123,
			UpdatedAt: 1710000000999,
		},
	})
	probe := findAgentCommand(resultEvents, agentproto.CommandThreadRead)
	if probe == nil {
		t.Fatalf("expected thread/read quiescence probe, got %#v", resultEvents)
	}
	lease := svc.goalInterlockLease(instanceID, "thread-1")
	if lease == nil || lease.Phase != GoalInterlockQuiescing {
		t.Fatalf("expected quiescing lease, got %#v", lease)
	}

	drainEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadRuntimeStatusUpdated,
		CommandID: probe.CommandID,
		ThreadID:  "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{
			Type: agentproto.ThreadRuntimeStatusTypeIdle,
		},
	})
	if findAgentCommand(drainEvents, agentproto.CommandPromptSend) == nil {
		t.Fatalf("expected queued prompt dispatch after idle barrier, got %#v", drainEvents)
	}
	lease = svc.goalInterlockLease(instanceID, "thread-1")
	if lease == nil || lease.Phase != GoalInterlockDraining {
		t.Fatalf("expected draining lease, got %#v", lease)
	}
	_ = surfaceID
}

func TestGoalInterlockPauseFailureReleasesLeaseWithNotice(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("expected pause command")
	}

	resultEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:         agentproto.EventThreadGoalCommandResult,
		CommandID:    pause.CommandID,
		ThreadID:     "thread-1",
		ErrorMessage: "goal set failed",
	})
	if svc.goalInterlockLease(instanceID, "thread-1") != nil {
		t.Fatal("expected lease to be released after pause failure")
	}
	foundNotice := false
	for _, event := range resultEvents {
		if event.Notice != nil && event.Notice.Code == "goal_pause_failed" {
			foundNotice = true
		}
	}
	if !foundNotice {
		t.Fatalf("expected pause failure notice, got %#v", resultEvents)
	}
}

func TestGoalInterlockExternalMutationRevokesAutoResume(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("expected pause command")
	}
	pauseResult := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadGoalCommandResult,
		CommandID: pause.CommandID,
		ThreadID:  "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID: "thread-1",
			Status:   "paused",
		},
	})
	probe := findAgentCommand(pauseResult, agentproto.CommandThreadRead)
	if probe == nil {
		t.Fatal("expected thread read probe after pause ack")
	}
	svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadRuntimeStatusUpdated,
		CommandID: probe.CommandID,
		ThreadID:  "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{
			Type: agentproto.ThreadRuntimeStatusTypeIdle,
		},
	})
	lease := svc.goalInterlockLease(instanceID, "thread-1")
	if lease == nil || lease.Phase != GoalInterlockDraining {
		t.Fatalf("expected draining lease before external mutation, got %#v", lease)
	}

	svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventThreadGoalUpdated,
		ThreadID: "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{
			ThreadID:         "thread-1",
			Status:           "paused",
			ExternalMutation: true,
		},
	})
	lease = svc.goalInterlockLease(instanceID, "thread-1")
	if lease == nil {
		t.Fatal("expected lease to survive during draining with no-resume marker")
	}
	if !lease.ExternalMutationSeen {
		t.Fatal("expected external mutation to mark no auto resume")
	}
	_ = surfaceID
}

func TestGoalObservedTurnSteerableBySteerAll(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-goal-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-queued",
		Text:             "排队消息",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-steer",
	})
	steer := findAgentCommand(events, agentproto.CommandTurnSteer)
	if steer == nil {
		t.Fatalf("expected observed goal turn to be steerable, got %#v", events)
	}
	if steer.Target.ThreadID != "thread-1" || steer.Target.TurnID != "turn-goal-1" {
		t.Fatalf("unexpected steer target: %#v", steer.Target)
	}
}

func TestGoalInterlockReconcileResendsPause(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads = goalActiveThread()
	svc.UpsertInstance(inst)
	svc.RestoreGoalInterlockLeases([]GoalInterlockLease{{
		InstanceID:           "inst-1",
		ThreadID:             "thread-1",
		Phase:                GoalInterlockPausePending,
		TriggerSurfaceID:     "surface-1",
		PauseCommandID:       "stale-pause",
		ExternalMutationSeen: false,
		UpdatedAt:            now,
	}})

	events := svc.ReconcileGoalInterlocks()
	pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if pause == nil || pause.Goal.Status != "paused" {
		t.Fatalf("expected pause resend after restore, got %#v", events)
	}
	lease := svc.goalInterlockLease("inst-1", "thread-1")
	if lease == nil || lease.PauseCommandID == "stale-pause" {
		t.Fatalf("expected refreshed pause command id, got %#v", lease)
	}
}
