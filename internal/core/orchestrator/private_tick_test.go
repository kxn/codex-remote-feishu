package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateTickOnlyAdvancesUnheldOwner(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	for _, id := range []string{"a", "b"} {
		s.ApplySurfaceAction(control.Action{Kind: control.ActionShowCommandMenu, SurfaceSessionID: id, ChatID: id, ActorUserID: id})
		s.root.Surfaces[id].DispatchMode = state.DispatchModePausedForLocal
		s.pausedUntil[id] = now.Add(-time.Second)
	}
	if !s.PrivateTickDue("a", now) {
		t.Fatal("expired pause missed")
	}
	s.TickPrivateSurface("a", now)
	if s.PrivateTickDue("a", now) {
		t.Fatal("idle owner should not schedule a tick")
	}
	if s.root.Surfaces["a"].DispatchMode != state.DispatchModeNormal {
		t.Fatal("owner timer was not advanced")
	}
	if s.root.Surfaces["b"].DispatchMode != state.DispatchModePausedForLocal {
		t.Fatal("other owner was advanced")
	}
	s.privateRecoveryHolds["b"] = PrivateCheckpoint{Version: 1, SurfaceID: "b"}
	if s.PrivateTickDue("b", now) {
		t.Fatal("held owner scheduled")
	}
	s.TickPrivateSurface("b", now)
	if s.root.Surfaces["b"].DispatchMode != state.DispatchModePausedForLocal {
		t.Fatal("held owner was advanced")
	}
}

func TestPrivateMigrationTickWaitsForOnlineAttachedCarrier(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.MaterializeSurface("s", "g", "chat", "user")
	surface := s.root.Surfaces["s"]
	surface.AttachedInstanceID = "inst"
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	surface.QueueItems = map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: "s", Status: state.QueueItemQueued, ActorUserID: "user", SourceMessageID: "msg", Inputs: []agentproto.Input{{Type: "text", Text: "hello"}}}}
	s.privateMigrationReleased["s"] = true
	s.TickPrivateSurface("s", now)
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatal("offline input dispatched")
	}
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Online: true, Threads: map[string]*state.ThreadRecord{}})
	events := s.TickPrivateSurface("s", now)
	if surface.ActiveQueueItemID != "queue-1" {
		t.Fatalf("input not resumed: %#v", surface)
	}
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			return
		}
	}
	t.Fatal("missing native prompt effect")
}

func TestPrivateAbandonDeadlineRetainedWithoutInventingTerminal(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.MaterializeSurface("s", "g", "chat", "user")
	surface := s.root.Surfaces["s"]
	surface.AttachedInstanceID = "instance"
	deadline := now.Add(30 * time.Second)
	s.setSurfaceDetachAbandoning(surface, deadline)
	checkpoint, err := s.ExportPrivateCheckpoint("s")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hold := range checkpoint.Holds {
		if hold.Kind == "abandoning" {
			var frozen struct{ Deadline time.Time }
			if err = json.Unmarshal(hold.Frozen, &frozen); err != nil || !frozen.Deadline.Equal(deadline) {
				t.Fatalf("deadline evidence: %s %v", hold.Frozen, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("abandon hold missing")
	}
	restored := NewService(nil, Config{PrivateApplication: true}, nil)
	if err = restored.RestorePrivateCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	restored.TickPrivateSurface("s", deadline.Add(time.Hour))
	if _, held := restored.PrivateRecoveryCheckpoint("s"); !held {
		t.Fatal("elapsed time invented a native terminal")
	}
}
