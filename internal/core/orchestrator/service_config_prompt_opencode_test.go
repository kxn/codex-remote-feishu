package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestOpenCodeHeadlessObservedConfigDoesNotPersistWorkspaceDefaults(t *testing.T) {
	now := time.Date(2026, 8, 9, 11, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	surface := svc.root.Surfaces["surface-1"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           workspaceKey,
		WorkspaceKey:            workspaceKey,
		ShortName:               "droid",
		Backend:                 agentproto.BackendOpenCode,
		OpenCodeProfileID:       "op_team",
		OpenCodeAdmissionRef:    state.NormalizeOpenCodeAdmissionRef(surface.OpenCodeAdmissionRef),
		Source:                  "headless",
		Managed:                 true,
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        "thread-1",
		CWD:             workspaceKey,
		ConfigScope:     "cwd_default",
		Model:           "codex_remote_opencode_op_team/kimi-k2",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         workspaceKey,
		ConfigScope: "thread",
		AccessMode:  agentproto.AccessModeAcceptEdits,
	})

	if defaults := svc.root.WorkspaceDefaults[state.WorkspaceDefaultsStorageKey(workspaceKey, state.InstanceBackendContract{
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
	})]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected opencode observed config not to persist workspace defaults, got %#v", defaults)
	}
	if defaults := svc.root.Instances["inst-1"].CWDDefaults[workspaceKey]; defaults != (state.ModelConfigRecord{}) {
		t.Fatalf("expected opencode observed config not to become instance cwd defaults, got %#v", defaults)
	}
	if got := svc.root.Instances["inst-1"].Threads["thread-1"].ObservedAccessMode; got != agentproto.AccessModeAcceptEdits {
		t.Fatalf("expected thread observed access mode to remain available, got %q", got)
	}
}

