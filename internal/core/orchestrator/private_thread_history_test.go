package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateHistoryQuerySurvivesCheckpointAndRejectsStaleResult(t *testing.T) {
	now := time.Now().UTC()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	inst := &state.InstanceRecord{InstanceID: "instance", Online: true, WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/workspace", Loaded: true}}}
	s.UpsertInstance(inst)
	s.MaterializeSurface("surface", "gateway", "chat", "user")
	v := s.root.Surfaces["surface"]
	v.AttachedInstanceID, v.SelectedThreadID, v.RouteMode = "instance", "thread", state.RouteModePinned
	events := s.ApplySurfaceAction(control.Action{Kind: control.ActionShowHistory, SurfaceSessionID: "surface", ActorUserID: "user", Text: "/history"})
	if len(events) != 2 || events[0].Kind != eventcontract.KindThreadHistory || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandThreadHistoryRead {
		t.Fatalf("history path: %#v", events)
	}
	command := *events[1].Command
	cp, err := s.ExportPrivateCheckpoint("surface")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cp); err != nil {
		t.Fatal(err)
	}
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if err = restored.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	restored.UpsertInstance(inst)
	event := agentproto.Event{Kind: agentproto.EventThreadHistoryRead, CommandID: command.CommandID, ThreadID: "thread", ThreadHistory: &agentproto.ThreadHistoryRecord{Thread: agentproto.ThreadSnapshotRecord{ThreadID: "thread"}}}
	if out := restored.ApplyAgentEvent("other", event); len(out) != 0 {
		t.Fatal("foreign carrier affected history")
	}
	stale := event
	stale.CommandID = "unrelated"
	if out := restored.ApplyAgentEvent("instance", stale); len(out) != 0 {
		t.Fatal("stale query affected history")
	}
	out := restored.ApplyAgentEvent("instance", event)
	if len(out) != 1 || out[0].ThreadHistoryView == nil || out[0].ThreadHistoryView.Loading {
		t.Fatalf("history not completed: %#v", out)
	}
	if out := restored.ApplyAgentEvent("instance", event); len(out) != 0 {
		t.Fatal("duplicate history result republished")
	}
}
