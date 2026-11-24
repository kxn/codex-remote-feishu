package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateGoalCheckpointRestoresExactPendingCardAndInterlock(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.MaterializeSurface("s", "gateway", "chat", "user")
	v := s.root.Surfaces["s"]
	v.AttachedInstanceID, v.SelectedThreadID = "instance", "thread"
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, Backend: agentproto.BackendCodex, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", Loaded: true}}})
	events := s.issueGoalCommand(v, "thread", "set", goalSetCommand("thread", "finish", nil, "user_control"), "card")
	command := events[0].Command.CommandID
	s.setGoalInterlockLease(&GoalInterlockLease{InstanceID: "instance", ThreadID: "thread", TriggerSurfaceID: "s", Phase: GoalInterlockQuiescing, ProbeCommandID: "probe"})
	s.goalProbeByCommand = map[string]string{"probe": goalInterlockKey("instance", "thread")}
	cp, err := s.ExportPrivateCheckpoint("s")
	if err != nil || cp.Version != 2 || cp.Goal == nil {
		t.Fatalf("checkpoint: %#v %v", cp.Goal, err)
	}
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	if restored.goalProbeByCommand["probe"] != goalInterlockKey("instance", "thread") {
		t.Fatal("goal probe binding lost")
	}
	restored.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", Online: true, Backend: agentproto.BackendCodex, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", Loaded: true}}})
	events = restored.ApplyAgentEvent("instance", agentproto.Event{Kind: agentproto.EventThreadGoalCommandResult, CommandID: command, ThreadID: "thread", Status: "accepted", ThreadGoal: &agentproto.ThreadGoalUpdate{ThreadID: "thread", Objective: "finish", Status: "active"}})
	if len(events) == 0 || events[0].PageView == nil || events[0].PageView.MessageID != "card" {
		t.Fatalf("goal result did not restore original card: %#v", events)
	}
	if _, ok := restored.goalUserCommands[command]; ok {
		t.Fatal("settled goal command remains pending")
	}
}

func TestPrivateGoalCheckpointRejectsOtherOwnerAndDuplicateCommand(t *testing.T) {
	s := newServiceForTest(nil)
	for _, c := range []*PrivateGoalCheckpoint{{Commands: []PrivateGoalCommand{{CommandID: "c", SurfaceID: "other", InstanceID: "i", ThreadID: "t", Action: "get"}}}, {Commands: []PrivateGoalCommand{{CommandID: "c", SurfaceID: "s", InstanceID: "i", ThreadID: "t", Action: "get"}, {CommandID: "c", SurfaceID: "s", InstanceID: "i", ThreadID: "t", Action: "get"}}}} {
		if err := s.validatePrivateGoalCheckpoint("s", c); err == nil {
			t.Fatal("invalid goal checkpoint accepted")
		}
	}
}
