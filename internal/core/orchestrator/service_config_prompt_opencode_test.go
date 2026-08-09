package orchestrator

import (
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

func TestOpenCodePromptSummaryAndDispatchIgnoreCodexPromptDefaultsAndOverrides(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceKey := "/data/dl/droid"
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", state.PlanModeSettingOff)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:        "inst-1",
		DisplayName:       "droid",
		WorkspaceRoot:     workspaceKey,
		WorkspaceKey:      workspaceKey,
		ShortName:         "droid",
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: "op_team",
		Source:            "headless",
		Managed:           true,
		Online:            true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "默认会话", CWD: workspaceKey},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
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
	if snapshot.NextPrompt.EffectiveModel != "" || snapshot.NextPrompt.EffectiveReasoningEffort != "" || snapshot.NextPrompt.EffectiveAccessMode != "" {
		t.Fatalf("opencode prompt summary inherited Codex prompt config: %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.OverrideModel != "" || snapshot.NextPrompt.OverrideReasoningEffort != "" || snapshot.NextPrompt.OverrideAccessMode != "" {
		t.Fatalf("opencode prompt summary retained unsupported overrides: %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectivePlanMode != string(state.PlanModeSettingOff) || snapshot.NextPrompt.PlanModeOverrideSet {
		t.Fatalf("opencode prompt summary retained plan override: %#v", snapshot.NextPrompt)
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
	if item.FrozenOverride != (state.ModelConfigRecord{}) || item.FrozenPlanMode != "" {
		t.Fatalf("opencode queue item froze unsupported overrides: override=%#v plan=%q", item.FrozenOverride, item.FrozenPlanMode)
	}
	command := svc.promptSendCommandFromQueueItem(surface, item, "user-1", "msg-1")
	if command.Overrides != (agentproto.PromptOverrides{}) {
		t.Fatalf("opencode dispatch sent unsupported prompt overrides: %#v", command.Overrides)
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
