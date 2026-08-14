package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestModelCommandClearsIncompatibleReasoningForKnownCatalogModel(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{
				{
					Model: "model-a",
					SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
						{ReasoningEffort: "high"},
					},
				},
				{
					Model: "model-b",
					SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
						{ReasoningEffort: "low"},
					},
				},
			},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "model-a", ReasoningEffort: "high"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model model-b",
	})
	if surface.PromptOverride.Model != "model-b" || surface.PromptOverride.ReasoningEffort != "" {
		t.Fatalf("expected model switch to clear incompatible reasoning, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "已回到模型默认思考强度") {
		t.Fatalf("expected cleanup notice, got %#v", events)
	}
}

func TestModelCommandRejectsKnownCatalogUnsupportedReasoningTuple(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "model-a",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "medium"},
					{ReasoningEffort: "high"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model model-a low",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "当前模型不支持这个推理强度") || !strings.Contains(summary, "medium、high") {
		t.Fatalf("expected supported efforts in error summary, got %q", summary)
	}
	if got := svc.root.Surfaces["surface-1"].PromptOverride; got != (state.ModelConfigRecord{}) {
		t.Fatalf("expected rejected tuple not to mutate override, got %#v", got)
	}
}

func TestReasoningCommandRejectsUnsupportedEffortForCurrentKnownModel(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "model-a",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "medium"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].PromptOverride.Model = "model-a"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning high",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "当前模型不支持这个推理强度") || !strings.Contains(summary, "medium") {
		t.Fatalf("expected model-scoped reasoning rejection, got %q", summary)
	}
	if got := svc.root.Surfaces["surface-1"].PromptOverride.ReasoningEffort; got != "" {
		t.Fatalf("expected rejected reasoning not to mutate override, got %q", got)
	}
}

func TestUnknownModelReasoningOverrideIsPreservedWithValidationWarning(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "known-model",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "medium"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model future-model max",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PromptOverride.Model != "future-model" || surface.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("expected unknown model advanced override to be preserved, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "无法本地校验") {
		t.Fatalf("expected validation warning notice, got %#v", events)
	}
}

func TestPromptSendDispatchDropsKnownIncompatibleReasoningOverride(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "model-a",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "low"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	item := &state.QueueItemRecord{
		ID:               "queue-1",
		SurfaceSessionID: "surface-1",
		FrozenOverride: state.ModelConfigRecord{
			Model:           "model-a",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
	}

	command, guardEvents := svc.promptSendCommandAndGuardEventsFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.Model != "model-a" || command.Overrides.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected model/access to be preserved, got %#v", command.Overrides)
	}
	if command.Overrides.ReasoningEffort != "" {
		t.Fatalf("expected incompatible reasoning to be dropped before dispatch, got %#v", command.Overrides)
	}
	if len(guardEvents) != 1 || guardEvents[0].Notice == nil {
		t.Fatalf("expected guard notice, got %#v", guardEvents)
	}
	if guardEvents[0].Notice.Code != "prompt_override_reasoning_dropped" ||
		guardEvents[0].Notice.DeliveryFamily != control.NoticeDeliveryFamilyPromptOverrideGuard ||
		guardEvents[0].Notice.DeliveryDedupKey == "" ||
		!strings.Contains(guardEvents[0].Notice.Text, "已改用模型默认思考强度") {
		t.Fatalf("unexpected guard notice: %#v", guardEvents[0].Notice)
	}
}

func TestModelCommandRejectsOtherModelForFixedCodexAPIProfile(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", ReasoningEffort: "high", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "custom-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Backend:    agentproto.BackendCodex,
		Online:     true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{
			{Model: "gpt-5.5", DisplayName: "GPT 5.5"},
		}},
		Threads: map[string]*state.ThreadRecord{},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.5",
	})

	catalog := commandCatalogFromEvent(t, events[0])
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "当前 Codex Profile 使用固定模型 provider-custom") {
		t.Fatalf("expected fixed profile rejection, got %q", summary)
	}
	if got := surface.PromptOverride; got != (state.ModelConfigRecord{}) {
		t.Fatalf("expected rejected fixed profile model not to mutate override, got %#v", got)
	}
}

func TestModelCommandRejectsReasoningForFixedCodexAPIProfileWithoutReasoning(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 22, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "custom-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Backend:    agentproto.BackendCodex,
		Online:     true,
		Threads:    map[string]*state.ThreadRecord{},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model provider-custom high",
	})

	catalog := commandCatalogFromEvent(t, events[0])
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "未配置固定推理强度时请保持自动") {
		t.Fatalf("expected fixed profile reasoning rejection, got %q", summary)
	}
	if got := surface.PromptOverride; got != (state.ModelConfigRecord{}) {
		t.Fatalf("expected rejected fixed profile reasoning not to mutate override, got %#v", got)
	}
}

