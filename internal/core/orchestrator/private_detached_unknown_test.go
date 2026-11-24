package orchestrator

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
	"time"
)

func TestPrivateDetachWithDisconnectedUnknownExecution(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		disconnectFirst, restart bool
	}{{"detach_then_disconnect", false, false}, {"disconnect_then_detach", true, false}, {"disconnect_then_workspace_detach", true, false}, {"detach_disconnect_restart", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			disconnectFirst := tc.disconnectFirst
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			s := newServiceForTest(&now)
			s.config.PrivateApplication = true
			s.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", DisplayName: "headless", WorkspaceRoot: "/tmp/project", WorkspaceKey: "/tmp/project", Source: "headless", Managed: true, Online: true, ObservedFocusedThreadID: "thread", Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/project"}}})
			a := control.Action{SurfaceSessionID: "surface", GatewayID: "gateway", ChatID: "chat", ActorUserID: "user", Kind: control.ActionAttachInstance, InstanceID: "instance"}
			s.ApplySurfaceAction(a)
			a.Kind = control.ActionTextMessage
			a.Text = "preserve input"
			a.MessageID = "message"
			s.ApplySurfaceAction(a)
			s.ApplyAgentEvent("instance", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
			if disconnectFirst {
				if _, e := s.ApplyPrivateTransportDisconnected("surface", "instance"); e != nil {
					t.Fatal(e)
				}
			}
			a.Kind = control.ActionDetach
			if tc.name == "disconnect_then_workspace_detach" {
				a.Kind = control.ActionWorkspaceDetach
			}
			a.Text = ""
			s.ApplySurfaceAction(a)
			if !disconnectFirst {
				if _, e := s.ApplyPrivateTransportDisconnected("surface", "instance"); e != nil {
					t.Fatal(e)
				}
				if tc.restart {
					cp, e := s.ExportPrivateCheckpoint("surface")
					if e != nil {
						t.Fatal(e)
					}
					restored := newServiceForTest(&now)
					restored.config.PrivateApplication = true
					if e = restored.RestorePrivateCheckpoint(cp); e != nil {
						t.Fatal(e)
					}
					s = restored
				}
				now = now.Add(21 * time.Second)
				if !s.PrivateOwnerDeadlineDue("surface", now) {
					t.Fatal("disconnected detach deadline not due")
				}
				s.TickPrivateOwnerDeadlines("surface", now)
			}
			surface := s.Surface("surface")
			if surface.AttachedInstanceID != "" || surface.Abandoning {
				t.Fatal("navigation remains attached")
			}
			if !s.PrivateDetachedInstance("surface", "instance") {
				t.Fatal("unknown execution was lost")
			}
			cp, e := s.ExportPrivateCheckpoint("surface")
			if e != nil {
				t.Fatal(e)
			}
			if len(cp.DetachedExecutions) != 1 || cp.ActiveQueueItemID != "" || len(cp.QueuedQueueItemIDs) != 0 {
				t.Fatal("old execution became dispatchable")
			}
			if cp.Version != 2 {
				t.Fatal("detached checkpoint was not version fenced")
			}
			for name, mutate := range map[string]func(*PrivateCheckpoint){
				"duplicate instance": func(v *PrivateCheckpoint) {
					v.DetachedExecutions = append(v.DetachedExecutions, v.DetachedExecutions[0])
				},
				"current instance": func(v *PrivateCheckpoint) { v.AttachedInstanceID = v.DetachedExecutions[0].InstanceID },
				"foreign hold": func(v *PrivateCheckpoint) {
					v.DetachedExecutions[0].Holds = append(v.DetachedExecutions[0].Holds, PrivateRecoveryHold{Kind: "pending_remote", InstanceID: "foreign", Frozen: json.RawMessage(`{}`)})
				},
				"duplicate input": func(v *PrivateCheckpoint) {
					id := v.DetachedExecutions[0].ActiveQueueItemID
					if id != "" {
						v.DetachedExecutions[0].QueuedQueueItemIDs = append(v.DetachedExecutions[0].QueuedQueueItemIDs, id)
					}
				},
				"active input overlap": func(v *PrivateCheckpoint) { v.ActiveQueueItemID = v.DetachedExecutions[0].ActiveQueueItemID },
				"queued input overlap": func(v *PrivateCheckpoint) {
					id := v.DetachedExecutions[0].ActiveQueueItemID
					if id != "" {
						v.QueuedQueueItemIDs = append(v.QueuedQueueItemIDs, id)
					}
				},
			} {
				t.Run(name, func(t *testing.T) {
					raw, _ := json.Marshal(cp)
					var bad PrivateCheckpoint
					_ = json.Unmarshal(raw, &bad)
					if (name == "duplicate input" || name == "active input overlap" || name == "queued input overlap") && bad.DetachedExecutions[0].ActiveQueueItemID == "" {
						t.Skip("no active item")
					}
					mutate(&bad)
					target := newServiceForTest(&now)
					target.config.PrivateApplication = true
					if e := target.RestorePrivateCheckpoint(bad); e == nil {
						t.Fatal("invalid detached references accepted")
					}
				})
			}

			restored := newServiceForTest(&now)
			restored.config.PrivateApplication = true
			if e = restored.RestorePrivateCheckpoint(cp); e != nil {
				t.Fatal(e)
			}
			if !restored.PrivateDetachedInstance("surface", "instance") {
				t.Fatal("detached evidence not durable")
			}
			if !restored.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "surface", Kind: control.ActionTextMessage}) {
				t.Fatal("old unknown blocked new work")
			}
		})
	}
}
