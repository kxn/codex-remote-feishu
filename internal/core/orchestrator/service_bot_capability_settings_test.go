package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateModeCommandWritesBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/mode claude",
	})
	if len(events) == 0 {
		t.Fatalf("expected feedback event")
	}

	record, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if !ok {
		t.Fatalf("expected bot capability settings for app-1")
	}
	if record.ProductMode != state.ProductModeNormal || record.Backend != agentproto.BackendClaude {
		t.Fatalf("bot settings contract = %s/%s, want normal/claude", record.ProductMode, record.Backend)
	}
	if record.UpdatedBy != "ou_user" || !record.UpdatedAt.Equal(now) {
		t.Fatalf("updated metadata = %q/%v, want ou_user/%v", record.UpdatedBy, record.UpdatedAt, now)
	}
}

func TestGroupSurfaceReadsBotCapabilitySettingsForBackend(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
		PromptOverride:  state.ModelConfigRecord{Model: "claude-sonnet", ReasoningEffort: "max"},
	}
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:chat:oc_room",
		"app-1",
		"oc_room",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"team-proxy",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)

	if got := svc.SurfaceBackend("feishu:app-1:chat:oc_room"); got != agentproto.BackendClaude {
		t.Fatalf("SurfaceBackend = %s, want claude", got)
	}
	if got := svc.SurfaceClaudeProfileID("feishu:app-1:chat:oc_room"); got != "devseek" {
		t.Fatalf("SurfaceClaudeProfileID = %q, want devseek", got)
	}
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	if surface.Backend != agentproto.BackendClaude || surface.ClaudeProfileID != "devseek" {
		t.Fatalf("group surface contract projection = %s/%q, want claude/devseek", surface.Backend, surface.ClaudeProfileID)
	}
}

