package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexProviderCommandSwitchesDetachedSurface(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{{ID: "team-proxy", Name: "Team Proxy"}})

	surface := svc.root.Surfaces["surface-1"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider team-proxy",
	})

	if surface.CodexProviderID != "team-proxy" {
		t.Fatalf("expected provider switch to team-proxy, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_provider_switched" {
		t.Fatalf("expected single switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "Team Proxy") || !strings.Contains(events[0].Notice.Text, "没有接管中的工作区") {
		t.Fatalf("unexpected switched notice: %#v", events[0].Notice)
	}
}

func TestCodexProviderCommandRejectsBusySurface(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{{ID: "team-proxy", Name: "Team Proxy"}})

	surface := svc.root.Surfaces["surface-1"]
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  "inst-pending",
		ThreadCWD:   "/data/dl/repo",
		Status:      state.HeadlessLaunchStarting,
		RequestedAt: now,
		ExpiresAt:   now.Add(time.Minute),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider team-proxy",
	})

	if surface.CodexProviderID != state.DefaultCodexProviderID {
		t.Fatalf("expected busy switch to keep current provider, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_starting" {
		t.Fatalf("expected busy rejection notice, got %#v", events)
	}
}

func TestCodexProviderCommandRestartsWorkspace(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", state.PlanModeSettingOff)
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{
		{ID: "default", Name: state.DefaultCodexProviderName},
		{ID: "team-proxy", Name: "Team Proxy"},
	})

	workspaceKey := "/data/dl/repo"
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceKey

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider team-proxy",
	})

	if surface.CodexProviderID != "team-proxy" {
		t.Fatalf("expected switched provider, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected workspace restart to schedule pending headless, got %#v", surface)
	}
	if surface.PendingHeadless.CodexProviderID != "team-proxy" || !surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected pending headless to carry provider and preserve new-thread-ready, got %#v", surface.PendingHeadless)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace restart notice + daemon command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "codex_provider_switched" {
		t.Fatalf("expected switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "workspace_create_starting" {
		t.Fatalf("expected workspace_create_starting second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
	if events[2].DaemonCommand.CodexProviderID != "team-proxy" {
		t.Fatalf("expected daemon command to carry switched provider, got %#v", events[2].DaemonCommand)
	}
}

func TestCodexProviderCommandRestartsPinnedCodexThread(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{
		{ID: "default", Name: state.DefaultCodexProviderName},
		{ID: "team-proxy", Name: "Team Proxy"},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:       "inst-visible",
		DisplayName:      "repo",
		WorkspaceRoot:    "/data/dl/repo",
		WorkspaceKey:     "/data/dl/repo",
		ShortName:        "repo",
		Backend:          agentproto.BackendCodex,
		CodexProviderID:  "default",
		Source:           "headless",
		Managed:          true,
		Online:           true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/repo", Loaded: true},
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-visible"
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-visible"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider team-proxy",
	})

	if surface.CodexProviderID != "team-proxy" {
		t.Fatalf("expected switched provider, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-1" || surface.PendingHeadless.CodexProviderID != "team-proxy" {
		t.Fatalf("expected pending headless to preserve thread and new provider, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeThreadRestore {
		t.Fatalf("expected exact-thread restart after provider switch, got %#v", surface.PendingHeadless)
	}
	if len(events) != 4 {
		t.Fatalf("expected kill old headless + switch notice + restart notice + restart command, got %#v", events)
	}
	if events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless {
		t.Fatalf("expected first event to kill old managed headless, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "codex_provider_switched" {
		t.Fatalf("expected switched notice second, got %#v", events)
	}
	if events[2].Notice == nil || events[2].Notice.Code != "headless_starting" {
		t.Fatalf("expected restart notice third, got %#v", events)
	}
	if events[3].DaemonCommand == nil || events[3].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless fourth, got %#v", events)
	}
	if events[3].DaemonCommand.ThreadID != "thread-1" || events[3].DaemonCommand.CodexProviderID != "team-proxy" {
		t.Fatalf("expected start headless to resume original thread under new provider, got %#v", events[3].DaemonCommand)
	}
}

func TestCodexProviderCommandRejectedInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-vscode")
	svc.MaterializeCodexProviders([]state.CodexProviderRecord{{ID: "team-proxy", Name: "Team Proxy"}})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-vscode",
		ActorUserID:      "user-vscode",
		Text:             "/codexprovider team-proxy",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_provider_mode_required" {
		t.Fatalf("expected vscode mode to reject codex provider switch, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("expected guidance to switch to codex mode, got %#v", events[0].Notice)
	}
}
