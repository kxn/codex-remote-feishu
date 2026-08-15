package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDetachedFeishuPrivatePromptSettingViewsFollowBackendRuntimeRequirements(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name               string
		contract           state.SurfaceBackendContract
		commandID          string
		wantAttachment     bool
		wantStatusContains string
	}{
		{
			name:               "codex model allows manual input",
			contract:           state.HeadlessCodexSurfaceBackendContract(state.NativeCodexProfileID),
			commandID:          control.FeishuCommandModel,
			wantStatusContains: "只能手动输入模型名",
		},
		{
			name:               "codex reasoning allows unvalidated input",
			contract:           state.HeadlessCodexSurfaceBackendContract(state.NativeCodexProfileID),
			commandID:          control.FeishuCommandReasoning,
			wantStatusContains: "卡片只提供自动",
		},
		{
			name:      "codex access is bot owned",
			contract:  state.HeadlessCodexSurfaceBackendContract(state.NativeCodexProfileID),
			commandID: control.FeishuCommandAccess,
		},
		{
			name:      "claude reasoning is bot owned",
			contract:  state.HeadlessClaudeSurfaceBackendContract("devseek"),
			commandID: control.FeishuCommandReasoning,
		},
		{
			name:      "claude access is bot owned",
			contract:  state.HeadlessClaudeSurfaceBackendContract("devseek"),
			commandID: control.FeishuCommandAccess,
		},
		{
			name:      "opencode access is bot owned",
			contract:  state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
			commandID: control.FeishuCommandAccess,
		},
		{
			name:           "opencode reasoning needs acp evidence",
			contract:       state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
			commandID:      control.FeishuCommandReasoning,
			wantAttachment: true,
		},
		{
			name:           "vscode model remains instance owned",
			contract:       state.VSCodeSurfaceBackendContract(),
			commandID:      control.FeishuCommandModel,
			wantAttachment: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newServiceForTest(&now)
			svc.MaterializeSurfaceResumeContract(
				"feishu:app-1:user:ou_user",
				"app-1",
				"ou_user",
				"ou_user",
				tt.contract,
				state.SurfaceVerbosityNormal,
				state.PlanModeSettingOff,
			)
			flow, ok := control.FeishuConfigFlowDefinitionByCommandID(tt.commandID)
			if !ok {
				t.Fatalf("missing config flow for %s", tt.commandID)
			}

			view := svc.buildConfigCommandViewState(svc.root.Surfaces["feishu:app-1:user:ou_user"], flow, control.FeishuCatalogConfigView{})
			if view.Config == nil {
				t.Fatal("expected config view")
			}
			if view.Config.RequiresAttachment != tt.wantAttachment {
				t.Fatalf("RequiresAttachment = %v, want %v: %#v", view.Config.RequiresAttachment, tt.wantAttachment, view.Config)
			}
			if tt.wantStatusContains != "" && !strings.Contains(view.Config.StatusText, tt.wantStatusContains) {
				t.Fatalf("status = %q, want %q", view.Config.StatusText, tt.wantStatusContains)
			}
		})
	}
}

func TestDetachedFeishuPrivateCodexPromptCommandsCreateAndProjectBotSettings(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:chat:oc_room",
		"app-1",
		"oc_room",
		"ou_group_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		state.NativeCodexProfileID,
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user",
		"app-1",
		"ou_user",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		state.NativeCodexProfileID,
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]

	modelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/model gpt-5.5 high",
		CatalogFamilyID:  control.FeishuCommandModel,
		CatalogVariantID: "model.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	if len(modelEvents) != 1 || modelEvents[0].PageView == nil || !modelEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected detached callback to update inline, got %#v", modelEvents)
	}
	if summary := commandCatalogSummaryText(modelEvents[0].PageView); !strings.Contains(summary, "当前还没有接管 Codex 实例") {
		t.Fatalf("expected detached model validation warning, got %q", summary)
	}

	for _, action := range []control.Action{
		{
			Kind:             control.ActionReasoningCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			GatewayID:        "app-1",
			ChatID:           "ou_user",
			ActorUserID:      "ou_user",
			Text:             "/reasoning low",
		},
		{
			Kind:             control.ActionAccessCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			GatewayID:        "app-1",
			ChatID:           "ou_user",
			ActorUserID:      "ou_user",
			Text:             "/access confirm",
		},
	} {
		events := svc.ApplySurfaceAction(action)
		if eventsContainNotice(events, "not_attached", "") {
			t.Fatalf("detached bot setting %q was rejected: %#v", action.Text, events)
		}
	}

	record, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if !ok {
		t.Fatal("expected first detached mutation to create bot capability settings")
	}
	wantBot := state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "low",
	}
	wantSurface := state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "low",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	if record.PromptOverride != wantBot || surface.PromptOverride != wantSurface {
		t.Fatalf("private bot settings = %#v / %#v, want %#v / %#v", record.PromptOverride, surface.PromptOverride, wantBot, wantSurface)
	}
	if group := svc.root.Surfaces["feishu:app-1:chat:oc_room"]; group.PromptOverride != wantBot {
		t.Fatalf("group projection = %#v, want bot-scoped %#v", group.PromptOverride, wantBot)
	}

	for _, tt := range []struct {
		commandID         string
		wantOverride      string
		wantExtraOverride string
	}{
		{commandID: control.FeishuCommandModel, wantOverride: "gpt-5.5", wantExtraOverride: "low"},
		{commandID: control.FeishuCommandReasoning, wantOverride: "low"},
		{commandID: control.FeishuCommandAccess, wantOverride: agentproto.AccessModeConfirm},
	} {
		flow, ok := control.FeishuConfigFlowDefinitionByCommandID(tt.commandID)
		if !ok {
			t.Fatalf("missing config flow for %s", tt.commandID)
		}
		view := svc.buildConfigCommandViewState(surface, flow, control.FeishuCatalogConfigView{})
		if view.Config == nil || view.Config.RequiresAttachment {
			t.Fatalf("detached %s view unavailable: %#v", tt.commandID, view.Config)
		}
		if view.Config.OverrideValue != tt.wantOverride || view.Config.OverrideExtraValue != tt.wantExtraOverride {
			t.Fatalf("detached %s summary override = %q/%q, want %q/%q", tt.commandID, view.Config.OverrideValue, view.Config.OverrideExtraValue, tt.wantOverride, tt.wantExtraOverride)
		}
	}

	clearEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/model clear",
	})
	if eventsContainNotice(clearEvents, "not_attached", "") {
		t.Fatalf("detached model clear was rejected: %#v", clearEvents)
	}
	record = svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("model clear should leave empty bot record override, got %#v", record.PromptOverride)
	}
}

