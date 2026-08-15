package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func materializeTestOpenCodeProfiles(svc *Service, profiles ...state.OpenCodeProfileSummary) {
	records := []state.OpenCodeProfileSummary{{
		ID:        state.DefaultOpenCodeProfileID,
		Revision:  1,
		Name:      "本机默认",
		BuiltIn:   true,
		Available: true,
	}}
	for _, profile := range profiles {
		profile.Available = true
		records = append(records, profile)
	}
	svc.MaterializeOpenCodeProfiles(records)
}

func TestHeadlessLaunchContractRebuildsOpenCodeAdmissionRefAfterResume(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"})

	surface := svc.root.Surfaces["surface-1"]
	if surface.OpenCodeAdmissionRef != nil {
		t.Fatalf("expected resumed surface without admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}

	contract := svc.headlessLaunchContract(surface)
	if contract.Backend != agentproto.BackendOpenCode || contract.OpenCodeProfileID != "op_team" {
		t.Fatalf("unexpected launch contract: %#v", contract)
	}
	if contract.OpenCodeAdmissionRef == nil || contract.OpenCodeAdmissionRef.ProfileRef.ID != "op_team" || contract.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected admission ref rebuilt from profile catalog, got %#v", contract.OpenCodeAdmissionRef)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected rebuilt admission ref written back to surface, got %#v", surface.OpenCodeAdmissionRef)
	}
}

func TestHeadlessLaunchContractKeepsExistingOpenCodeAdmissionRef(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 9, Name: "Team OpenCode"})

	surface := svc.root.Surfaces["surface-1"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}

	contract := svc.headlessLaunchContract(surface)
	if contract.OpenCodeAdmissionRef == nil || contract.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected frozen admission ref to be preserved, got %#v", contract.OpenCodeAdmissionRef)
	}
}

func TestHeadlessLaunchContractDefaultOpenCodeProfileNeedsNoAdmissionRef(t *testing.T) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract(state.DefaultOpenCodeProfileID), "", "")
	materializeTestOpenCodeProfiles(svc)

	surface := svc.root.Surfaces["surface-1"]
	contract := svc.headlessLaunchContract(surface)
	if contract.OpenCodeAdmissionRef != nil {
		t.Fatalf("default opencode profile must not require admission ref, got %#v", contract.OpenCodeAdmissionRef)
	}
}

func TestOpenCodeProfileCommandSwitchesDetachedSurfaceAndClearsRuntime(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendOpenCode, "", "", state.PlanModeSettingOn)
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"})

	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: state.DefaultOpenCodeProfileID, Revision: 1}}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if surface.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected profile switch to op_team, got %#v", surface)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.ID != "op_team" || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected profile switch to freeze target admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("expected detached switch to clear plan mode override, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected detached switch to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "opencode_profile_switched" {
		t.Fatalf("expected single switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "Team OpenCode") || !strings.Contains(events[0].Notice.Text, "没有接管中的工作区") {
		t.Fatalf("unexpected switched notice: %#v", events[0].Notice)
	}
}

func TestOpenCodeProfileCommandRestartsWorkspaceAndFreezesAdmissionRef(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), "", state.PlanModeSettingOff)
	materializeTestOpenCodeProfiles(svc,
		state.OpenCodeProfileSummary{ID: "op_old", Revision: 2, Name: "Old OpenCode"},
		state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"},
	)

	workspaceKey := "/data/dl/repo"
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceKey
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 2}}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if surface.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected switched profile, got %#v", surface)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.ID != "op_team" || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected surface to freeze target admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	if surface.PendingHeadless == nil {
		t.Fatalf("expected workspace restart to schedule pending headless, got %#v", surface)
	}
	if surface.PendingHeadless.Backend != agentproto.BackendOpenCode ||
		surface.PendingHeadless.OpenCodeProfileID != "op_team" ||
		surface.PendingHeadless.OpenCodeAdmissionRef == nil ||
		surface.PendingHeadless.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected pending headless to carry profile/admission ref, got %#v", surface.PendingHeadless)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace restart notice + daemon command, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
	if events[2].DaemonCommand.OpenCodeProfileID != "op_team" ||
		events[2].DaemonCommand.OpenCodeAdmissionRef == nil ||
		events[2].DaemonCommand.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected daemon command to carry switched profile/admission ref, got %#v", events[2].DaemonCommand)
	}
}