func TestGroupSurfaceReadsBotCapabilitySettingsForOpenCodeProfile(t *testing.T) {
	now := time.Date(2026, 8, 1, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:         "app-1",
		ProductMode:       state.ProductModeNormal,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		OpenCodeAdmissionRef: &state.OpenCodeAdmissionRef{
			ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7},
		},
		PromptOverride: state.ModelConfigRecord{
			Model:           "gpt-5.5",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeConfirm,
		},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:chat:oc_room",
		"app-1",
		"oc_room",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"team-proxy",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)

	if got := svc.SurfaceBackend("feishu:app-1:chat:oc_room"); got != agentproto.BackendOpenCode {
		t.Fatalf("SurfaceBackend = %s, want opencode", got)
	}
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	if surface.Backend != agentproto.BackendOpenCode || surface.OpenCodeProfileID != "op_team" {
		t.Fatalf("group surface opencode projection = %s/%q, want opencode/op_team", surface.Backend, surface.OpenCodeProfileID)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.ID != "op_team" || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("group surface opencode projection lost admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	contract := state.SurfaceDesiredBackendContract(surface)
	if contract.CodexProfileID != "" || contract.ClaudeProfileID != "" {
		t.Fatalf("opencode desired contract retained inactive profile fields: %#v", contract)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{ReasoningEffort: "high", AccessMode: ""}) ||
		surface.PlanMode != state.PlanModeSettingOff ||
		surface.PlanModeOverrideSet {
		t.Fatalf("opencode bot projection should keep reasoning only; access/plan are session-scoped: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
	// 会话级 access/plan 不被后续 bot 投影覆盖。
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)
	svc.projectLatestBotCapabilitySettingsToSurface(surface)
	if surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm ||
		surface.PlanMode != state.PlanModeSettingOn ||
		!surface.PlanModeOverrideSet {
		t.Fatalf("bot projection must not overwrite session access/plan: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestPrivateModeCommandSwitchesBotCapabilitySettingsToOpenCodeAndKeepsRuntimeReasoningAccessAndPlan(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:      "app-1",
		ProductMode:    state.ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "default",
		PromptOverride: state.ModelConfigRecord{Model: "gpt-5.5", ReasoningEffort: "high"},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/mode opencode",
	})
	if len(events) == 0 {
		t.Fatalf("expected mode switch feedback")
	}

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.Backend != agentproto.BackendOpenCode {
		t.Fatalf("bot backend = %q, want opencode", record.Backend)
	}
	if record.PromptOverride != (state.ModelConfigRecord{ReasoningEffort: "high", AccessMode: ""}) ||
		record.PlanModeOverrideSet {
		t.Fatalf("bot opencode settings should keep reasoning only; access/plan are session-scoped: %#v %s/%v", record.PromptOverride, record.PlanMode, record.PlanModeOverrideSet)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{ReasoningEffort: "high", AccessMode: agentproto.AccessModeConfirm}) ||
		surface.PlanMode != state.PlanModeSettingOn ||
		!surface.PlanModeOverrideSet {
		t.Fatalf("surface opencode projection should retain runtime reasoning/access and plan: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestPrivatePlanCommandWritesSurfaceSettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/plan on",
	})
	if len(events) == 0 {
		t.Fatalf("expected feedback event")
	}

	if record, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok && record.PlanModeOverrideSet {
		t.Fatalf("plan must not be written to bot capability record, got %#v", record)
	}
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("private surface plan = %s/%v, want on/true for current UX", surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestPrivateCapabilityCommandsPreserveConcurrentGatewayFields(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/mode claude",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_b",
		GatewayID:        "app-1",
		ChatID:           "ou_b",
		ActorUserID:      "ou_b",
		Text:             "/plan on",
	})

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.Backend != agentproto.BackendClaude {
		t.Fatalf("bot backend = %q, want claude after unrelated plan update", record.Backend)
	}
	if record.PlanModeOverrideSet {
		t.Fatalf("bot record must not carry session plan, got %#v", record)
	}
	ouB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	if ouB.PlanMode != state.PlanModeSettingOn || !ouB.PlanModeOverrideSet {
		t.Fatalf("ou_b surface plan = %s/%v, want on/true", ouB.PlanMode, ouB.PlanModeOverrideSet)
	}
}

func TestPrivateCapabilityCommandInterleavingsPreserveUnrelatedFields(t *testing.T) {
	cases := []struct {
		name       string
		firstKind  control.ActionKind
		firstText  string
		stale      func(*state.SurfaceConsoleRecord)
		secondKind control.ActionKind
		secondText string
		check      func(state.BotCapabilitySettingsRecord) bool
	}{
		{
			name:      "model then provider",
			firstKind: control.ActionModelCommand,
			firstText: "/model gpt-5.5 high",
			stale: func(surface *state.SurfaceConsoleRecord) {
				surface.PromptOverride = state.ModelConfigRecord{}
			},
			secondKind: control.ActionCodexProfileCommand,
			secondText: "/codexprofile team-proxy",
			check: func(record state.BotCapabilitySettingsRecord) bool {
				return record.CodexProfileID == "team-proxy" && record.PromptOverride.Model == "gpt-5.5" && record.PromptOverride.ReasoningEffort == "high"
			},
		},
		{
			name:      "model then reasoning",
			firstKind: control.ActionModelCommand,
			firstText: "/model gpt-5.5 high",
			stale: func(surface *state.SurfaceConsoleRecord) {
				surface.PromptOverride = state.ModelConfigRecord{}
			},
			secondKind: control.ActionReasoningCommand,
			secondText: "/reasoning low",
			check: func(record state.BotCapabilitySettingsRecord) bool {
				return record.PromptOverride.Model == "gpt-5.5" && record.PromptOverride.ReasoningEffort == "low"
			},
		},
		{
			name:      "access then plan",
			firstKind: control.ActionAccessCommand,
			firstText: "/access confirm",
			stale: func(surface *state.SurfaceConsoleRecord) {
				surface.PromptOverride = state.ModelConfigRecord{}
			},
			secondKind: control.ActionPlanCommand,
			secondText: "/plan on",
			check: func(state.BotCapabilitySettingsRecord) bool {
				return true
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
			svc.UpsertInstance(&state.InstanceRecord{
				InstanceID:    "inst-1",
				Backend:       agentproto.BackendCodex,
				WorkspaceRoot: "/data/dl/project",
				WorkspaceKey:  "/data/dl/project",
				ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
					Model: "gpt-5.5",
					SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
						{ReasoningEffort: "high"},
						{ReasoningEffort: "low"},
					},
				}}},
				Threads: map[string]*state.ThreadRecord{},
			})
			for _, userID := range []string{"ou_a", "ou_b"} {
				surfaceID := "feishu:app-1:user:" + userID
				svc.MaterializeSurfaceResumeWithCodexProfile(surfaceID, "app-1", userID, userID, state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
				svc.root.Surfaces[surfaceID].AttachedInstanceID = "inst-1"
			}

			svc.ApplySurfaceAction(privateCapabilityAction(tc.firstKind, "app-1", "ou_a", tc.firstText))
			tc.stale(svc.root.Surfaces["feishu:app-1:user:ou_b"])
			svc.ApplySurfaceAction(privateCapabilityAction(tc.secondKind, "app-1", "ou_b", tc.secondText))

			record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
			if !tc.check(record) {
				t.Fatalf("interleaved bot capability record = %#v", record)
			}
			if record.PromptOverride.AccessMode != "" || record.PlanModeOverrideSet {
				t.Fatalf("access/plan must not be written to bot record: %#v", record)
			}
			if tc.name == "access then plan" {
				surfaceA := svc.root.Surfaces["feishu:app-1:user:ou_a"]
				surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
				if surfaceA.PromptOverride.AccessMode != agentproto.AccessModeConfirm ||
					surfaceB.PlanMode != state.PlanModeSettingOn || !surfaceB.PlanModeOverrideSet {
					t.Fatalf("session access/plan not isolated: %#v / %s/%v", surfaceA.PromptOverride, surfaceB.PlanMode, surfaceB.PlanModeOverrideSet)
				}
			}
		})
	}
}