func TestDetachedFeishuPrivateClaudePromptCommandsDoNotNeedWorkspaceSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract(
		"feishu:app-1:user:ou_user",
		"app-1",
		"ou_user",
		"ou_user",
		state.HeadlessClaudeSurfaceBackendContract("devseek"),
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]

	for _, action := range []control.Action{
		{
			Kind:             control.ActionReasoningCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			GatewayID:        "app-1",
			ChatID:           "ou_user",
			ActorUserID:      "ou_user",
			Text:             "/reasoning max",
		},
		{
			Kind:             control.ActionAccessCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			GatewayID:        "app-1",
			ChatID:           "ou_user",
			ActorUserID:      "ou_user",
			Text:             "/access confirm",
		},
	} {
		events := svc.ApplySurfaceAction(action)
		if eventsContainNotice(events, "not_attached", "") {
			t.Fatalf("detached Claude setting %q was rejected: %#v", action.Text, events)
		}
	}

	record := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if record.PromptOverride.ReasoningEffort != "max" || record.PromptOverride.AccessMode != "" {
		t.Fatalf("Claude bot override must keep reasoning only, got %#v", record.PromptOverride)
	}
	if surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("session access = %q, want confirm", surface.PromptOverride.AccessMode)
	}
	if len(svc.root.ClaudeWorkspaceProfileSnapshots) != 0 {
		t.Fatalf("detached settings should not invent a workspace snapshot: %#v", svc.root.ClaudeWorkspaceProfileSnapshots)
	}
}

func TestDetachedFeishuPrivateOpenCodeReasoningStillRequiresRuntimeEvidence(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract(
		"feishu:app-1:user:ou_user",
		"app-1",
		"ou_user",
		"ou_user",
		state.HeadlessOpenCodeSurfaceBackendContract("op_team"),
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/reasoning high",
	})
	if !eventsContainNotice(events, "not_attached", "") {
		t.Fatalf("expected detached OpenCode reasoning to fail closed, got %#v", events)
	}
	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok {
		t.Fatalf("rejected reasoning must not create bot settings")
	}
}

func TestDetachedNonFeishuPromptSettingsStillRequireAttachment(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "", "chat-1", "user-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.5",
	})
	if !eventsContainNotice(events, "not_attached", "") {
		t.Fatalf("expected non-Feishu detached model command to retain attachment gate, got %#v", events)
	}
}

func TestDetachedFeishuPrivateCodexFixedProfileStillRejectsOtherModel(t *testing.T) {
	now := time.Date(2026, 8, 15, 9, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "fixed", Kind: state.CodexProfileKindAPI, Name: "Fixed", Model: "provider-custom", ReasoningEffort: "high", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile(
		"feishu:app-1:user:ou_user",
		"app-1",
		"ou_user",
		"ou_user",
		state.ProductModeNormal,
		agentproto.BackendCodex,
		"fixed",
		"",
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/model another-model",
	})
	if len(events) != 1 || events[0].PageView == nil || !strings.Contains(commandCatalogSummaryText(events[0].PageView), "固定模型 provider-custom") {
		t.Fatalf("expected fixed profile rejection, got %#v", events)
	}
	if _, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]; ok {
		t.Fatalf("fixed profile rejection must not create bot settings")
	}
}
