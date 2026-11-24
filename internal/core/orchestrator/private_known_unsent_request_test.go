package orchestrator

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestKnownUnsentRequestRestoresCardWithoutStoppingActiveTurn(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", WorkspaceRoot: "/work", WorkspaceKey: "/work", Online: true, ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/work", Loaded: true}}})
	s.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface", ChatID: "chat", ActorUserID: "user", InstanceID: "inst"})
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI}})
	s.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "thread", TurnID: "turn", RequestID: "req", Metadata: map[string]any{"requestType": "approval", "title": "Approve", "body": "Allow?"}})
	result := s.ApplySurfaceAction(control.Action{Kind: control.ActionRespondRequest, SurfaceSessionID: "surface", MessageID: "card", Request: testRequestAction("req", "approval", "accept", nil, 0)})
	var id string
	for _, event := range result {
		if event.Command != nil {
			id = event.Command.CommandID
		}
	}
	if id == "" {
		t.Fatal("no response command")
	}
	surface := s.root.Surfaces["surface"]
	surface.ActiveQueueItemID = "queue"
	surface.QueueItems["queue"] = &state.QueueItemRecord{ID: "queue", SurfaceSessionID: "surface", Status: state.QueueItemRunning}
	events := s.HandleKnownUnsent("surface", id, errors.New("old wrapper"))
	request := surface.PendingRequests["req"]
	if len(events) == 0 || request == nil || request.LifecycleState == requestLifecycleSubmitting || request.PendingDispatchCommandID != "" {
		t.Fatalf("request was not restored: %+v events=%d", request, len(events))
	}
	if surface.ActiveQueueItemID != "queue" || surface.QueueItems["queue"].Status != state.QueueItemRunning {
		t.Fatal("active prompt was changed")
	}
}

func TestKnownUnsentGoalDoesNotTouchOtherDispatchingPrompt(t *testing.T) {
	svc, surfaceID, _ := goalInterlockTestSetup(t)
	surface := svc.root.Surfaces[surfaceID]
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionGoalCommand, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: "user-1", MessageID: "goal", Text: "/goal"})
	goal := findAgentCommand(events, agentproto.CommandThreadGoalGet)
	if goal == nil {
		t.Fatal("missing goal query")
	}
	surface.ActiveQueueItemID = "queue"
	surface.QueueItems["queue"] = &state.QueueItemRecord{ID: "queue", SurfaceSessionID: surfaceID, Status: state.QueueItemDispatching}
	failed := svc.HandleKnownUnsent(surfaceID, goal.CommandID, errors.New("old wrapper"))
	if findPageEvent(failed) == nil {
		t.Fatal("goal failure card missing")
	}
	if _, ok := svc.goalUserCommands[goal.CommandID]; ok {
		t.Fatal("goal command remains pending")
	}
	if surface.ActiveQueueItemID != "queue" || surface.QueueItems["queue"].Status != state.QueueItemDispatching {
		t.Fatal("goal failure changed unrelated prompt")
	}
	if events = svc.HandleKnownUnsent(surfaceID, "unrelated-control", errors.New("unsent")); len(events) != 0 || surface.ActiveQueueItemID != "queue" {
		t.Fatal("unknown command changed active prompt")
	}
}