func TestPrivateCapabilityCommandsRemainGatewayIsolated(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionPlanCommand, "app-1", "ou_a", "/plan on"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionModeCommand, "app-2", "ou_b", "/mode claude"))

	app1, ok1 := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	app2 := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-2")]
	if ok1 {
		t.Fatalf("session plan must not create bot capability record, got %#v", app1)
	}
	if app2.Backend != agentproto.BackendClaude || app2.PlanModeOverrideSet {
		t.Fatalf("app-2 bot capability record = %#v", app2)
	}
	ouA := svc.root.Surfaces["feishu:app-1:user:ou_a"]
	if ouA.PlanMode != state.PlanModeSettingOn || !ouA.PlanModeOverrideSet {
		t.Fatalf("app-1 surface plan = %s/%v, want on/true", ouA.PlanMode, ouA.PlanModeOverrideSet)
	}
}

func TestPrivateBackendSwitchPreservesInactiveProviderAndProfileSelections(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})

	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionCodexProfileCommand, "app-1", "ou_user", "/codexprofile team-proxy"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionModeCommand, "app-1", "ou_user", "/mode claude"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionClaudeProfileCommand, "app-1", "ou_user", "/claudeprofile devseek"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionModeCommand, "app-1", "ou_user", "/mode codex"))

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.Backend != agentproto.BackendCodex || record.CodexProfileID != "team-proxy" || record.ClaudeProfileID != "devseek" {
		t.Fatalf("bot capability selections = %#v, want active team-proxy and remembered devseek", record)
	}
}

func privateCapabilityAction(kind control.ActionKind, gatewayID, userID, text string) control.Action {
	return control.Action{
		Kind:             kind,
		SurfaceSessionID: "feishu:" + gatewayID + ":user:" + userID,
		GatewayID:        gatewayID,
		ChatID:           userID,
		ActorUserID:      userID,
		Text:             text,
	}
}

func TestBotCapabilityMutationRefreshesAllGatewaySurfaceProjections(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.ProductModeNormal, agentproto.BackendCodex, "codex-a", "profile-a", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.ProductModeNormal, agentproto.BackendCodex, "provider-b", "profile-b", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.root.Surfaces["feishu:app-1:user:ou_a"].ClaudeProfileID = "profile-a"
	svc.root.Surfaces["feishu:app-1:user:ou_b"].ClaudeProfileID = "profile-b"

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/mode claude",
	})
	surface := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	if surface.Backend != agentproto.BackendClaude {
		t.Fatalf("private surface backend projection = %q, want claude", surface.Backend)
	}
	if surface.CodexProfileID != "codex-a" || surface.ClaudeProfileID != "profile-a" {
		t.Fatalf("private surface profile/profile projection = %q/%q, want codex-a/profile-a", surface.CodexProfileID, surface.ClaudeProfileID)
	}
}

func TestMaterializeBotCapabilitySettingsRefreshesExistingSurfaceProjection(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "codex-old", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	svc.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
		GatewayID:           "app-1",
		ProductMode:         state.ProductModeNormal,
		Backend:             agentproto.BackendClaude,
		CodexProfileID:      "codex-new",
		ClaudeProfileID:     "profile-new",
		PromptOverride:      state.ModelConfigRecord{ReasoningEffort: "high"},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}})

	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	if surface.Backend != agentproto.BackendClaude || surface.CodexProfileID != "codex-new" || surface.ClaudeProfileID != "profile-new" {
		t.Fatalf("materialized contract projection = %s/%q/%q", surface.Backend, surface.CodexProfileID, surface.ClaudeProfileID)
	}
	if surface.PromptOverride.ReasoningEffort != "high" || surface.PlanModeOverrideSet {
		t.Fatalf("materialized projection must keep reasoning only; access/plan are session-scoped: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestProfileProjectionClearsStaleCodexDerivedCache(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.CodexAdmissionRef = &state.CodexAdmissionRef{ProfileRef: state.CodexProfileRef{ID: "default", Revision: 1}}
	surface.CodexConnectionContract = &state.CodexConnectionContract{ConnectionContractID: "old-contract"}
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProfileCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/codexprofile team-proxy",
	})

	if surface.CodexAdmissionRef != nil || surface.CodexConnectionContract != nil || surface.CodexThreadPolicy != nil {
		t.Fatalf("expected stale codex derived cache to be cleared on profile projection, got %#v", surface)
	}
	if surface.CodexProfileID != "team-proxy" {
		t.Fatalf("expected profile projection, got %q", surface.CodexProfileID)
	}
}

func TestBotCapabilitySettingsExportRejectsCrossGatewayRecord(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID: "app-2",
		Backend:   agentproto.BackendClaude,
	}

	if records := svc.BotCapabilitySettings(); len(records) != 0 {
		t.Fatalf("cross-gateway canonical record leaked into durable export: %#v", records)
	}
}

