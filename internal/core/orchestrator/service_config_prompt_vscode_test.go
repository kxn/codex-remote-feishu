package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestVSCodePromptWithoutLocalOverrideDoesNotSendObservedConfigAsOverride(t *testing.T) {
	now := time.Date(2026, 5, 3, 16, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		ThreadID:        "thread-1",
		CWD:             "/data/dl/droid",
		ConfigScope:     "cwd_default",
		Model:           "gpt-5.3-codex",
		ReasoningEffort: "medium",
		AccessMode:      agentproto.AccessModeConfirm,
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:        agentproto.EventConfigObserved,
		ThreadID:    "thread-1",
		CWD:         "/data/dl/droid",
		ConfigScope: "thread",
		PlanMode:    "on",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.BaseModel != "gpt-5.3-codex" || snapshot.NextPrompt.BaseReasoningEffort != "medium" {
		t.Fatalf("expected vscode snapshot to keep observed config for display, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.ObservedThreadPlanMode != "on" {
		t.Fatalf("expected observed plan mode in snapshot, got %#v", snapshot.NextPrompt)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})
	command := promptSendCommandFromEvents(t, events)
	if command.Overrides != (agentproto.PromptOverrides{}) {
		t.Fatalf("expected vscode prompt without local override to preserve backend config, got %#v", command.Overrides)
	}
	item := svc.root.Surfaces["surface-1"].QueueItems[svc.root.Surfaces["surface-1"].ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected active queue item")
	}
	if item.FrozenOverride != (state.ModelConfigRecord{}) || item.FrozenPlanMode != "" {
		t.Fatalf("expected empty frozen overrides, got override=%#v plan=%q", item.FrozenOverride, item.FrozenPlanMode)
	}
}

func TestVSCodePromptOnlySendsLocalRequestedOverride(t *testing.T) {
	now := time.Date(2026, 5, 3, 16, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
		CWDDefaults: map[string]state.ModelConfigRecord{
			"/data/dl/droid": {AccessMode: agentproto.AccessModeConfirm},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModelCommand, SurfaceSessionID: "surface-1", Text: "/model gpt-5.4 high"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续",
	})
	command := promptSendCommandFromEvents(t, events)
	if command.Overrides.Model != "gpt-5.4" || command.Overrides.ReasoningEffort != "high" {
		t.Fatalf("expected explicit model/reasoning override, got %#v", command.Overrides)
	}
	if command.Overrides.AccessMode != "" || command.Overrides.PlanMode != "" {
		t.Fatalf("expected observed access/plan to stay out of vscode override, got %#v", command.Overrides)
	}
}

func TestVSCodePlanClearReturnsToBackendState(t *testing.T) {
	now := time.Date(2026, 5, 3, 16, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  false,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionPlanCommand, SurfaceSessionID: "surface-1", Text: "/plan on"})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("expected explicit plan override, got %#v", surface)
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionPlanCommand, SurfaceSessionID: "surface-1", Text: "/plan clear"})
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("expected plan override to be cleared, got %#v", surface)
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
	if item == nil || item.FrozenPlanMode != "" {
		t.Fatalf("expected cleared vscode plan to avoid frozen override, got %#v", item)
	}
}

func TestVSCodePlanProposalExecuteExplicitlyDisablesPlanMode(t *testing.T) {
	now := time.Date(2026, 5, 3, 16, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Source:                  "vscode",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	surface := svc.root.Surfaces["surface-1"]
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
		Delta:    "第一步",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	proposal := svc.activePlanProposal(surface)
	if proposal == nil {
		t.Fatal("expected active plan proposal before action")
	}
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanProposalDecision,
		SurfaceSessionID: "surface-1",
		ActorUserID:      "user-1",
		MessageID:        "om-proposal-1",
		PickerID:         proposal.ProposalID,
		OptionID:         "execute",
	})
	command := promptSendCommandFromEvents(t, events)
	if command.Overrides.PlanMode != string(state.PlanModeSettingOff) {
		t.Fatalf("expected proposal execute to send explicit plan off override, got %#v", command.Overrides)
	}
	if surface.PlanMode != state.PlanModeSettingOff || !surface.PlanModeOverrideSet {
		t.Fatalf("expected proposal execute to leave explicit plan off override, got %#v", surface)
	}
}

func promptSendCommandFromEvents(t *testing.T, events []eventcontract.Event) *agentproto.Command {
	t.Helper()
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			return event.Command
		}
	}
	t.Fatalf("expected prompt-send command, got %#v", events)
	return nil
}
