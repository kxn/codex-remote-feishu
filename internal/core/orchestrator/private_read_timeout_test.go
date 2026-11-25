package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateReadTimeoutKeepsGoalQueueAndDoesNotReplayAfterRestart(t *testing.T) {
	s, v, _ := newReviewSessionService(t)
	s.config.PrivateApplication = true
	lease := &GoalInterlockLease{InstanceID: "inst-1", ThreadID: "thread-main", TriggerSurfaceID: v.SurfaceSessionID, TriggerQueueItemID: "queue-1", Phase: GoalInterlockQuiescing, ProbeCommandID: "probe"}
	s.setGoalInterlockLease(lease)
	s.goalProbeByCommand = map[string]string{"probe": goalInterlockKey("inst-1", "thread-main")}
	v.QueueItems["queue-1"] = &state.QueueItemRecord{ID: "queue-1", SurfaceSessionID: v.SurfaceSessionID, Status: state.QueueItemQueued}
	v.QueuedQueueItemIDs = []string{"queue-1"}
	if _, ok := s.HandlePrivateReadObservationTimeout("other", "inst-1", "probe", nil); ok {
		t.Fatal("foreign owner timed out query")
	}
	events, ok := s.HandlePrivateReadObservationTimeout(v.SurfaceSessionID, "inst-1", "probe", nil)
	if !ok || len(events) == 0 || lease.Phase != GoalInterlockObservationTimeout || lease.ExternalMutationSeen || lease.ProbeCommandID != "" || len(v.QueuedQueueItemIDs) != 1 {
		t.Fatalf("bad timeout state: %+v", lease)
	}
	if _, ok = s.HandlePrivateReadObservationTimeout(v.SurfaceSessionID, "inst-1", "probe", nil); ok {
		t.Fatal("duplicate timeout replayed")
	}
	cp, err := s.ExportPrivateCheckpoint(v.SurfaceSessionID)
	if err != nil {
		t.Fatal(err)
	}
	cp.GatewayID, cp.ChatID, cp.ActorUserID = "gateway", "chat", "user"
	cp.NextQueueID = 1
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	var copy PrivateCheckpoint
	if err = json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(copy); err != nil {
		t.Fatal(err)
	}
	if got := restored.ReconcileGoalInterlocks(); len(got) != 0 {
		t.Fatalf("unknown query replayed: %+v", got)
	}
}

func TestPrivateReadTimeoutSettlesExactHistoryAndOAuthWithoutClaimingFailure(t *testing.T) {
	for _, kind := range []control.ActionKind{control.ActionShowHistory, control.ActionMCPOAuthCommand} {
		t.Run(string(kind), func(t *testing.T) {
			now := time.Now()
			s := newServiceForTest(&now)
			s.config.PrivateApplication = true
			s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/repo", WorkspaceKey: "/repo", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/repo", Loaded: true}}})
			s.MaterializeSurface("surface", "gateway", "chat", "user")
			v := s.root.Surfaces["surface"]
			v.AttachedInstanceID = "instance"
			v.SelectedThreadID = "thread"
			v.RouteMode = state.RouteModePinned
			text := "/history"
			if kind == control.ActionMCPOAuthCommand {
				text = "/mcpoauth github"
			}
			out := s.ApplySurfaceAction(control.Action{Kind: kind, SurfaceSessionID: "surface", ActorUserID: "user", Text: text})
			id := ""
			for _, e := range out {
				if e.Command != nil {
					id = e.Command.CommandID
				}
			}
			if id == "" {
				t.Fatal("missing query")
			}
			if _, ok := s.HandlePrivateReadObservationTimeout("surface", "other", id, nil); ok {
				t.Fatal("foreign carrier settled")
			}
			result, ok := s.HandlePrivateReadObservationTimeout("surface", "instance", id, nil)
			if !ok || len(result) == 0 {
				t.Fatal("timeout not projected")
			}
			if kind == control.ActionMCPOAuthCommand && (result[0].Notice == nil || strings.Contains(result[0].Notice.Text, "失败") || !strings.Contains(result[0].Notice.Text, "尚未确认")) {
				t.Fatalf("oauth timeout misreported: %+v", result)
			}
			if _, ok = s.HandlePrivateReadObservationTimeout("surface", "instance", id, nil); ok {
				t.Fatal("timeout repeated")
			}
			if kind == control.ActionShowHistory {
				late := agentproto.Event{Kind: agentproto.EventThreadHistoryRead, CommandID: id, ThreadID: "thread", ThreadHistory: &agentproto.ThreadHistoryRecord{Thread: agentproto.ThreadSnapshotRecord{ThreadID: "thread"}}}
				if e := s.ApplyAgentEvent("instance", late); len(e) != 0 {
					t.Fatal("late timed-out history published")
				}
			}
		})
	}
}