func TestOpenCodeProfileCommandRefreshesCurrentProfileWhenRevisionChanged(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"})

	workspaceKey := "/data/dl/repo"
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceKey
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 6}}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.ID != "op_team" || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected same-profile reselect to refresh admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	if surface.PendingHeadless == nil ||
		surface.PendingHeadless.OpenCodeProfileID != "op_team" ||
		surface.PendingHeadless.OpenCodeAdmissionRef == nil ||
		surface.PendingHeadless.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected same-profile reselect to restart workspace with refreshed revision, got %#v", surface.PendingHeadless)
	}
	if len(events) != 3 || events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected workspace restart events for refreshed profile revision, got %#v", events)
	}
}

func TestOpenCodeProfileCommandRestartsPinnedWorkspaceForNewThread(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 7, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), "", state.PlanModeSettingOff)
	materializeTestOpenCodeProfiles(svc,
		state.OpenCodeProfileSummary{ID: "op_old", Revision: 2, Name: "Old OpenCode"},
		state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"},
	)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-visible",
		DisplayName:       "repo",
		WorkspaceRoot:     "/data/dl/repo",
		WorkspaceKey:      "/data/dl/repo",
		ShortName:         "repo",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_old",
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/repo", Loaded: true},
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-visible"
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 2}}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-visible"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if surface.PendingHeadless == nil {
		t.Fatalf("expected pending headless restart, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "" ||
		surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		!surface.PendingHeadless.PrepareNewThread ||
		surface.PendingHeadless.OpenCodeProfileID != "op_team" ||
		surface.PendingHeadless.OpenCodeAdmissionRef == nil ||
		surface.PendingHeadless.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected current-workspace restart for a new thread under target profile, got %#v", surface.PendingHeadless)
	}
	if len(events) != 4 {
		t.Fatalf("expected kill old headless + switch notice + restart notice + restart command, got %#v", events)
	}
	if events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless {
		t.Fatalf("expected first event to kill old managed headless, got %#v", events)
	}
	if events[3].DaemonCommand == nil ||
		events[3].DaemonCommand.Kind != control.DaemonCommandStartHeadless ||
		events[3].DaemonCommand.ThreadID != "" ||
		events[3].DaemonCommand.WorkspaceKey != "/data/dl/repo" ||
		events[3].DaemonCommand.OpenCodeProfileID != "op_team" ||
		events[3].DaemonCommand.OpenCodeAdmissionRef == nil ||
		events[3].DaemonCommand.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected start headless to create a new thread under target profile, got %#v", events[3].DaemonCommand)
	}
}

func TestOpenCodeProfileCommandRevisionRefreshStartsNewThreadForPinnedWorkspace(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 7, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-visible",
		WorkspaceRoot:     "/data/dl/repo",
		WorkspaceKey:      "/data/dl/repo",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{
			ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 6},
		},
		Source:  "headless",
		Managed: true,
		Online:  true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧模型会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-visible"
	surface.ClaimedWorkspaceKey = "/data/dl/repo"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 6}}
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-visible"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if surface.PendingHeadless == nil ||
		surface.PendingHeadless.ThreadID != "" ||
		surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		!surface.PendingHeadless.PrepareNewThread ||
		surface.PendingHeadless.OpenCodeAdmissionRef == nil ||
		surface.PendingHeadless.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected revision refresh to prepare a new session, got %#v", surface.PendingHeadless)
	}
	if got := events[len(events)-1].DaemonCommand; got == nil || got.ThreadID != "" || got.WorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected revision refresh start command to omit the old thread, got %#v", got)
	}
}

