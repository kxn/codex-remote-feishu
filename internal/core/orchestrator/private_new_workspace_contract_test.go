package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateNewWorkspaceDoesNotReuseIncompatibleOrOfflineInstance(t *testing.T) {
	for _, test := range []struct {
		name   string
		online bool
	}{{"online_incompatible", true}, {"offline", false}} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now()
			s := newServiceForTest(&now)
			s.config.PrivateApplication = true
			s.MaterializeSurfaceResumeContract("s", "g", "chat", "user", state.HeadlessCodexSurfaceBackendContract(state.OAuthCodexProfileID), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
			s.UpsertInstance(&state.InstanceRecord{InstanceID: "old", WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID, Source: "vscode", Online: test.online, Threads: map[string]*state.ThreadRecord{}})
			surface := s.root.Surfaces["s"]
			events := s.enterTargetPickerNewThread(surface, "/workspace")
			if surface.PendingHeadless == nil || surface.PendingHeadless.InstanceID == "old" || !surface.PendingHeadless.PrepareNewThread || surface.PendingHeadless.CodexProfileID != state.OAuthCodexProfileID {
				t.Fatalf("fresh launch not prepared: %+v events=%+v", surface.PendingHeadless, events)
			}
			for _, e := range events {
				if e.DaemonCommand != nil && e.DaemonCommand.Kind == control.DaemonCommandKillHeadless {
					t.Fatal("unrelated old instance was stopped")
				}
			}
			pending := surface.PendingHeadless
			s.UpsertInstance(&state.InstanceRecord{InstanceID: pending.InstanceID, WorkspaceRoot: "/workspace", WorkspaceKey: "/workspace", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID, Source: "headless", Managed: true, Online: true, Threads: map[string]*state.ThreadRecord{}})
			connected := s.ApplyInstanceConnected(pending.InstanceID)
			if surface.RouteMode != state.RouteModeNewThreadReady || surface.AttachedInstanceID != pending.InstanceID {
				t.Fatalf("fresh session did not become ready: route=%s events=%+v", surface.RouteMode, connected)
			}
		})
	}
}