func TestSurfaceResumeMaterializeKeepsLoadedBotCapabilityProjection(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
		GatewayID:           "app-1",
		ProductMode:         state.ProductModeNormal,
		Backend:             agentproto.BackendClaude,
		CodexProfileID:      "codex-new",
		ClaudeProfileID:     "profile-new",
		PromptOverride:      state.ModelConfigRecord{ReasoningEffort: "high"},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}})

	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "codex-old", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	if surface.Backend != agentproto.BackendClaude || surface.CodexProfileID != "codex-new" || surface.ClaudeProfileID != "profile-new" {
		t.Fatalf("resume contract projection = %s/%q/%q", surface.Backend, surface.CodexProfileID, surface.ClaudeProfileID)
	}
	if surface.PromptOverride.ReasoningEffort != "high" || surface.PlanModeOverrideSet {
		t.Fatalf("resume projection must keep reasoning only; access/plan are session-scoped: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestMalformedFeishuPrivateIdentityCannotWriteBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "feishu:app-1:unknown:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/plan on",
	})

	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok {
		t.Fatalf("malformed Feishu identity must not create bot capability settings")
	}
	surface := svc.root.Surfaces["feishu:app-1:unknown:ou_user"]
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("malformed Feishu identity should keep local mutation semantics: %#v", surface)
	}
}

func TestGatewayMismatchedFeishuPrivateIdentityCannotWriteBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-2",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/plan on",
	})

	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-2")]; ok {
		t.Fatalf("gateway-mismatched Feishu identity must not create bot capability settings")
	}
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("gateway-mismatched Feishu identity should keep local mutation semantics: %#v", surface)
	}
}

func TestBotCapabilityTransactionFailureDoesNotFallbackToLocalMutation(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(privateCapabilityAction(control.ActionStatus, "app-1", "ou_user", ""))
	localMutationCalled := false

	ok := svc.applySurfaceCapabilitySettingsMutation(surface, func(record *state.BotCapabilitySettingsRecord) {
		record.GatewayID = ""
	}, func(*state.SurfaceConsoleRecord) {
		localMutationCalled = true
	})

	if ok {
		t.Fatal("invalid canonical transaction unexpectedly succeeded")
	}
	if localMutationCalled {
		t.Fatal("canonical transaction failure must not fall back to local mutation")
	}
	if len(svc.root.BotCapabilitySettings) != 0 {
		t.Fatalf("failed canonical transaction persisted state: %#v", svc.root.BotCapabilitySettings)
	}
}

func TestPrivateClaudeProfileSwitchWithoutWorkspacePreservesCanonicalPromptAndPlan(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "profile-b", Name: "Profile B"}})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendClaude,
		WorkspaceRoot: "/data/dl/project",
		WorkspaceKey:  "/data/dl/project",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionModeCommand, "app-1", "ou_a", "/mode claude"))
	surfaceB := svc.ensureSurface(privateCapabilityAction(control.ActionStatus, "app-1", "ou_b", ""))
	surfaceB.AttachedInstanceID = "inst-1"
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionAccessCommand, "app-1", "ou_b", "/access confirm"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionPlanCommand, "app-1", "ou_b", "/plan on"))
	svc.ApplySurfaceAction(privateCapabilityAction(control.ActionClaudeProfileCommand, "app-1", "ou_a", "/claudeprofile profile-b"))

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.ClaudeProfileID != "profile-b" {
		t.Fatalf("bot claude profile = %q, want profile-b", record.ClaudeProfileID)
	}
	if record.PromptOverride.AccessMode != "" || record.PlanModeOverrideSet {
		t.Fatalf("bot record must not carry session access/plan: %#v %s/%v", record.PromptOverride, record.PlanMode, record.PlanModeOverrideSet)
	}
	surfaceB = svc.root.Surfaces["feishu:app-1:user:ou_b"]
	if surfaceB.PromptOverride.AccessMode != agentproto.AccessModeConfirm || surfaceB.PlanMode != state.PlanModeSettingOn || !surfaceB.PlanModeOverrideSet {
		t.Fatalf("profile switch lost session access/plan on ou_b = %#v %s/%v", surfaceB.PromptOverride, surfaceB.PlanMode, surfaceB.PlanModeOverrideSet)
	}
}

func TestPrivateClaudeWorkspaceSnapshotRestoreUpdatesCanonicalProjection(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		PromptOverride: state.ModelConfigRecord{
			Model:           "clear-me",
			ReasoningEffort: "low",
		},
	}
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
	})
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(surface.ClaimedWorkspaceKey, agentproto.BackendClaude, "profile-a")
	svc.root.ClaudeWorkspaceProfileSnapshots[key] = state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}

	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	want := state.ModelConfigRecord{ReasoningEffort: "high"}
	if record.PromptOverride != want || record.PlanModeOverrideSet {
		t.Fatalf("bot snapshot projection = %#v %s/%v, want %#v off/false", record.PromptOverride, record.PlanMode, record.PlanModeOverrideSet, want)
	}
}

