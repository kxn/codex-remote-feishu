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

func TestGoalInterlockCommandResultWritesBackThreadGoal(t *testing.T) {
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
	svc.ApplyAgentEvent(instanceID, agentproto.Event{
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
	thread := svc.root.Instances[instanceID].Threads["thread-1"]
	if thread.ThreadGoal == nil || thread.ThreadGoal.Status != "paused" {
		t.Fatalf("expected command result to write back thread goal, got %#v", thread.ThreadGoal)
	}
}

func TestGoalInterlockRevokedOnDetach(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	if svc.goalInterlockLease(instanceID, "thread-1") == nil {
		t.Fatal("expected lease before detach")
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1"})
	if svc.goalInterlockLease(instanceID, "thread-1") != nil {
		t.Fatal("expected lease revoked on detach")
	}
}

func TestGoalInterlockRevokedOnInstanceRemove(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	if svc.goalInterlockLease(instanceID, "thread-1") == nil {
		t.Fatal("expected lease before remove")
	}
	svc.RemoveInstance(instanceID)
	if svc.goalInterlockLease(instanceID, "thread-1") != nil {
		t.Fatal("expected lease revoked on instance remove")
	}
}

func TestGoalInterlockPauseFailureBackoffBlocksRetry(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := threadLifecycleInstance()
	inst.Threads = goalActiveThread()
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "普通消息",
	})
	pause := findAgentCommand(first, agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("expected initial pause command")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventThreadGoalCommandResult,
		CommandID:    pause.CommandID,
		ThreadID:     "thread-1",
		ErrorMessage: "goal set failed",
	})

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-2",
		Text:             "再来一条",
	})
	if findAgentCommand(second, agentproto.CommandThreadGoalSet) != nil {
		t.Fatalf("pause failure must not immediately retry on next message, got %#v", second)
	}
	foundBackoffNotice := false
	for _, event := range second {
		if event.Notice != nil && event.Notice.Code == "goal_pause_backoff" {
			foundBackoffNotice = true
		}
	}
	if !foundBackoffNotice {
		t.Fatalf("expected pause backoff notice, got %#v", second)
	}

	key := goalInterlockKey("inst-1", "thread-1")
	svc.goalPauseBackoff[key] = now.Add(-time.Second)
	third := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-3",
		Text:             "过期重试",
	})
	if findAgentCommand(third, agentproto.CommandThreadGoalSet) == nil {
		t.Fatalf("expected pause retry after backoff expiry, got %#v", third)
	}
}

func TestGoalInterlockReconcileClearsStaleCommandIDs(t *testing.T) {
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
	first := svc.ReconcileGoalInterlocks()
	firstPause := findAgentCommand(first, agentproto.CommandThreadGoalSet)
	if firstPause == nil {
		t.Fatal("expected first pause resend")
	}
	second := svc.ReconcileGoalInterlocks()
	secondPause := findAgentCommand(second, agentproto.CommandThreadGoalSet)
	if secondPause == nil || secondPause.CommandID == firstPause.CommandID {
		t.Fatalf("expected second pause resend with new id, got %#v", second)
	}
	if _, exists := svc.goalInterlockByCommand[firstPause.CommandID]; exists {
		t.Fatal("stale pause command id must be removed from correlation map")
	}
	if _, exists := svc.goalInterlockByCommand["stale-pause"]; exists {
		t.Fatal("restored stale pause command id must be removed on resend")
	}
}
