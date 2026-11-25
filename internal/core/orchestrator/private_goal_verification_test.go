package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateGoalPauseFailureRequiresExplicitReadAndPauseBeforeQueue(t *testing.T) {
	s, surfaceID, instanceID := goalInterlockTestSetup(t)
	s.config.PrivateApplication = true
	msg := control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "m", Text: "work"}
	pause := findAgentCommand(s.ApplySurfaceAction(msg), agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("missing initial pause")
	}
	out := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: pause.CommandID, ThreadID: "thread-1", ErrorMessage: "error"})
	lease := s.goalInterlockLease(instanceID, "thread-1")
	if lease == nil || lease.Phase != GoalInterlockVerificationRequired || findAgentCommand(out, agentproto.CommandThreadGoalSet) != nil {
		t.Fatal("failure did not hold without write retry")
	}
	action := control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, ActorUserID: "user-1", ChatID: "chat-1", Text: "/goal retry"}
	get := findAgentCommand(s.ApplySurfaceAction(action), agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatal("retry did not query goal")
	}
	out = s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "active"}})
	if lease.Phase != GoalInterlockVerificationRequired || findAgentCommand(out, agentproto.CommandThreadGoalSet) != nil || findAgentCommand(out, agentproto.CommandPromptSend) != nil {
		t.Fatal("active goal bypassed verification")
	}
	action.Text = "/goal pause"
	manual := findAgentCommand(s.ApplySurfaceAction(action), agentproto.CommandThreadGoalSet)
	if manual == nil {
		t.Fatal("manual pause absent")
	}
	s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: manual.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "paused"}})
	if lease.Phase != GoalInterlockVerificationRequired {
		t.Fatal("manual pause removed barrier")
	}
	action.Text = "/goal retry"
	get = findAgentCommand(s.ApplySurfaceAction(action), agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatal("second goal read missing")
	}
	out = s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "paused"}})
	probe := findAgentCommand(out, agentproto.CommandThreadRead)
	if probe == nil || findAgentCommand(out, agentproto.CommandPromptSend) != nil {
		t.Fatal("missing idle proof")
	}
	out = s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, CommandID: probe.CommandID, ThreadID: "thread-1", RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle}})
	if findAgentCommand(out, agentproto.CommandPromptSend) == nil {
		t.Fatalf("confirmed idle did not dispatch: %+v", out)
	}
	if lease.ExternalMutationSeen != true {
		t.Fatal("manual pause could be automatically undone")
	}
}

func TestPrivateGoalVerificationReadTimeoutAndOwnershipStayDistinct(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "foreign"}[foreign], func(t *testing.T) {
			s, surfaceID, instanceID := goalInterlockTestSetup(t)
			s.config.PrivateApplication = true
			v := s.root.Surfaces[surfaceID]
			v.QueuedQueueItemIDs = []string{"queue-1"}
			v.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", SurfaceSessionID: surfaceID, Status: state.QueueItemQueued}
			lease := &GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1", TriggerSurfaceID: surfaceID, Phase: GoalInterlockVerificationGetPending, GetCommandID: "get"}
			s.setGoalInterlockLease(lease)
			s.goalGetByCommand = map[string]string{"get": goalInterlockKey(instanceID, "thread-1")}
			var ok bool
			if foreign {
				_, ok = s.HandlePrivateOwnershipQueryFailure(surfaceID, instanceID, "get", nil)
			} else {
				_, ok = s.HandlePrivateReadObservationTimeout(surfaceID, instanceID, "get", nil)
			}
			want := GoalInterlockVerificationRequired
			if foreign {
				want = GoalInterlockOwnershipLost
			}
			if !ok || lease.Phase != want || lease.GetCommandID != "" || len(v.QueuedQueueItemIDs) != 1 {
				t.Fatalf("bad state: %+v", lease)
			}
			if out := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: "get", ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "paused"}}); len(out) != 0 {
				t.Fatal("late stale get acted")
			}
		})
	}
}

func TestPrivateGoalTerminalInactiveDoesNotResumeCompletedGoal(t *testing.T) {
	for _, status := range []string{"complete", "blocked"} {
		t.Run(status, func(t *testing.T) {
			s, surfaceID, instanceID := goalInterlockTestSetup(t)
			s.config.PrivateApplication = true
			v := s.root.Surfaces[surfaceID]
			v.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", SurfaceSessionID: surfaceID, Status: state.QueueItemQueued}
			v.QueuedQueueItemIDs = []string{"queue-1"}
			lease := &GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1", TriggerSurfaceID: surfaceID, Phase: GoalInterlockResumePending, ResumeCommandID: "resume"}
			s.setGoalInterlockLease(lease)
			s.goalInterlockByCommand = map[string]string{"resume": goalInterlockKey(instanceID, "thread-1")}
			out := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: "resume", ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: status}})
			if lease.Phase != GoalInterlockQuiescing || !lease.ExternalMutationSeen || findAgentCommand(out, agentproto.CommandThreadRead) == nil || findAgentCommand(out, agentproto.CommandThreadGoalSet) != nil {
				t.Fatalf("inactive goal not safely probed: %+v", lease)
			}
		})
	}
}