func TestGroupClaudeWorkspaceSnapshotRestoreUpdatesExistingCanonicalProjection(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 1, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		PromptOverride: state.ModelConfigRecord{
			ReasoningEffort: "low",
		},
	}
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
	})
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(surface.ClaimedWorkspaceKey, agentproto.BackendClaude, "profile-a")
	svc.root.ClaudeWorkspaceProfileSnapshots[key] = state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeFullAccess,
	}

	svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	wantRecord := state.ModelConfigRecord{ReasoningEffort: "high"}
	if record.PromptOverride != wantRecord || record.PlanModeOverrideSet {
		t.Fatalf("bot snapshot projection = %#v %s/%v, want %#v off/false", record.PromptOverride, record.PlanMode, record.PlanModeOverrideSet, wantRecord)
	}
	wantSurface := state.ModelConfigRecord{ReasoningEffort: "high", AccessMode: agentproto.AccessModeFullAccess}
	if surface.PromptOverride != wantSurface || surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("group execution projection = %#v %s/%v, want canonical %#v off/false", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet, wantSurface)
	}
}

func TestGroupLifecycleMutationWithoutCanonicalRecordStaysLocal(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
	})
	surface.Backend = agentproto.BackendClaude
	surface.ClaudeProfileID = "profile-a"
	surface.PromptOverride = state.ModelConfigRecord{ReasoningEffort: "high"}
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	svc.clearLifecyclePlanModeOverride(surface)

	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok {
		t.Fatal("group lifecycle mutation must not initialize canonical bot settings from a surface snapshot")
	}
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("local lifecycle projection = %s/%v, want off/false", surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestInvalidCanonicalRecordBlocksLifecycleAndSurfaceAction(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 3, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surfaceID := "feishu:app-1:user:ou_user"
	surface := &state.SurfaceConsoleRecord{
		SurfaceSessionID:    surfaceID,
		Platform:            "feishu",
		GatewayID:           "app-1",
		ChatID:              "ou_user",
		ActorUserID:         "ou_user",
		ProductMode:         state.ProductModeNormal,
		Backend:             agentproto.BackendClaude,
		ClaudeProfileID:     "stale-local",
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
		QueueItems:          map[string]*state.QueueItemRecord{},
		StagedImages:        map[string]*state.StagedImageRecord{},
		StagedFiles:         map[string]*state.StagedFileRecord{},
		PendingRequests:     map[string]*state.RequestPromptRecord{},
		SurfaceMessages:     map[string]*state.SurfaceMessageRecord{},
	}
	svc.root.Surfaces[surfaceID] = surface
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID: "app-2",
		Backend:   agentproto.BackendCodex,
	}

	// plan 为会话级设置，与 canonical record 有效性解耦。
	if !svc.clearLifecyclePlanModeOverride(surface) {
		t.Fatal("expected session plan clear to succeed")
	}
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("session plan clear changed projection: %s/%v", surface.PlanMode, surface.PlanModeOverrideSet)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: surfaceID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/plan off",
	})
	if eventsContainNotice(events, "bot_capability_settings_invalid", "机器人设置") {
		t.Fatalf("session plan command must not be blocked by invalid canonical record, got %#v", events)
	}
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("session plan command changed projection: %s/%v", surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestInvalidCanonicalRecordBlocksClaudeSnapshotRestore(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 4, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := &state.SurfaceConsoleRecord{
		SurfaceSessionID:    "feishu:app-1:chat:oc_room",
		Platform:            "feishu",
		GatewayID:           "app-1",
		ChatID:              "oc_room",
		ActorUserID:         "ou_user",
		ProductMode:         state.ProductModeNormal,
		Backend:             agentproto.BackendClaude,
		ClaudeProfileID:     "profile-a",
		ClaimedWorkspaceKey: "/data/dl/repo-a",
		PromptOverride:      state.ModelConfigRecord{ReasoningEffort: "low"},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{}
	svc.root.ClaudeWorkspaceProfileSnapshots[state.ClaudeWorkspaceProfileSnapshotStorageKey("/data/dl/repo-a", agentproto.BackendClaude, "profile-a")] = state.ClaudeWorkspaceProfileSnapshotRecord{
		ReasoningEffort: "high",
	}

	events := svc.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)

	if !eventsContainNotice(events, "bot_capability_settings_invalid", "机器人设置") {
		t.Fatalf("expected invalid canonical state notice, got %#v", events)
	}
	if surface.PromptOverride.ReasoningEffort != "low" || surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("failed snapshot restore changed raw projection: %#v plan=%s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
	}
}

func TestInvalidCanonicalRecordBlocksQueuedDispatch(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 4, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/repo-a",
		WorkspaceKey:  "/data/dl/repo-a",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	surface := &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "feishu:app-1:user:ou_user",
		Platform:           "feishu",
		GatewayID:          "app-1",
		ChatID:             "ou_user",
		ActorUserID:        "ou_user",
		AttachedInstanceID: "inst-1",
		DispatchMode:       state.DispatchModeNormal,
		QueueItems: map[string]*state.QueueItemRecord{
			"queue-1": {
				ID:               "queue-1",
				SurfaceSessionID: "feishu:app-1:user:ou_user",
				Status:           state.QueueItemQueued,
				Inputs:           []agentproto.Input{{Type: agentproto.InputText, Text: "do it"}},
			},
		},
		QueuedQueueItemIDs: []string{"queue-1"},
		StagedImages:       map[string]*state.StagedImageRecord{},
		StagedFiles:        map[string]*state.StagedFileRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
		SurfaceMessages:    map[string]*state.SurfaceMessageRecord{},
	}
	svc.root.Surfaces[surface.SurfaceSessionID] = surface
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{}

	events := svc.dispatchNext(surface)

	for _, event := range events {
		if event.Kind == eventcontract.KindAgentCommand {
			t.Fatalf("invalid canonical settings dispatched queued prompt: %#v", events)
		}
	}
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("invalid canonical settings advanced queue: active=%q queued=%#v", surface.ActiveQueueItemID, surface.QueuedQueueItemIDs)
	}
}

