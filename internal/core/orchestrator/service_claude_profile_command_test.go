package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeProfileCommandSwitchesDetachedSurfaceAndClearsRuntime(t *testing.T) {
	now := time.Date(2026, 4, 29, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "default", "", state.PlanModeSettingOn)
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})

	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/claudeprofile devseek",
	})

	if surface.ClaudeProfileID != "devseek" {
		t.Fatalf("expected profile switch to devseek, got %#v", surface)
	}
	if surface.PlanMode != state.PlanModeSettingOff {
		t.Fatalf("expected detached switch to clear plan mode, got %q", surface.PlanMode)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected detached switch to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "claude_profile_switched" {
		t.Fatalf("expected single switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "DevSeek") || !strings.Contains(events[0].Notice.Text, "没有接管中的工作区") {
		t.Fatalf("unexpected switched notice: %#v", events[0].Notice)
	}
}

func TestClaudeProfileCommandRejectsBusySurface(t *testing.T) {
	now := time.Date(2026, 4, 29, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "default", "", "")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})

	surface := svc.root.Surfaces["surface-1"]
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  "inst-pending",
		ThreadCWD:   "/data/dl/repo",
		Status:      state.HeadlessLaunchStarting,
		RequestedAt: now,
		ExpiresAt:   now.Add(time.Minute),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/claudeprofile devseek",
	})

	if surface.ClaudeProfileID != state.DefaultClaudeProfileID {
		t.Fatalf("expected busy switch to keep current profile, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_starting" {
		t.Fatalf("expected busy rejection notice, got %#v", events)
	}
}

func TestClaudeProfileCommandRestartsWorkspaceAndRestoresTargetProfileSnapshot(t *testing.T) {
	now := time.Date(2026, 4, 29, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", "", state.PlanModeSettingOff)
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{
		{ID: "profile-a", Name: "Profile A"},
		{ID: "profile-b", Name: "Profile B"},
	})

	workspaceKey := "/data/dl/repo"
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceKey
	surface.PlanMode = state.PlanModeSettingOff
	surface.PromptOverride = state.ModelConfigRecord{
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeFullAccess,
	}
	svc.MaterializeClaudeWorkspaceProfileSnapshots(map[string]state.ClaudeWorkspaceProfileSnapshotRecord{
		state.ClaudeWorkspaceProfileSnapshotStorageKey(workspaceKey, agentproto.BackendClaude, "profile-b"): {
			ReasoningEffort: "high",
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/claudeprofile profile-b",
	})

	if surface.ClaudeProfileID != "profile-b" {
		t.Fatalf("expected profile-b after switch, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected workspace restart to schedule pending headless, got %#v", surface)
	}
	if surface.PendingHeadless.ClaudeProfileID != "profile-b" || !surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected pending headless to carry profile-b and preserve new-thread-ready, got %#v", surface.PendingHeadless)
	}
	if surface.PlanMode != state.PlanModeSettingOff {
		t.Fatalf("expected target profile plan mode not to restore from snapshot, got %q", surface.PlanMode)
	}
	if surface.PromptOverride.Model != "" || surface.PromptOverride.ReasoningEffort != "high" || surface.PromptOverride.AccessMode != "" {
		t.Fatalf("expected target profile snapshot to restore reasoning only, got %#v", surface.PromptOverride)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace restart notice + daemon command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "claude_profile_switched" {
		t.Fatalf("expected switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "workspace_create_starting" {
		t.Fatalf("expected workspace_create_starting second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
	if events[2].DaemonCommand.ClaudeProfileID != "profile-b" {
		t.Fatalf("expected daemon command to carry switched profile, got %#v", events[2].DaemonCommand)
	}
}

func TestClaudeProfileCommandRestartsPinnedClaudeThreadOnProfileSwitch(t *testing.T) {
	now := time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", "", "")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{
		{ID: "profile-a", Name: "Profile A"},
		{ID: "profile-b", Name: "Profile B"},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-visible",
		DisplayName:     "repo",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		Source:          "headless",
		Managed:         true,
		Online:          true,
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
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/claudeprofile profile-b",
	})

	if surface.ClaudeProfileID != "profile-b" {
		t.Fatalf("expected switched profile, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-1" || surface.PendingHeadless.ClaudeProfileID != "profile-b" {
		t.Fatalf("expected pending headless to preserve thread and new profile, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeThreadRestore {
		t.Fatalf("expected exact-thread restart after profile switch, got %#v", surface.PendingHeadless)
	}
	if len(events) != 4 {
		t.Fatalf("expected kill old headless + switch notice + restart notice + restart command, got %#v", events)
	}
	if events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless {
		t.Fatalf("expected first event to kill old managed headless, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "claude_profile_switched" {
		t.Fatalf("expected switched notice second, got %#v", events)
	}
	if events[2].Notice == nil || events[2].Notice.Code != "headless_starting" {
		t.Fatalf("expected restart notice third, got %#v", events)
	}
	if events[3].DaemonCommand == nil || events[3].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless fourth, got %#v", events)
	}
	if events[3].DaemonCommand.ThreadID != "thread-1" || events[3].DaemonCommand.ClaudeProfileID != "profile-b" {
		t.Fatalf("expected start headless to resume original thread under new profile, got %#v", events[3].DaemonCommand)
	}
}

func TestClaudeProfileCommandPreservesPinnedThreadWhenRestartingOnDetachedWorkspace(t *testing.T) {
	now := time.Date(2026, 5, 1, 2, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", "", "")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{
		{ID: "profile-a", Name: "Profile A"},
		{ID: "profile-b", Name: "Profile B"},
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{
			agentproto.BackendClaude: {
				"thread-1": {
					ThreadID:   "thread-1",
					Name:       "修复登录流程",
					CWD:        "/data/dl/repo",
					Loaded:     true,
					LastUsedAt: now.Add(-time.Minute),
					ListOrder:  1,
				},
			},
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{
		ThreadID:         "thread-1",
		InstanceID:       "inst-old",
		SurfaceSessionID: "surface-1",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/claudeprofile profile-b",
	})

	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-1" || surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeThreadRestore {
		t.Fatalf("expected profile switch to keep exact-thread continuation, got %#v", surface.PendingHeadless)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + restart notice + restart command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "claude_profile_switched" {
		t.Fatalf("expected switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "headless_starting" {
		t.Fatalf("expected restart notice second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless || events[2].DaemonCommand.ThreadID != "thread-1" {
		t.Fatalf("expected start headless to target original thread, got %#v", events)
	}
}

func TestClaudeProfileCommandRejectedInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 4, 30, 9, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-vscode")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionClaudeProfileCommand,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-vscode",
		ActorUserID:      "user-vscode",
		Text:             "/claudeprofile devseek",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "command_rejected" {
		t.Fatalf("expected vscode mode to reject claude profile switch, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/mode claude") {
		t.Fatalf("expected guidance to switch to claude mode, got %#v", events[0].Notice)
	}
}
