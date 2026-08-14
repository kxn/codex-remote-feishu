package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func materializeTestCodexProfiles(svc *Service, profiles ...state.CodexProfileSummary) {
	records := []state.CodexProfileSummary{{
		ID:        state.NativeCodexProfileID,
		Kind:      state.CodexProfileKindNative,
		Name:      "本机默认",
		Available: true,
	}}
	for _, profile := range profiles {
		profile.Kind = state.CodexProfileKindAPI
		profile.Available = true
		records = append(records, profile)
	}
	svc.MaterializeCodexProfiles(records)
}

func TestCodexProfileCommandSwitchesDetachedSurface(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})

	surface := svc.root.Surfaces["surface-1"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile team-proxy",
	})

	if surface.CodexProfileID != "team-proxy" {
		t.Fatalf("expected profile switch to team-proxy, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_profile_switched" {
		t.Fatalf("expected single switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "Team Proxy") || !strings.Contains(events[0].Notice.Text, "没有接管中的工作区") {
		t.Fatalf("unexpected switched notice: %#v", events[0].Notice)
	}
}

func TestCodexProfileCommandCanonicalSlashSwitchesBotProfile(t *testing.T) {
	now := time.Date(2026, 8, 1, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "team-proxy", Kind: state.CodexProfileKindAPI, Name: "Team Proxy", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user",
		state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)

	events := svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionCodexProfileCommand, SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/codexprofile team-proxy",
	})

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.CodexProfileID != "team-proxy" {
		t.Fatalf("bot codex profile = %q, want team-proxy", record.CodexProfileID)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_profile_switched" ||
		!strings.Contains(events[0].Notice.Text, "Codex Profile") {
		t.Fatalf("expected codex profile switched notice, got %#v", events)
	}
}

func TestCodexProfileCommandClearsModelOverrideWhenSwitchingToFixedAPIProfile(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", ReasoningEffort: "high", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user",
		state.ProductModeNormal, agentproto.BackendCodex, state.OAuthCodexProfileID, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID: "app-1", ProductMode: state.ProductModeNormal, Backend: agentproto.BackendCodex,
		CodexProfileID: state.OAuthCodexProfileID,
		PromptOverride: surface.PromptOverride,
		UpdatedAt:      now.Add(-time.Minute),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionCodexProfileCommand, SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/codexprofile custom-profile",
	})

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.CodexProfileID != "custom-profile" {
		t.Fatalf("bot codex profile = %q, want custom-profile", record.CodexProfileID)
	}
	if record.PromptOverride.Model != "" || record.PromptOverride.ReasoningEffort != "" || record.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected fixed profile switch to clear model/reasoning only, got %#v", record.PromptOverride)
	}
	if surface.PromptOverride.Model != "" || surface.PromptOverride.ReasoningEffort != "" || surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected surface projection to clear model/reasoning only, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_profile_switched" {
		t.Fatalf("expected switched notice, got %#v", events)
	}
}

func TestCodexProfileCommandRejectsUnavailableOAuthProfile(t *testing.T) {
	now := time.Date(2026, 8, 1, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: state.OAuthCodexProfileID, Kind: state.CodexProfileKindOAuth, Name: "ChatGPT 登录", Available: false, StatusCode: "missing"},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	surface := svc.root.Surfaces["surface-1"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile " + state.OAuthCodexProfileID,
	})

	if surface.CodexProfileID != state.DefaultCodexProfileID {
		t.Fatalf("unavailable profile changed selected profile: %#v", surface)
	}
	if len(events) != 1 || events[0].PageView == nil ||
		!containsPageSectionLine(events[0].PageView.NoticeSections, "未检测到 ChatGPT 登录") {
		t.Fatalf("expected inline structured unavailable error, got %#v", events)
	}
}

func TestCodexProfileCommandRejectsUnavailableAPIProfileWithFriendlyReason(t *testing.T) {
	now := time.Date(2026, 8, 1, 11, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "expensivecodex", Kind: state.CodexProfileKindAPI, Name: "expensivecodex", Available: false, StatusCode: "profile_definition_incomplete"},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	surface := svc.root.Surfaces["surface-1"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile expensivecodex",
	})

	if surface.CodexProfileID != state.DefaultCodexProfileID {
		t.Fatalf("unavailable profile changed selected profile: %#v", surface)
	}
	if len(events) != 1 || events[0].PageView == nil ||
		!containsPageSectionLine(events[0].PageView.NoticeSections, "配置不完整") ||
		containsPageSectionLine(events[0].PageView.NoticeSections, "profile_definition_incomplete") {
		t.Fatalf("expected friendly unavailable API error, got %#v", events)
	}
}