func TestPrivateGoalObservationRetryCreatesNewProbeAndWaitsForIdle(t *testing.T) {
	s, surfaceID, instanceID := goalInterlockTestSetup(t)
	s.config.PrivateApplication = true
	input := control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "user-input", Text: "work"}
	pause := findAgentCommand(s.ApplySurfaceAction(input), agentproto.CommandThreadGoalSet)
	if pause == nil {
		t.Fatal("missing goal pause")
	}
	events := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: pause.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "paused", CreatedAt: 1710000000123, UpdatedAt: 1710000000999}})
	old := findAgentCommand(events, agentproto.CommandThreadRead)
	if old == nil {
		t.Fatal("missing first probe")
	}
	if _, ok := s.HandlePrivateReadObservationTimeout(surfaceID, instanceID, old.CommandID, nil); !ok {
		t.Fatal("timeout not recorded")
	}
	retry := s.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "retry", Text: "/goal retry"})
	next := findAgentCommand(retry, agentproto.CommandThreadRead)
	if next == nil || next.CommandID == old.CommandID {
		t.Fatalf("not a fresh read: %+v", retry)
	}
	if s.root.Surfaces[surfaceID].ActiveQueueItemID != "" {
		t.Fatal("queue dispatched without idle")
	}
	stale := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, CommandID: old.CommandID, ThreadID: "thread-1", RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle}})
	if findAgentCommand(stale, agentproto.CommandPromptSend) != nil {
		t.Fatal("old timed-out proof dispatched queue")
	}
	current := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, CommandID: next.CommandID, ThreadID: "thread-1", RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle}})
	if findAgentCommand(current, agentproto.CommandPromptSend) == nil {
		t.Fatalf("fresh idle did not release queue: %+v", current)
	}
}

func TestPrivateGoalReadTimeoutAndReconnectRemainLocal(t *testing.T) {
	s, surfaceID, instanceID := goalInterlockTestSetup(t)
	s.config.PrivateApplication = true
	events := s.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal"})
	get := findAgentCommand(events, agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatal("missing goal read")
	}
	out, ok := s.HandlePrivateReadObservationTimeout(surfaceID, instanceID, get.CommandID, nil)
	if !ok || len(out) == 0 {
		t.Fatal("goal timeout not shown")
	}
	if _, ok = s.goalUserCommands[get.CommandID]; ok {
		t.Fatal("timed out goal remained pending")
	}
	late := s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Objective: "stale", Status: "active"}})
	if len(late) != 0 {
		t.Fatal("late timed-out goal published")
	}
	v := s.root.Surfaces[surfaceID]
	v.PendingTextInput = &state.PendingTextInputRecord{Text: "original", ActorUserID: "user-1", SourceMessageID: "source"}
	s.privateReconnectQueries = map[string]PrivateReconnectQuery{surfaceID: {InstanceID: instanceID, ThreadID: v.SelectedThreadID, CommandID: "reconnect"}}
	if _, ok = s.HandlePrivateReadObservationTimeout(surfaceID, instanceID, "reconnect", nil); !ok {
		t.Fatal("reconnect timeout missing")
	}
	if v.PendingTextInput == nil || v.PendingTextInput.Text != "original" {
		t.Fatal("timeout lost pending input")
	}
	if s.PrivateReconnectQueryMatches(surfaceID, instanceID, "reconnect") {
		t.Fatal("old reconnect still pending")
	}
}

func TestPrivateGoalAutoGetTimeoutDropsOnlyAutomaticRecovery(t *testing.T) {
	s, surfaceID, instanceID := goalInterlockTestSetup(t)
	s.config.PrivateApplication = true
	lease := &GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1", TriggerSurfaceID: surfaceID, Phase: GoalInterlockResumePending, GetCommandID: "get"}
	s.setGoalInterlockLease(lease)
	s.goalGetByCommand = map[string]string{"get": goalInterlockKey(instanceID, "thread-1")}
	if _, ok := s.HandlePrivateReadObservationTimeout(surfaceID, "foreign", "get", nil); ok {
		t.Fatal("foreign settled automatic get")
	}
	if out, ok := s.HandlePrivateReadObservationTimeout(surfaceID, instanceID, "get", nil); !ok || len(out) == 0 || s.goalInterlockLease(instanceID, "thread-1") != nil {
		t.Fatal("automatic get not settled")
	}
}

func TestGoalRetryDoesNotReleaseOwnershipLostQueue(t *testing.T) {
	s, surfaceID, instanceID := goalInterlockTestSetup(t)
	s.config.PrivateApplication = true
	lease := &GoalInterlockLease{InstanceID: instanceID, ThreadID: "thread-1", TriggerSurfaceID: surfaceID, Phase: GoalInterlockOwnershipLost}
	s.setGoalInterlockLease(lease)
	out := s.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", Text: "/goal retry"})
	get := findAgentCommand(out, agentproto.CommandThreadGoalGet)
	if get == nil {
		t.Fatal("non-timeout existing goal query changed")
	}
	out = s.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: get.CommandID, ThreadID: "thread-1", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread-1", Status: "paused"}})
	if findAgentCommand(out, agentproto.CommandPromptSend) != nil || lease.Phase != GoalInterlockOwnershipLost {
		t.Fatal("ordinary goal read released ownership-lost queue")
	}
}
