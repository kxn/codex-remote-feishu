package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestGoalUserControlOverridesQueuedAutoResume(t *testing.T) {
	for _, text := range []string{"/goal pause", "/goal edit ship it", "/goal resume", "/goal clear --confirm", "/goal"} {
		t.Run(text, func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			goal := &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused", CreatedAt: 100, UpdatedAt: 200}
			svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = goal
			svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
				Phase: GoalInterlockDraining, TriggerSurfaceID: surfaceID, GoalCreatedAt: 100, Objective: "ship it", PausedUpdatedAt: 200})
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand,
				SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: text})
			kind := agentproto.CommandThreadGoalSet
			if text == "/goal" {
				kind = agentproto.CommandThreadGoalGet
			} else if text == "/goal clear --confirm" {
				kind = agentproto.CommandThreadGoalClear
			}
			request := findAgentCommand(events, kind)
			if request == nil {
				t.Fatal("expected explicit user Goal command")
			}
			svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
				CommandID: request.CommandID, ThreadID: "thread-1", ThreadGoal: goal})
			resumeEvents := svc.maybeResumeGoalInterlock(instanceID, "thread-1")
			if text == "/goal" {
				if findAgentCommand(resumeEvents, agentproto.CommandThreadGoalGet) == nil {
					t.Fatal("read-only Goal query must preserve the original auto-resume path")
				}
			} else if len(resumeEvents) != 0 {
				t.Fatalf("explicit user control must prevent queued automatic resume, got %#v", resumeEvents)
			}
		})
	}
}

func TestGoalUserPauseKeepsPendingIdleBarrier(t *testing.T) {
	for _, phase := range []GoalInterlockPhase{GoalInterlockPausePending, GoalInterlockQuiescing} {
		t.Run(string(phase), func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage,
				SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "queued", Text: "新的输入"})
			pause := findAgentCommand(events, agentproto.CommandThreadGoalSet)
			if pause == nil {
				t.Fatal("expected queue interlock pause")
			}
			if phase == GoalInterlockQuiescing {
				svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
					CommandID: pause.CommandID, ThreadID: "thread-1",
					ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused"}})
			}
			manual := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand,
				SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal pause"})
			request := findAgentCommand(manual, agentproto.CommandThreadGoalSet)
			if request == nil {
				t.Fatal("expected manual Goal pause")
			}
			svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
				CommandID: request.CommandID, ThreadID: "thread-1",
				ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused"}})
			lease := svc.goalInterlockLease(instanceID, "thread-1")
			if lease == nil || lease.Phase != phase || !lease.ExternalMutationSeen {
				t.Fatalf("manual pause must cancel auto-resume but preserve idle barrier, got %#v", lease)
			}
			if sent := findAgentCommand(svc.dispatchNext(svc.root.Surfaces[surfaceID]), agentproto.CommandPromptSend); sent != nil {
				t.Fatal("manual pause must not release queued input before the idle probe")
			}
		})
	}
}

func TestGoalAutoResumeRequiresStillPausedGoal(t *testing.T) {
	for _, status := range []string{"paused", "complete", "blocked", "active", "budget_limited"} {
		t.Run(status, func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			key := goalInterlockKey(instanceID, "thread-1")
			svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
				Phase: GoalInterlockResumePending, TriggerSurfaceID: surfaceID, GetCommandID: "resume-check", GoalCreatedAt: 100, Objective: "ship it"})
			svc.goalGetByCommand = map[string]string{"resume-check": key}
			events := svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
				CommandID: "resume-check", ThreadID: "thread-1",
				ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: status, CreatedAt: 100}})
			resume := findAgentCommand(events, agentproto.CommandThreadGoalSet)
			if status == "paused" {
				if resume == nil || resume.Goal.Status != "active" {
					t.Fatal("unchanged paused Goal must retain its original automatic restore path")
				}
			} else if resume != nil {
				t.Fatalf("automatic restoration must not reactivate a %s Goal", status)
			}
		})
	}
}

func TestGoalRevokedAutoResumeLeaseEndsAfterQueueDrains(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			svc, surfaceID, instanceID := goalInterlockTestSetup(t)
			svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
				Phase: GoalInterlockDraining, TriggerSurfaceID: surfaceID, ExternalMutationSeen: true})
			if busy {
				svc.root.Surfaces[surfaceID].ActiveQueueItemID = "still-running"
			}
			if events := svc.maybeResumeGoalInterlock(instanceID, "thread-1"); len(events) != 0 {
				t.Fatalf("revoked lease must not restore the Goal: %#v", events)
			}
			if retained := svc.goalInterlockLease(instanceID, "thread-1") != nil; retained != busy {
				t.Fatalf("revoked lease retained=%t, want %t until queue drains", retained, busy)
			}
		})
	}
}

func TestGoalNewInputPausesAgainAfterUserPauseAndResume(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	goal := &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused"}
	svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = goal
	svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
		Phase: GoalInterlockDraining, TriggerSurfaceID: surfaceID, Objective: goal.Objective})
	for _, sub := range []string{"pause", "resume"} {
		events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
			ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal " + sub})
		command := findAgentCommand(events, agentproto.CommandThreadGoalSet)
		if command == nil {
			t.Fatalf("expected manual %s command", sub)
		}
		if sub == "resume" {
			goal.Status = "active"
		}
		svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
			CommandID: command.CommandID, ThreadID: "thread-1", ThreadGoal: goal})
		svc.maybeResumeGoalInterlock(instanceID, "thread-1")
	}
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", MessageID: "next-input", Text: "新的普通输入"})
	if command := findAgentCommand(events, agentproto.CommandThreadGoalSet); command == nil || command.Goal.Status != "paused" {
		t.Fatal("new ordinary input must pause active Goal again after the previous lease ends")
	}
	if command := findAgentCommand(events, agentproto.CommandPromptSend); command != nil {
		t.Fatal("new ordinary input must not bypass the new pause barrier")
	}
}

func TestGoalPendingUserPauseBlocksLateAutoResumeQuery(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	goal := &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "paused"}
	svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal = goal
	svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
		Phase: GoalInterlockDraining, TriggerSurfaceID: surfaceID, Objective: goal.Objective})
	get := findAgentCommand(svc.maybeResumeGoalInterlock(instanceID, "thread-1"), agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatal("expected automatic restoration status check")
	}
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal pause"})
	if command := findAgentCommand(events, agentproto.CommandThreadGoalSet); command == nil {
		t.Fatal("expected manual pause request")
	}
	events = svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
		CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: goal})
	if command := findAgentCommand(events, agentproto.CommandThreadGoalSet); command != nil {
		t.Fatal("late automatic restoration must not override a pending manual pause")
	}
}

func TestGoalSuccessfulResumeRetiresPreviousDrainingLease(t *testing.T) {
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	svc.root.Instances[instanceID].Threads["thread-1"].ThreadGoal.Status = "paused"
	svc.setGoalInterlockLease(&GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1",
		Phase: GoalInterlockDraining, TriggerSurfaceID: surfaceID, Objective: "ship it"})
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal resume"})
	resume := findAgentCommand(events, agentproto.CommandThreadGoalSet)
	if resume == nil {
		t.Fatal("expected manual resume request")
	}
	svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult,
		CommandID: resume.CommandID, ThreadID: "thread-1",
		ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "ship it", Status: "active"}})
	events = svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", MessageID: "input-after-resume", Text: "恢复后的普通输入"})
	if command := findAgentCommand(events, agentproto.CommandThreadGoalSet); command == nil || command.Goal.Status != "paused" {
		t.Fatal("new input must acquire a new pause barrier instead of reusing the old draining lease")
	}
}