func containsPageSectionLine(sections []control.FeishuCardTextSection, fragment string) bool {
	for _, section := range sections {
		for _, line := range section.Lines {
			if strings.Contains(line, fragment) {
				return true
			}
		}
	}
	return false
}

func TestCodexProfileCommandExplicitCurrentSelectionWritesCanonicalBotProfile(t *testing.T) {
	now := time.Date(2026, 7, 31, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user",
		state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff,
	)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID: "app-1", ProductMode: state.ProductModeNormal, Backend: agentproto.BackendCodex,
		CodexProfileID: "team-proxy", UpdatedAt: now.Add(-time.Minute),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind: control.ActionCodexProfileCommand, SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/codexprofile team-proxy",
	})
	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.CodexProfileID != "team-proxy" || !record.UpdatedAt.Equal(now) {
		t.Fatalf("explicit current selection did not write canonical profile evidence: %#v", record)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_profile_current" {
		t.Fatalf("explicit current selection should remain a runtime no-op: %#v", events)
	}
}

func TestCodexProfileCommandRejectsBusySurface(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})

	surface := svc.root.Surfaces["surface-1"]
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  "inst-pending",
		ThreadCWD:   "/data/dl/repo",
		Status:      state.HeadlessLaunchStarting,
		RequestedAt: now,
		ExpiresAt:   now.Add(time.Minute),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile team-proxy",
	})

	if surface.CodexProfileID != state.DefaultCodexProfileID {
		t.Fatalf("expected busy switch to keep current profile, got %#v", surface)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_starting" {
		t.Fatalf("expected busy rejection notice, got %#v", events)
	}
}

