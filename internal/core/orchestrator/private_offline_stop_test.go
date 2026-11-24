package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
	"time"
)

func TestPrivateOfflineStopPersistsExactTargetAndWaitsForCarrier(t *testing.T) {
	now := time.Now().UTC()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", WorkspaceRoot: "/tmp/project", WorkspaceKey: "/tmp/project", Source: "headless", Managed: true, Online: true, ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/project"}}})
	a := control.Action{SurfaceSessionID: "surface", GatewayID: "gateway", ChatID: "chat", ActorUserID: "user", Kind: control.ActionAttachInstance, InstanceID: "instance"}
	s.ApplySurfaceAction(a)
	a.Kind = control.ActionTextMessage
	a.MessageID = "input"
	a.Text = "hello"
	s.ApplySurfaceAction(a)
	s.BindPrivatePromptCommand("surface", "command-1")
	s.ApplyAgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
	a.Kind = control.ActionTextMessage
	a.MessageID = "queued"
	a.Text = "queued text"
	s.ApplySurfaceAction(a)
	if len(s.Surface("surface").QueuedQueueItemIDs) != 1 {
		t.Fatal("missing queued draft")
	}

	if _, e := s.ApplyPrivateTransportDisconnected("surface", "instance"); e != nil {
		t.Fatal(e)
	}
	a.Kind = control.ActionStop
	a.MessageID = "stop-message"
	events := s.ApplySurfaceAction(a)
	for _, v := range events {
		if v.Command != nil {
			t.Fatal("offline stop sent a native command")
		}
	}
	cp, e := s.ExportPrivateCheckpoint("surface")
	if e != nil {
		t.Fatal(e)
	}
	if len(cp.QueuedQueueItemIDs) != 0 {
		t.Fatal("stop failed to discard queued draft")
	}
	if cp.Version != 2 || cp.OfflineStop == nil || cp.OfflineStop.InstanceID != "instance" || cp.OfflineStop.ThreadID != "thread" || cp.OfflineStop.TurnID != "turn" {
		t.Fatalf("stop was not frozen: %+v", cp.OfflineStop)
	}
	restored := newServiceForTest(&now)
	restored.config.PrivateApplication = true
	if e = restored.RestorePrivateCheckpoint(cp); e != nil {
		t.Fatal(e)
	}
	if len(restored.Surface("surface").QueuedQueueItemIDs) != 0 {
		t.Fatal("restart resurrected discarded draft")
	}

	if got := restored.ResumePrivateOfflineStop("surface", "other"); len(got) != 0 {
		t.Fatal("foreign carrier received stop")
	}
	if cp.OfflineStop.CommandID == "" {
		t.Fatal("missing frozen command identity")
	}
	terminalFirst := newServiceForTest(&now)
	terminalFirst.config.PrivateApplication = true
	if e = terminalFirst.RestorePrivateCheckpoint(cp); e != nil {
		t.Fatal(e)
	}
	terminalFirst.ResolvePrivateOfflineStopTerminal("surface", "instance", "unrelated-command", "thread", "turn")
	if _, ok := terminalFirst.privateOfflineStops["surface"]; !ok {
		t.Fatal("unrelated terminal cleared stop")
	}
	terminalFirst.ResolvePrivateOfflineStopTerminal("surface", "instance", cp.OfflineStop.CommandID, "thread", "turn")
	if _, ok := terminalFirst.privateOfflineStops["surface"]; ok {
		t.Fatal("exact terminal failed to clear stop")
	}
	restored.turns.bindActiveRemote("instance", &remoteTurnBinding{InstanceID: "instance", SurfaceSessionID: "surface", CommandID: "other-command", QueueItemID: cp.OfflineStop.QueueItemID, ThreadID: "thread", TurnID: "turn"})
	if got := restored.ResumePrivateOfflineStop("surface", "instance"); len(got) != 0 {
		t.Fatal("unrelated command received stop")
	}
	restored.turns.clearActiveRemote("instance")
	if e = restored.RestorePrivateNativeProjection("surface", "instance", cp.OfflineStop.CommandID, "thread", "turn"); e != nil {
		t.Fatal(e)
	}
	got := restored.ResumePrivateOfflineStop("surface", "instance")
	if len(got) != 1 || got[0].Command == nil || got[0].Command.Kind != agentproto.CommandTurnInterrupt || got[0].Command.Target.ThreadID != "thread" || got[0].Command.Target.TurnID != "turn" {
		t.Fatal("exact old turn stop not issued")
	}
	if got = restored.ResumePrivateOfflineStop("surface", "instance"); len(got) != 0 {
		t.Fatal("stop duplicated")
	}
}