func TestOpenCodePromptSummaryAndDispatchCarryRuntimeReasoningAccessAndPlanButIgnoreModel(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:                "inst-1",
		DisplayName:               "droid",
		WorkspaceRoot:             workspaceKey,
		WorkspaceKey:              workspaceKey,
		ShortName:                 "droid",
		Backend:                   agentproto.BackendOpenCode,
		OpenCodeProfileID:         "op_team",
		OpenCodeRuntimeAccessMode: agentproto.AccessModeConfirm,
		Source:                    "headless",
		Managed:                   true,
		Online:                    true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/big-pickle",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
				{ReasoningEffort: "max"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/big-pickle",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{
		Model:           "gpt-5.5",
		ReasoningEffort: "high",
		AccessMode:      agentproto.AccessModeConfirm,
	}
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.EffectiveModel != "opencode/big-pickle" {
		t.Fatalf("opencode prompt summary inherited Codex prompt config: %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.OverrideModel != "" || snapshot.NextPrompt.OverrideReasoningEffort != "high" ||
		snapshot.NextPrompt.EffectiveReasoningEffort != "high" ||
		snapshot.NextPrompt.EffectiveReasoningEffortSource != "surface_override" {
		t.Fatalf("opencode prompt summary should retain runtime reasoning override but ignore model override: %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.OverrideAccessMode != agentproto.AccessModeConfirm ||
		snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm ||
		snapshot.NextPrompt.EffectiveAccessModeSource != "surface_override" {
		t.Fatalf("opencode prompt summary did not retain runtime access override: %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.OverridePlanMode != string(state.PlanModeSettingOn) ||
		snapshot.NextPrompt.EffectivePlanMode != string(state.PlanModeSettingOn) ||
		!snapshot.NextPrompt.PlanModeOverrideSet {
		t.Fatalf("opencode prompt summary did not retain plan override: %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queued item")
	}
	if item.FrozenOverride.Model != "" || item.FrozenOverride.ReasoningEffort != "high" || item.FrozenOverride.AccessMode != agentproto.AccessModeConfirm || item.FrozenPlanMode != state.PlanModeSettingOn {
		t.Fatalf("opencode queue item should freeze runtime reasoning, access and plan only: override=%#v plan=%q", item.FrozenOverride, item.FrozenPlanMode)
	}
	if queuedItemPromptDispatchPlan(item).EffectiveExecutionThreadID() != "thread-1" {
		t.Fatalf("opencode queue item execution thread = %#v, want thread-1", queuedItemPromptDispatchPlan(item))
	}
	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides != (agentproto.PromptOverrides{ReasoningEffort: "high", PlanMode: string(state.PlanModeSettingOn)}) {
		t.Fatalf("opencode dispatch should send runtime reasoning and plan ACP overrides: %#v", command.Overrides)
	}
}

func TestOpenCodeFrozenQueueItemKeepsPlanModeAfterLaterPlanCommand(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "先规划",
	})
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected first queue item")
	}
	if item.FrozenPlanMode != state.PlanModeSettingOn {
		t.Fatalf("first queue item frozen plan = %q, want on", item.FrozenPlanMode)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/plan off",
	})

	if surface.PlanMode != state.PlanModeSettingOff || !surface.PlanModeOverrideSet {
		t.Fatalf("surface plan after command = %s/%v, want off/true", surface.PlanMode, surface.PlanModeOverrideSet)
	}
	if item.FrozenPlanMode != state.PlanModeSettingOn {
		t.Fatalf("frozen queue item plan was retroactively changed to %q", item.FrozenPlanMode)
	}
	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.PlanMode != string(state.PlanModeSettingOn) {
		t.Fatalf("dispatch plan override = %#v, want frozen on", command.Overrides)
	}
}

func TestOpenCodePlanClearStopsFreezingMode(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/plan clear",
	})
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("surface plan after clear = %s/%v, want off/false", surface.PlanMode, surface.PlanModeOverrideSet)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.OverridePlanMode != "" || snapshot.NextPrompt.PlanModeOverrideSet {
		t.Fatalf("clear should remove local plan override from summary: %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "跟随当前 mode",
	})
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queued item")
	}
	if item.FrozenPlanMode != "" {
		t.Fatalf("clear should not freeze mode, got %q", item.FrozenPlanMode)
	}
	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.PlanMode != "" {
		t.Fatalf("clear should not dispatch mode override, got %#v", command.Overrides)
	}
}

func TestOpenCodeReasoningCommandStoresRuntimeEffortOverride(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/big-pickle",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
				{ReasoningEffort: "max"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/big-pickle",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning max",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PromptOverride.ReasoningEffort != "max" {
		t.Fatalf("surface reasoning override = %q, want max", surface.PromptOverride.ReasoningEffort)
	}
	if len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "已更新飞书临时推理强度覆盖") {
		t.Fatalf("expected reasoning update notice, got %#v", events)
	}
}

func TestOpenCodeReasoningCommandUsesObservedACPEffortCatalog(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 8, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadSettingsUpdated,
		ThreadID: "thread-1",
		ThreadSettings: &agentproto.ThreadSettingsUpdate{
			ThreadID: "thread-1",
			Model:    "opencode/big-pickle",
		},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventModelCatalogUpdated,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/big-pickle",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
			},
		}}},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning high",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("surface reasoning override = %q, want high", surface.PromptOverride.ReasoningEffort)
	}
	if len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "已更新飞书临时推理强度覆盖") {
		t.Fatalf("expected reasoning update notice, got %#v", events)
	}
}

func TestOpenCodeReasoningIgnoresInactiveFixedCodexProfile(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 8, 45, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{
		ID:              "fixed-codex",
		Name:            "Fixed Codex",
		Model:           "codex/fixed",
		ReasoningEffort: "max",
	})
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	surface := svc.root.Surfaces["surface-1"]
	surface.CodexProfileID = "fixed-codex"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/big-pickle",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/big-pickle",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning high",
	})
	if surface.PromptOverride.ReasoningEffort != "high" {
		t.Fatalf("opencode reasoning should use ACP catalog instead of inactive Codex profile, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || !strings.Contains(events[0].Notice.Text, "已更新飞书临时推理强度覆盖") {
		t.Fatalf("expected reasoning update notice, got %#v", events)
	}
}