func TestInvalidCanonicalRecordBlocksTickAutoContinueDispatch(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 4, 45, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		WorkspaceRoot:           "/data/dl/repo-a",
		WorkspaceKey:            "/data/dl/repo-a",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", CWD: "/data/dl/repo-a", Loaded: true},
		},
	})
	surfaceID := "feishu:app-1:user:ou_user"
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: surfaceID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		InstanceID:       "inst-1",
	})
	surface := svc.root.Surfaces[surfaceID]
	surface.AutoContinue.Enabled = true
	surface.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{
		EpisodeID:          "autocontinue-1",
		InstanceID:         "inst-1",
		FrozenDispatchPlan: agentproto.PromptDispatchPlan{ExecutionThreadID: "thread-1", CWD: "/data/dl/repo-a"},
		FrozenRouteMode:    state.RouteModePinned,
		State:              state.AutoContinueEpisodeScheduled,
		PendingDueAt:       now,
	}
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{}

	events := svc.Tick(now)

	for _, event := range events {
		if event.Kind == eventcontract.KindAgentCommand {
			t.Fatalf("invalid canonical settings dispatched AutoContinue from Tick: %#v", events)
		}
	}
	if surface.ActiveQueueItemID != "" || surface.AutoContinue.Episode.State != state.AutoContinueEpisodeScheduled {
		t.Fatalf("invalid canonical settings advanced AutoContinue: active=%q episode=%#v", surface.ActiveQueueItemID, surface.AutoContinue.Episode)
	}
	if !eventsContainNotice(events, "bot_capability_settings_invalid", "机器人设置") {
		t.Fatalf("expected invalid canonical state notice, got %#v", events)
	}
}

func TestPrivateClaudeWorkspaceSnapshotPersistsCanonicalProjection(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		PromptOverride: state.ModelConfigRecord{
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeConfirm,
		},
	}
	surface := svc.ensureSurface(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
	})
	surface.ClaimedWorkspaceKey = "/data/dl/repo-a"
	surface.PromptOverride = state.ModelConfigRecord{
		ReasoningEffort: "low",
		AccessMode:      agentproto.AccessModeFullAccess,
	}

	svc.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	key := state.ClaudeWorkspaceProfileSnapshotStorageKey(surface.ClaimedWorkspaceKey, agentproto.BackendClaude, "profile-a")
	want := state.ClaudeWorkspaceProfileSnapshotRecord{ReasoningEffort: "high", AccessMode: agentproto.AccessModeFullAccess}
	if got := svc.root.ClaudeWorkspaceProfileSnapshots[key]; got != want {
		t.Fatalf("persisted snapshot = %#v, want canonical %#v", got, want)
	}
}

func TestPrivateAccessCommandWritesSurfaceSettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendCodex,
		WorkspaceRoot: "/data/dl/project",
		WorkspaceKey:  "/data/dl/project",
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "gpt-5.5",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "high"},
					{ReasoningEffort: "low"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user",
		"app-1",
		"ou_user",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"default",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/access confirm",
	})
	if len(events) == 0 {
		t.Fatalf("expected feedback event")
	}

	if record, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok && record.PromptOverride.AccessMode != "" {
		t.Fatalf("access must not be written to bot record, got %#v", record)
	}
	if surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("private surface access override = %q, want confirm", surface.PromptOverride.AccessMode)
	}
}

func TestGroupPromptSummaryUsesBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendCodex,
		WorkspaceRoot: "/data/dl/project",
		WorkspaceKey:  "/data/dl/project",
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:      "app-1",
		ProductMode:    state.ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "default",
		PromptOverride: state.ModelConfigRecord{
			Model:           "gpt-5.5",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeConfirm,
		},
		PlanMode:            state.PlanModeSettingOn,
		PlanModeOverrideSet: true,
	}
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:chat:oc_room",
		"app-1",
		"oc_room",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"team-proxy",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
	surface.AttachedInstanceID = "inst-1"
	surface.PromptOverride.AccessMode = agentproto.AccessModeConfirm
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	summary := svc.resolveNextPromptSummary(svc.root.Instances["inst-1"], surface, "", "", state.ModelConfigRecord{})
	if summary.OverrideModel != "gpt-5.5" || summary.OverrideReasoningEffort != "high" {
		t.Fatalf("summary override = %q/%q, want gpt-5.5/high", summary.OverrideModel, summary.OverrideReasoningEffort)
	}
	if summary.EffectiveAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("summary access = %q, want confirm", summary.EffectiveAccessMode)
	}
	if summary.EffectivePlanMode != string(state.PlanModeSettingOn) || !summary.PlanModeOverrideSet {
		t.Fatalf("summary plan = %q/%v, want on/true", summary.EffectivePlanMode, summary.PlanModeOverrideSet)
	}
}

func TestGroupSurfaceBotCapabilitySettingsAreGatewayScoped(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:       "app-1",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "devseek",
	}
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-2")] = state.BotCapabilitySettingsRecord{
		GatewayID:      "app-2",
		ProductMode:    state.ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: "team-proxy",
	}
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-2:chat:oc_room", "app-2", "oc_room", "ou_2", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	if got := svc.SurfaceBackend("feishu:app-1:chat:oc_room"); got != agentproto.BackendClaude {
		t.Fatalf("app-1 SurfaceBackend = %s, want claude", got)
	}
	if got := svc.SurfaceBackend("feishu:app-2:chat:oc_room"); got != agentproto.BackendCodex {
		t.Fatalf("app-2 SurfaceBackend = %s, want codex", got)
	}
	if got := svc.SurfaceCodexProfileID("feishu:app-2:chat:oc_room"); got != "team-proxy" {
		t.Fatalf("app-2 SurfaceCodexProfileID = %q, want team-proxy", got)
	}
}

func TestPrivateProviderAndProfileCommandsWriteBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionCodexProfileCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/codexprofile team-proxy"})
	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.CodexProfileID != "team-proxy" {
		t.Fatalf("bot codex profile = %q, want team-proxy", record.CodexProfileID)
	}

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionClaudeProfileCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/claudeprofile devseek"})
	record = svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.Backend != agentproto.BackendClaude || record.ClaudeProfileID != "devseek" {
		t.Fatalf("bot claude profile = %s/%q, want claude/devseek", record.Backend, record.ClaudeProfileID)
	}
}

func TestPrivateModelAndReasoningCommandsWriteBotCapabilitySettings(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		Backend:       agentproto.BackendCodex,
		WorkspaceRoot: "/data/dl/project",
		WorkspaceKey:  "/data/dl/project",
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "gpt-5.5",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "high"},
					{ReasoningEffort: "low"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.AttachedInstanceID = "inst-1"

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/model gpt-5.5 high"})
	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.PromptOverride.Model != "gpt-5.5" || record.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("bot model/reasoning = %#v, want gpt-5.5/high", record.PromptOverride)
	}

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionReasoningCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "ou_user", ActorUserID: "ou_user", Text: "/reasoning low"})
	record = svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.PromptOverride.ReasoningEffort != "low" {
		t.Fatalf("bot reasoning = %q, want low", record.PromptOverride.ReasoningEffort)
	}
}

