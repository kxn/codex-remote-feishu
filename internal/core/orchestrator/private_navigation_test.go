package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateNavigationSnapshotRequiresExactCarrier(t *testing.T) {
	now := time.Now()
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	owner := agentproto.NativeOwner{Version: agentproto.PrivateNativeProtocolVersion, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "inst", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Backend: agentproto.BackendCodex, WorkspaceRoot: "/tmp/project", WorkspaceKey: "/tmp/project", Online: true, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", CWD: "/tmp/project"}}})
	a := control.Action{SurfaceSessionID: "surface", GatewayID: "gateway", ChatID: "chat", ActorUserID: "user", Kind: control.ActionAttachInstance, InstanceID: "inst"}
	s.ApplySurfaceAction(a)
	v := s.Surface("surface")
	v.Backend = agentproto.BackendCodex
	v.RouteMode = state.RouteModePinned
	v.SelectedThreadID = "thread"
	s.claimKnownThread(v, s.Instance("inst"), "thread")
	s.SetPrivateNativeProfiles("surface", PrivateNativeProfiles{Codex: "actual-profile"})
	s.AuthenticatePrivateNavigation("surface", owner)
	cp, err := s.ExportPrivateCheckpoint("surface")
	if err != nil || cp.Navigation == nil {
		t.Fatalf("missing snapshot: %v", err)
	}
	v.CodexProfileID = "switching-profile"
	transition, err := s.ExportPrivateCheckpoint("surface")
	if err != nil || transition.Navigation != nil {
		t.Fatal("pending profile change captured old carrier as new profile")
	}
	v.CodexProfileID = ""
	for _, wrong := range []bool{true, false} {
		t.Run(map[bool]string{true: "other_namespace", false: "exact"}[wrong], func(t *testing.T) {
			r := newServiceForTest(&now)
			r.config.PrivateApplication = true
			if err := r.RestorePrivateCheckpoint(cp); err != nil {
				t.Fatal(err)
			}
			r.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Backend: agentproto.BackendCodex, WorkspaceRoot: "/tmp/project", WorkspaceKey: "/tmp/project", Online: true, Threads: map[string]*state.ThreadRecord{}})
			r.SetPrivateNativeProfiles("surface", PrivateNativeProfiles{Codex: "actual-profile"})
			candidate := owner
			if wrong {
				candidate.HistoryNamespace = "other"
			}
			r.AuthenticatePrivateNavigation("surface", candidate)
			got := r.Instance("inst").Threads["thread"] != nil
			if got == wrong {
				t.Fatalf("restored=%v wrong=%v", got, wrong)
			}
			if r.Instance("inst").ActiveThreadID != "" || r.Instance("inst").ActiveTurnID != "" {
				t.Fatal("navigation restored an execution")
			}
		})
	}
}

func TestPrivateReconnectQueryKeepsIdentityAndDeadline(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newServiceForTest(&now)
	s.config.PrivateApplication = true
	s.UpsertInstance(&state.InstanceRecord{InstanceID: "inst", Backend: agentproto.BackendCodex, WorkspaceRoot: "/tmp/project", WorkspaceKey: "/tmp/project", Online: true, Threads: map[string]*state.ThreadRecord{}})
	s.ensureSurface(control.Action{SurfaceSessionID: "surface", GatewayID: "gateway", ChatID: "chat", ActorUserID: "user"})
	v := s.Surface("surface")
	v.Backend = agentproto.BackendCodex
	v.RouteMode = state.RouteModePinned
	v.AttachedInstanceID = "inst"
	v.SelectedThreadID = "thread"
	first := s.BeginPrivateReconnectQuery("surface", "inst")
	if len(first) != 1 || first[0].Command == nil {
		t.Fatal("missing query")
	}
	if next := s.BeginPrivateReconnectQuery("surface", "inst"); len(next) != 0 {
		t.Fatal("reconnect overwrote query")
	}
	cp, err := s.ExportPrivateCheckpoint("surface")
	if err != nil || cp.ReconnectQuery == nil {
		t.Fatalf("missing frozen query: %v", err)
	}
	r := newServiceForTest(&now)
	r.config.PrivateApplication = true
	if err = r.RestorePrivateCheckpoint(cp); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	if !r.PrivateOwnerDeadlineDue("surface", now) {
		t.Fatal("original deadline reset")
	}
	events := r.TickPrivateOwnerDeadlines("surface", now)
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != "private_reconnect_thread_unavailable" {
		t.Fatal("missing recoverable timeout notice")
	}
	if r.PrivateReconnectQueryMatches("surface", "inst", first[0].Command.CommandID) {
		t.Fatal("expired query still active")
	}
}