func TestOpenCodeReasoningCommandRejectsRuntimeWithoutEffortSupport(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 9, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/no-effort",
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/no-effort",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning high",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PromptOverride.ReasoningEffort != "" {
		t.Fatalf("unsupported reasoning should not mutate override, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 {
		t.Fatalf("expected one error card event, got %#v", events)
	}
	catalog := commandCatalogFromEvent(t, events[0])
	if summary := commandCatalogSummaryText(catalog); !strings.Contains(summary, "当前模型没有声明可用推理强度") {
		t.Fatalf("expected unsupported effort summary, got %q", summary)
	}
}

func TestOpenCodeBareReasoningCommandDoesNotOfferManualFallbackBeforeEffortObserved(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 11, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/big-pickle",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "正在等待 OpenCode ACP 上报 effort 配置") ||
		strings.Contains(summary, "手动发送 /reasoning") {
		t.Fatalf("expected opencode no-manual-fallback summary, got %q", summary)
	}
}

func TestOpenCodeBareReasoningCommandUsesObservedEffortOptions(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 11, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model:                  "opencode/big-pickle",
			DefaultReasoningEffort: "high",
			SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{
				{ReasoningEffort: "low"},
				{ReasoningEffort: "high"},
			},
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/big-pickle",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReasoningCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/reasoning",
	})
	catalog := commandCatalogFromEvent(t, events[0])
	buttons := catalog.Sections[0].Entries[0].Buttons
	if len(buttons) != 3 {
		t.Fatalf("expected automatic + opencode ACP effort buttons, got %#v", buttons)
	}
	want := []string{"/reasoning clear", "/reasoning low", "/reasoning high"}
	for i, command := range want {
		if buttons[i].CommandText != command {
			t.Fatalf("button %d command = %q, want %q: %#v", i, buttons[i].CommandText, command, buttons)
		}
	}
	summary := commandCatalogSummaryText(catalog)
	if !strings.Contains(summary, "当前 OpenCode session 默认推理强度为 high") || strings.Contains(summary, "Codex") {
		t.Fatalf("unexpected opencode reasoning summary: %q", summary)
	}
}

func TestOpenCodeDispatchDropsFrozenReasoningWhenCurrentRuntimeDoesNotExposeEffort(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 12, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		ModelCatalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{
			Model: "opencode/no-effort",
		}}},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {
				ThreadID: "thread-1",
				Name:     "默认会话",
				CWD:      workspaceKey,
				ThreadSettings: &agentproto.ThreadSettingsUpdate{
					ThreadID: "thread-1",
					Model:    "opencode/no-effort",
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	item := &state.QueueItemRecord{
		ID:                 "queue-1",
		SurfaceSessionID:   "surface-1",
		FrozenDispatchPlan: agentproto.DefaultPromptDispatchPlanForExecutionThread("thread-1"),
		FrozenOverride:     state.ModelConfigRecord{ReasoningEffort: "high"},
	}

	command, guardEvents := svc.promptSendCommandAndGuardEventsFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides.ReasoningEffort != "" {
		t.Fatalf("unsupported opencode reasoning should be dropped before ACP prompt, got %#v", command.Overrides)
	}
	if len(guardEvents) != 1 || guardEvents[0].Notice == nil ||
		guardEvents[0].Notice.Code != "prompt_override_reasoning_dropped" ||
		!strings.Contains(guardEvents[0].Notice.Text, "已改用模型默认思考强度") {
		t.Fatalf("expected unsupported reasoning guard notice, got %#v", guardEvents)
	}
}

func TestOpenCodeAccessCommandStoresDesiredRuntimePermissionWhileDetached(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/access confirm",
	})

	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	if surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("surface runtime access = %q, want confirm", surface.PromptOverride.AccessMode)
	}
	record, ok := svc.root.BotCapabilitySettings[state.BotCapabilitySettingsKey("app-1")]
	if !ok || record.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("bot runtime access = %#v, want confirm", record)
	}
	for _, event := range events {
		if event.DaemonCommand != nil {
			t.Fatalf("detached access update should only save desired state, got daemon command %#v", event.DaemonCommand)
		}
	}
}