func TestCodexProfileCommandRestartsWorkspace(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", state.PlanModeSettingOff)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})

	workspaceKey := "/data/dl/repo"
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceKey

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile team-proxy",
	})

	if surface.CodexProfileID != "team-proxy" {
		t.Fatalf("expected switched profile, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected workspace restart to schedule pending headless, got %#v", surface)
	}
	if surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		surface.PendingHeadless.CodexProfileID != "team-proxy" ||
		!surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected pending headless to carry profile and preserve new-thread-ready, got %#v", surface.PendingHeadless)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace restart notice + daemon command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "codex_profile_switched" {
		t.Fatalf("expected switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "workspace_route_restart_starting" {
		t.Fatalf("expected workspace_route_restart_starting second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
	if events[2].DaemonCommand.CodexProfileID != "team-proxy" {
		t.Fatalf("expected daemon command to carry switched profile, got %#v", events[2].DaemonCommand)
	}
}

func TestCodexProfileCommandRestartsPinnedCodexThread(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-visible",
		DisplayName:    "repo",
		WorkspaceRoot:  "/data/dl/repo",
		WorkspaceKey:   "/data/dl/repo",
		ShortName:      "repo",
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "default",
		Source:         "headless",
		Managed:        true,
		Online:         true,
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
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile team-proxy",
	})

	if surface.CodexProfileID != "team-proxy" {
		t.Fatalf("expected switched profile, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-1" || surface.PendingHeadless.CodexProfileID != "team-proxy" {
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
	if events[1].Notice == nil || events[1].Notice.Code != "codex_profile_switched" {
		t.Fatalf("expected switched notice second, got %#v", events)
	}
	if events[2].Notice == nil || events[2].Notice.Code != "headless_starting" {
		t.Fatalf("expected restart notice third, got %#v", events)
	}
	if events[3].DaemonCommand == nil || events[3].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless fourth, got %#v", events)
	}
	if events[3].DaemonCommand.ThreadID != "thread-1" || events[3].DaemonCommand.CodexProfileID != "team-proxy" {
		t.Fatalf("expected start headless to resume original thread under new profile, got %#v", events[3].DaemonCommand)
	}
}

func TestCodexProfileCommandCrossModelGroupRestartsSameWorkspaceForNewThread(t *testing.T) {
	now := time.Date(2026, 8, 2, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", ReasoningEffort: "high", Available: true},
		{ID: "gpt-profile", Kind: state.CodexProfileKindAPI, Name: "GPT", Model: "gpt-5.5", ReasoningEffort: "xhigh", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "custom-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-custom",
		DisplayName:    "repo",
		WorkspaceRoot:  "/data/dl/repo",
		WorkspaceKey:   "/data/dl/repo",
		ShortName:      "repo",
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "custom-profile",
		Source:         "headless",
		Managed:        true,
		Online:         true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧自定义模型会话", CWD: "/tmp/other-repo", Loaded: true},
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-custom"
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-custom"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprofile gpt-profile",
	})

	if surface.CodexProfileID != "gpt-profile" {
		t.Fatalf("expected switched profile, got %#v", surface)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	pending := surface.PendingHeadless
	if surface.PendingHeadless.ThreadID != "" ||
		string(surface.PendingHeadless.Purpose) != "workspace_route_restart" ||
		!surface.PendingHeadless.PrepareNewThread {
		t.Fatalf("expected cross-model profile switch to restart current workspace route for a new thread, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.CodexProfileID != "gpt-profile" {
		t.Fatalf("expected pending headless to carry target profile, got %#v", surface.PendingHeadless)
	}
	if surface.PendingHeadless.WorkspaceKey != "/data/dl/repo" || surface.PendingHeadless.ThreadCWD != "/data/dl/repo" || surface.ClaimedWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected pending restart to preserve current workspace, surface=%#v pending=%#v", surface, surface.PendingHeadless)
	}
	if len(events) != 5 {
		t.Fatalf("expected kill + switched notice + new-thread notice + route restart notice + start command, got %#v", events)
	}
	if events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless {
		t.Fatalf("expected first event to kill old managed headless, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "codex_profile_switched" {
		t.Fatalf("expected switched notice second, got %#v", events)
	}
	if events[2].Notice == nil || events[2].Notice.Code != "codex_model_group_new_thread" {
		t.Fatalf("expected cross-model-group notice third, got %#v", events)
	}
	if events[3].Notice == nil || events[3].Notice.Code == "workspace_create_starting" || events[3].Notice.Code != "workspace_route_restart_starting" {
		t.Fatalf("expected current-workspace route restart notice fourth, got %#v", events)
	}
	if events[4].DaemonCommand == nil ||
		events[4].DaemonCommand.Kind != control.DaemonCommandStartHeadless ||
		events[4].DaemonCommand.ThreadID != "" ||
		events[4].DaemonCommand.CodexProfileID != "gpt-profile" ||
		events[4].DaemonCommand.WorkspaceKey != "/data/dl/repo" ||
		events[4].DaemonCommand.ThreadCWD != "/data/dl/repo" {
		t.Fatalf("expected start headless to create a new thread under target profile, got %#v", events[4])
	}

	svc.root.Instances["inst-custom"].Online = false
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     pending.InstanceID,
		DisplayName:    "repo",
		WorkspaceRoot:  "/data/dl/repo",
		WorkspaceKey:   "/data/dl/repo",
		ShortName:      "repo",
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "gpt-profile",
		Source:         "headless",
		Managed:        true,
		Online:         true,
		Threads:        map[string]*state.ThreadRecord{},
	})
	connectEvents := svc.ApplyInstanceConnected(pending.InstanceID)
	if surface.PendingHeadless != nil ||
		surface.AttachedInstanceID != pending.InstanceID ||
		surface.SelectedThreadID != "" ||
		surface.RouteMode != state.RouteModeNewThreadReady ||
		surface.PreparedThreadCWD != "/data/dl/repo" ||
		surface.ClaimedWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected connected target profile to restore same-workspace new-thread-ready route, surface=%#v events=%#v", surface, connectEvents)
	}
	for _, event := range connectEvents {
		if event.Notice != nil && (event.Notice.Code == "workspace_attached" || event.Notice.Code == "workspace_create_starting") {
			t.Fatalf("expected route restart connect not to emit workspace onboarding notice, got %#v", connectEvents)
		}
		if event.TargetPickerView != nil {
			t.Fatalf("expected route restart connect not to open workspace picker, got %#v", connectEvents)
		}
	}

	textEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})
	command := firstCodexCommand(textEvents, agentproto.CommandPromptSend)
	if command == nil ||
		command.Target.ExecutionMode != agentproto.PromptExecutionModeStartNew ||
		!command.Target.CreateThreadIfMissing ||
		command.Target.ThreadID != "" ||
		command.Target.CWD != "/data/dl/repo" {
		t.Fatalf("expected next text to start a new thread in the same workspace under target profile, got events=%#v command=%#v", textEvents, command)
	}
	if got := svc.root.Instances[pending.InstanceID].CodexProfileID; got != "gpt-profile" {
		t.Fatalf("expected connected headless instance to keep target profile, got %q", got)
	}
}

func TestCodexProfileCommandRejectedInVSCodeMode(t *testing.T) {
	now := time.Date(2026, 5, 1, 11, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-vscode")
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "surface-vscode",
		ChatID:           "chat-vscode",
		ActorUserID:      "user-vscode",
		Text:             "/codexprofile team-proxy",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_profile_mode_required" {
		t.Fatalf("expected vscode mode to reject codex profile switch, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("expected guidance to switch to codex mode, got %#v", events[0].Notice)
	}
}