func TestGroupCapabilityCommandsRejectMutation(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		kind   control.ActionKind
		text   string
		reject bool
	}{
		{name: "mode", kind: control.ActionModeCommand, text: "/mode claude", reject: true},
		{name: "codex profile", kind: control.ActionCodexProfileCommand, text: "/codexprofile team-proxy", reject: true},
		{name: "claude profile", kind: control.ActionClaudeProfileCommand, text: "/claudeprofile devseek", reject: true},
		{name: "model", kind: control.ActionModelCommand, text: "/model gpt-5.5 high", reject: true},
		{name: "reasoning", kind: control.ActionReasoningCommand, text: "/reasoning low", reject: true},
		{name: "access", kind: control.ActionAccessCommand, text: "/access confirm", reject: false},
		{name: "plan", kind: control.ActionPlanCommand, text: "/plan on", reject: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newServiceForTest(&now)
			materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
			svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{{ID: "devseek", Name: "DevSeek"}})
			svc.UpsertInstance(&state.InstanceRecord{
				InstanceID:    "inst-1",
				Backend:       agentproto.BackendCodex,
				WorkspaceRoot: "/data/dl/project",
				WorkspaceKey:  "/data/dl/project",
				ModelCatalog: &agentproto.ModelCatalogSnapshot{
					Entries: []agentproto.ModelCatalogEntry{{
						Model: "gpt-5.5",
						SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
							{ReasoningEffort: "high"},
							{ReasoningEffort: "low"},
						},
					}},
				},
				Threads: map[string]*state.ThreadRecord{},
			})
			svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
				GatewayID:      "app-1",
				ProductMode:    state.ProductModeNormal,
				Backend:        agentproto.BackendCodex,
				CodexProfileID: state.NativeCodexProfileID,
				PlanMode:       state.PlanModeSettingOff,
			}
			svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, state.NativeCodexProfileID, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
			surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]
			surface.AttachedInstanceID = "inst-1"

			events := svc.ApplySurfaceAction(control.Action{
				Kind:             tc.kind,
				SurfaceSessionID: surface.SurfaceSessionID,
				GatewayID:        "app-1",
				ChatID:           "oc_room",
				ActorUserID:      "ou_user",
				Text:             tc.text,
			})

			record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
			if tc.reject {
				if !eventsContainNotice(events, "bot_capability_private_required", "私聊") {
					t.Fatalf("expected private-chat rejection notice, got %#v", events)
				}
			} else if eventsContainNotice(events, "bot_capability_private_required", "私聊") {
				t.Fatalf("session-scoped command must not be rejected in group chat, got %#v", events)
			}
			if record.Backend != agentproto.BackendCodex || record.CodexProfileID != state.NativeCodexProfileID || record.ClaudeProfileID != "" ||
				record.PromptOverride != (state.ModelConfigRecord{}) || record.PlanModeOverrideSet {
				t.Fatalf("bot capability settings mutated after %s: %#v", tc.text, record)
			}
			if tc.reject {
				if surface.PromptOverride != (state.ModelConfigRecord{}) || surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
					t.Fatalf("rejected command mutated session state: %#v %s/%v", surface.PromptOverride, surface.PlanMode, surface.PlanModeOverrideSet)
				}
				return
			}
			if tc.kind == control.ActionAccessCommand && surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
				t.Fatalf("group access should apply to session surface, got %#v", surface.PromptOverride)
			}
			if tc.kind == control.ActionPlanCommand && (surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet) {
				t.Fatalf("group plan should apply to session surface, got %s/%v", surface.PlanMode, surface.PlanModeOverrideSet)
			}
			if surface.Backend != agentproto.BackendCodex || surface.CodexProfileID != state.NativeCodexProfileID || surface.ClaudeProfileID != "" ||
				surface.PromptOverride.Model != "" || surface.PromptOverride.ReasoningEffort != "" {
				t.Fatalf("group capability command mutated bot-scoped session fields: %#v", surface)
			}
		})
	}
}
func TestGroupContextCommandsRemainMutable(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAutoWhipCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "oc_room", ActorUserID: "ou_user", Text: "/autowhip on"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAutoContinueCommand, SurfaceSessionID: surface.SurfaceSessionID, GatewayID: "app-1", ChatID: "oc_room", ActorUserID: "ou_user", Text: "/autocontinue on"})

	if !surface.AutoWhip.Enabled {
		t.Fatalf("expected group autowhip to remain mutable")
	}
	if !surface.AutoContinue.Enabled {
		t.Fatalf("expected group autocontinue to remain mutable")
	}
	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok {
		t.Fatalf("context commands should not create bot capability settings")
	}
}

func TestGroupCapabilityCardCallbackRejectsInline(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")] = state.BotCapabilitySettingsRecord{
		GatewayID:      "app-1",
		ProductMode:    state.ProductModeNormal,
		Backend:        agentproto.BackendCodex,
		CodexProfileID: state.NativeCodexProfileID,
	}
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:chat:oc_room", "app-1", "oc_room", "ou_user", state.ProductModeNormal, agentproto.BackendCodex, state.NativeCodexProfileID, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	surface := svc.root.Surfaces["feishu:app-1:chat:oc_room"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/mode claude",
		CatalogFamilyID:  control.FeishuCommandMode,
		CatalogVariantID: "mode.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if len(events) != 1 || events[0].PageView == nil || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected inline page rejection, got %#v", events)
	}
	if !strings.Contains(commandCatalogSummaryText(events[0].PageView), "私聊") {
		t.Fatalf("expected inline rejection to mention private chat, got %#v", events[0].PageView)
	}
	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.Backend != agentproto.BackendCodex || record.CodexProfileID != state.NativeCodexProfileID {
		t.Fatalf("bot capability settings mutated by card callback: %#v", record)
	}
	if surface.Backend != agentproto.BackendCodex || surface.CodexProfileID != state.NativeCodexProfileID {
		t.Fatalf("group surface capability state mutated by card callback: %#v", surface)
	}
}

func eventsContainNotice(events []eventcontract.Event, code, text string) bool {
	for _, event := range events {
		if event.Notice == nil || event.Notice.Code != code {
			continue
		}
		if text == "" || strings.Contains(event.Notice.Text, text) {
			return true
		}
	}
	return false
}