func TestPromptSendDispatchDropsMismatchedModelOverrideForFixedCodexAPIProfile(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", ReasoningEffort: "high", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "custom-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Backend:    agentproto.BackendCodex,
		Online:     true,
		Threads:    map[string]*state.ThreadRecord{},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	item := &state.QueueItemRecord{
		ID:               "queue-1",
		SurfaceSessionID: "surface-1",
		FrozenOverride: state.ModelConfigRecord{
			Model:           "gpt-5.5",
			ReasoningEffort: "high",
			AccessMode:      agentproto.AccessModeFullAccess,
		},
	}

	command, guardEvents := svc.promptSendCommandAndGuardEventsFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.Model != "" || command.Overrides.ReasoningEffort != "" || command.Overrides.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected fixed profile to drop model/reasoning overrides only, got %#v", command.Overrides)
	}
	if len(guardEvents) != 1 || guardEvents[0].Notice == nil || guardEvents[0].Notice.Code != "prompt_override_model_dropped" {
		t.Fatalf("expected fixed profile model guard notice, got %#v", guardEvents)
	}
	if !strings.Contains(guardEvents[0].Notice.Text, "provider-custom") {
		t.Fatalf("expected guard notice to mention fixed model, got %#v", guardEvents[0].Notice)
	}
}

func TestReasoningCardUsesFixedCodexAPIProfileReasoning(t *testing.T) {
	now := time.Date(2026, 8, 2, 1, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{ID: "custom-profile", Kind: state.CodexProfileKindAPI, Name: "Custom API", Model: "provider-custom", ReasoningEffort: "high", Available: true},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "custom-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "gpt-5.5",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "medium"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 2 || buttons[0].CommandText != "/reasoning clear" || buttons[1].CommandText != "/reasoning high" {
		t.Fatalf("expected automatic + fixed profile reasoning option, got %#v", buttons)
	}
	if summary := commandCatalogSummaryText(catalog); strings.Contains(summary, "gpt-5.5") {
		t.Fatalf("fixed profile reasoning card should not describe stale GPT catalog, got %q", summary)
	}
}

func TestReasoningCardUsesDynamicCatalogForDeepSeekCodexAPIProfile(t *testing.T) {
	now := time.Date(2026, 8, 2, 2, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeCodexProfiles([]state.CodexProfileSummary{
		{ID: state.NativeCodexProfileID, Kind: state.CodexProfileKindNative, Name: "本机默认", Available: true},
		{
			ID: "deepseek-profile", Kind: state.CodexProfileKindAPI, Name: "DeepSeek",
			BaseURL: "https://api.deepseek.com/", Model: "deepseek-v4-flash", ReasoningEffort: "high", Available: true,
		},
	})
	svc.MaterializeSurfaceResumeWithCodexProfile("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "deepseek-profile", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model:                  "deepseek-v4-pro",
			DefaultReasoningEffort: "high",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
				{ReasoningEffort: "max"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{},
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.PromptOverride.Model = "deepseek-v4-pro"

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandReasoning)
	if !ok {
		t.Fatal("expected reasoning config flow")
	}
	view := svc.buildConfigCommandViewState(surface, flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	got := make([]string, 0, len(view.Config.FormOptions))
	for _, option := range view.Config.FormOptions {
		got = append(got, "/reasoning "+option.Value)
	}
	want := []string{"/reasoning clear", "/reasoning low", "/reasoning high", "/reasoning max"}
	if len(got) != len(want) {
		t.Fatalf("expected automatic + DeepSeek catalog reasoning options, got %#v", view.Config.FormOptions)
	}
	for index, command := range want {
		if got[index] != command {
			t.Fatalf("option %d command = %q, want %q: %#v", index, got[index], command, view.Config.FormOptions)
		}
	}
}

func TestReasoningCardDoesNotUseStaleCatalogAfterRefreshFailure(t *testing.T) {
	now := time.Date(2026, 5, 4, 9, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			ErrorMessage: "model.list failed",
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "model-a",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "high"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].PromptOverride.Model = "model-a"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/reasoning",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 1 || buttons[0].CommandText != "/reasoning clear" {
		t.Fatalf("expected only automatic reasoning option for failed catalog refresh, got %#v", buttons)
	}
	if summary := commandCatalogSummaryText(catalog); !strings.Contains(summary, "模型列表刷新失败") {
		t.Fatalf("expected failed refresh notice, got %q", summary)
	}
}

func TestPromptSendDispatchDoesNotDropReasoningWhenCatalogRefreshFailed(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{
			ErrorMessage: "model.list failed",
			Entries: []agentproto.ModelCatalogEntry{{
				Model: "model-a",
				SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
					{ReasoningEffort: "low"},
				},
			}},
		},
		Threads: map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	item := &state.QueueItemRecord{
		ID:               "queue-1",
		SurfaceSessionID: "surface-1",
		FrozenOverride: state.ModelConfigRecord{
			Model:           "model-a",
			ReasoningEffort: "high",
		},
	}

	command, guardEvents := svc.promptSendCommandAndGuardEventsFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.ReasoningEffort != "high" {
		t.Fatalf("expected stale catalog not to drop reasoning override, got %#v", command.Overrides)
	}
	if len(guardEvents) != 0 {
		t.Fatalf("expected no guard event for stale catalog, got %#v", guardEvents)
	}
}