func TestOpenCodeAccessCommandRestartsIdleHeadlessForCurrentWorkspace(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("feishu:app-1:user:ou_user", "app-1", "ou_user", "ou_user", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:                "inst-1",
		DisplayName:               "droid",
		WorkspaceRoot:             workspaceKey,
		WorkspaceKey:              workspaceKey,
		ShortName:                 "droid",
		Backend:                   agentproto.BackendOpenCode,
		OpenCodeProfileID:         "op_team",
		OpenCodeRuntimeAccessMode: agentproto.AccessModeFullAccess,
		Source:                    "headless",
		Managed:                   true,
		Online:                    true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey, Loaded: true},
		},
	})
	surface := svc.root.Surfaces["feishu:app-1:user:ou_user"]
	surface.AttachedInstanceID = "inst-1"
	surface.ClaimedWorkspaceKey = workspaceKey
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "默认会话",
	}
	if !svc.claimKnownThread(surface, svc.root.Instances["inst-1"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/access confirm",
	})

	if surface.PromptOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("surface runtime access = %q, want confirm", surface.PromptOverride.AccessMode)
	}
	if surface.PendingHeadless == nil ||
		surface.PendingHeadless.Purpose != state.HeadlessLaunchPurposeThreadRestore ||
		surface.PendingHeadless.OpenCodeRuntimeAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected runtime access restart pending launch, got %#v", surface.PendingHeadless)
	}
	if len(events) != 4 {
		t.Fatalf("expected kill + access notice + restart notice + start, got %#v", events)
	}
	if events[0].DaemonCommand == nil ||
		events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless ||
		events[0].DaemonCommand.InstanceID != "inst-1" {
		t.Fatalf("expected first event to kill old headless, got %#v", events)
	}
	start := events[3].DaemonCommand
	if start == nil ||
		start.Kind != control.DaemonCommandStartHeadless ||
		start.ThreadID != "thread-1" ||
		start.OpenCodeRuntimeAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected restart command to carry runtime access, got %#v", start)
	}
}

func TestOpenCodePromptSummaryUsesObservedThreadSettingsAsReadOnlyBackendState(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadSettingsUpdated,
		ThreadID: "thread-1",
		ThreadSettings: &agentproto.ThreadSettingsUpdate{
			ThreadID: "thread-1",
			Model:    "opencode/big-pickle",
		},
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.BaseModel != "opencode/big-pickle" || snapshot.NextPrompt.EffectiveModel != "opencode/big-pickle" {
		t.Fatalf("opencode observed model not reflected as read-only backend state: %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})
	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queued item")
	}
	if item.FrozenOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("opencode observed model should not be resent as a prompt override: %#v", item.FrozenOverride)
	}
}

func TestOpenCodePromptSummaryShowsObservedPlanModeWithoutFoldingCustomMode(t *testing.T) {
	now := time.Date(2026, 8, 11, 13, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadSettingsUpdated,
		ThreadID: "thread-1",
		PlanMode: "on",
	})
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.NextPrompt.ObservedThreadPlanMode != "on" {
		t.Fatalf("observed plan snapshot = %#v, want on", snapshot)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadSettingsUpdated,
		ThreadID: "thread-1",
		PlanMode: "review",
	})
	snapshot = svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.NextPrompt.ObservedThreadPlanMode != "review" {
		t.Fatalf("custom observed mode snapshot = %#v, want raw review", snapshot)
	}
}
