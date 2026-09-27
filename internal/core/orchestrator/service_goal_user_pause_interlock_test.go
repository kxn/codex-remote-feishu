package orchestrator

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"testing"
)

func prepareRejectedPresetGoalResume(t *testing.T) (*Service, string, string, *agentproto.Command) {
	t.Helper()
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceID,
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-1",
		Text:             "[sol] 普通消息",
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

	svc.root.Instances[instanceID].ModelCatalog = &agentproto.ModelCatalogSnapshot{}
	drainEvents := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventThreadRuntimeStatusUpdated,
		CommandID: probe.CommandID,
		ThreadID:  "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{
			Type: agentproto.ThreadRuntimeStatusTypeIdle,
		},
	})
	if findAgentCommand(drainEvents, agentproto.CommandPromptSend) != nil {
		t.Fatal("rejected preset still dispatched")
	}
	surface := svc.root.Surfaces[surfaceID]
	if len(surface.QueuedQueueItemIDs) != 0 || surface.ActiveQueueItemID != "" {
		t.Fatalf("queue not drained: %#v", surface)
	}
	get := findAgentCommand(drainEvents, agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatalf("all queued work rejected, Goal has no resume flow: lease=%#v events=%#v", svc.goalInterlockLease(instanceID, "thread-1"), drainEvents)
	}
	return svc, surfaceID, instanceID, get
}
func TestUserPauseAndClearRevokeInFlightGoalResume(t *testing.T) {
	for _, action := range []string{"/goal pause", "/goal clear --confirm"} {
		t.Run(action, func(t *testing.T) {
			svc, surfaceID, instanceID, get := prepareRejectedPresetGoalResume(t)
			user := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: action})
			kind := agentproto.CommandThreadGoalSet
			if action == "/goal clear --confirm" {
				kind = agentproto.CommandThreadGoalClear
			}
			cmd := findAgentCommand(user, kind)
			if cmd == nil || cmd.Goal.Purpose != "user_control" {
				t.Fatal("user command not accepted")
			}
			late := svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused", CreatedAt: 1710000000123}})
			if findAgentCommand(late, agentproto.CommandThreadGoalSet) != nil {
				t.Fatal("late Get overrode user intent")
			}
			if svc.goalInterlockLease(instanceID, "thread-1") != nil {
				t.Fatal("user intent retained resume lease")
			}
			if events := svc.failGoalUserCommand(surfaceID, cmd.CommandID, errors.New("send failed")); len(events) == 0 {
				t.Fatal("user command failure was hidden")
			}
			resumed := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, Text: "/goal resume"})
			if cmd := findAgentCommand(resumed, agentproto.CommandThreadGoalSet); cmd == nil || cmd.Goal.Status != "active" || cmd.Goal.Purpose != "user_control" {
				t.Fatal("explicit resume was blocked")
			}
		})
	}
}