func TestOpenCodeProfileSwitchReconcilesOtherGatewaySurfacesWithAdmissionRef(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestOpenCodeProfiles(svc,
		state.OpenCodeProfileSummary{ID: "op_old", Revision: 2, Name: "Old OpenCode"},
		state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"},
	)
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	workspaceKey := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-b",
		DisplayName:       "repo",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		ShortName:         "repo",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_old",
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey, Loaded: true},
		},
	})
	surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	surfaceB.AttachedInstanceID = "inst-b"
	surfaceB.ClaimedWorkspaceKey = workspaceKey
	surfaceB.SelectedThreadID = "thread-1"
	surfaceB.RouteMode = state.RouteModePinned
	surfaceB.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 2}}
	surfaceB.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	if !svc.claimKnownThread(surfaceB, svc.root.Instances["inst-b"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/opencodeprofile op_team",
	})

	foundKill := false
	foundStart := false
	for _, event := range events {
		if event.DaemonCommand == nil {
			continue
		}
		switch event.DaemonCommand.Kind {
		case control.DaemonCommandKillHeadless:
			if event.DaemonCommand.InstanceID == "inst-b" {
				foundKill = true
			}
		case control.DaemonCommandStartHeadless:
			if event.DaemonCommand.SurfaceSessionID == surfaceB.SurfaceSessionID &&
				event.DaemonCommand.ThreadID == "" &&
				event.DaemonCommand.OpenCodeProfileID == "op_team" &&
				event.DaemonCommand.OpenCodeAdmissionRef != nil &&
				event.DaemonCommand.OpenCodeAdmissionRef.ProfileRef.Revision == 7 {
				foundStart = true
			}
		}
	}
	if !foundKill || !foundStart {
		t.Fatalf("expected other gateway surface restart under target opencode profile/admission, kill=%t start=%t events=%#v", foundKill, foundStart, events)
	}
	if surfaceB.PendingHeadless == nil ||
		surfaceB.PendingHeadless.ThreadID != "" ||
		surfaceB.PendingHeadless.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		!surfaceB.PendingHeadless.PrepareNewThread ||
		surfaceB.PendingHeadless.OpenCodeProfileID != "op_team" ||
		surfaceB.PendingHeadless.OpenCodeAdmissionRef == nil ||
		surfaceB.PendingHeadless.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected pending headless on other surface with target profile/admission, got %#v", surfaceB.PendingHeadless)
	}
}

func TestOpenCodeProfileSwitchDeferredReconcileStartsNewThread(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 9, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestOpenCodeProfiles(svc,
		state.OpenCodeProfileSummary{ID: "op_old", Revision: 2, Name: "Old OpenCode"},
		state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"},
	)
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.HeadlessOpenCodeSurfaceBackendContract("op_old"), state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	workspaceKey := normalizeWorkspaceClaimKey(t.TempDir())
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-b",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_old",
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{
			ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 2},
		},
		Source:  "headless",
		Managed: true,
		Online:  true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "旧模型会话", CWD: workspaceKey, Loaded: true},
		},
	})
	surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	surfaceB.AttachedInstanceID = "inst-b"
	surfaceB.ClaimedWorkspaceKey = workspaceKey
	surfaceB.SelectedThreadID = "thread-1"
	surfaceB.RouteMode = state.RouteModePinned
	surfaceB.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_old", Revision: 2}}
	surfaceB.ActiveQueueItemID = "q1"
	surfaceB.QueueItems = map[string]*state.QueueItemRecord{
		"q1": {ID: "q1", Status: state.QueueItemRunning},
	}
	if !svc.claimKnownThread(surfaceB, svc.root.Instances["inst-b"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/opencodeprofile op_team",
	})
	if !surfaceB.ContractRefreshPending {
		t.Fatal("expected busy sibling surface to defer profile refresh")
	}

	surfaceB.ActiveQueueItemID = ""
	surfaceB.QueueItems = nil
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surfaceB.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "ou_b",
		ActorUserID:      "ou_b",
		MessageID:        "om-b-1",
		Text:             "继续",
	})

	if surfaceB.PendingTextInput == nil || surfaceB.PendingTextInput.Text != "继续" {
		t.Fatalf("expected input to wait for refreshed runtime, got %#v", surfaceB.PendingTextInput)
	}
	if surfaceB.PendingHeadless == nil ||
		surfaceB.PendingHeadless.ThreadID != "" ||
		surfaceB.PendingHeadless.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		!surfaceB.PendingHeadless.PrepareNewThread ||
		surfaceB.PendingHeadless.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected deferred profile refresh to start a new session, got %#v", surfaceB.PendingHeadless)
	}
	foundStart := false
	for _, event := range events {
		if event.DaemonCommand != nil &&
			event.DaemonCommand.Kind == control.DaemonCommandStartHeadless &&
			event.DaemonCommand.ThreadID == "" &&
			event.DaemonCommand.WorkspaceKey == workspaceKey {
			foundStart = true
		}
	}
	if !foundStart {
		t.Fatalf("expected deferred refresh to launch current workspace without the old thread, got %#v", events)
	}
}

func TestOpenCodeProfileCommandRejectedOutsideOpenCodeMode(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", "", "")
	materializeTestOpenCodeProfiles(svc, state.OpenCodeProfileSummary{ID: "op_team", Revision: 7, Name: "Team OpenCode"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionOpenCodeProfileCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/opencodeprofile op_team",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "command_rejected" {
		t.Fatalf("expected codex mode to reject opencode profile switch, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "/mode opencode") {
		t.Fatalf("expected guidance to switch to opencode mode, got %#v", events[0].Notice)
	}
}
