package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCompletePlanItemStoresMaterializedBufferedTextForProposal(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
		Delta:    "hello",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
		Delta:    " world",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", events)
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
	})
	if len(events) != 0 {
		t.Fatalf("expected completed plan item to stay buffered until turn completion, got %#v", events)
	}
	pending := svc.progress.pendingPlanProposal[turnRenderKey("inst-1", "thread-1", "turn-1")]
	if pending == nil || pending.Text != "hello world" {
		t.Fatalf("expected completed plan item to store full materialized text, got %#v", pending)
	}
}

func TestTurnCompletedPresentsPlanProposalCard(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	}
	svc.UpsertInstance(inst)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.PlanMode = state.PlanModeSettingOn
	svc.bindSurfaceToThreadMode(surface, inst, "thread-1", state.RouteModePinned)

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
		Delta:    "第一步\n第二步",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "plan",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var page *control.FeishuPageView
	for _, event := range events {
		catalog, ok := eventCommandCatalog(event)
		if !ok {
			continue
		}
		page = catalog
		break
	}
	if page == nil {
		t.Fatalf("expected plan proposal command page, got %#v", events)
	}
	if page.CommandID != control.FeishuCommandPlan || page.Title != "提案计划" || !page.Interactive {
		t.Fatalf("unexpected plan proposal page: %#v", page)
	}
	if len(page.BodySections) != 1 || page.BodySections[0].Label != "提案内容" {
		t.Fatalf("expected proposal body section, got %#v", page.BodySections)
	}
	if len(page.Sections) != 1 || len(page.Sections[0].Entries) != 1 || len(page.Sections[0].Entries[0].Buttons) != 3 {
		t.Fatalf("expected three proposal buttons, got %#v", page.Sections)
	}
	if svc.activePlanProposal(surface) == nil {
		t.Fatal("expected active plan proposal runtime after presenting card")
	}
}

func TestPlanProposalExecuteEnqueuesContinuationAndDisablesPlanMode(t *testing.T) {
	now := time.Date(2026, 4, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	}
	svc.UpsertInstance(inst)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.PlanMode = state.PlanModeSettingOn
	svc.bindSurfaceToThreadMode(surface, inst, "thread-1", state.RouteModePinned)

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

	if state.NormalizePlanModeSetting(surface.PlanMode) != state.PlanModeSettingOff {
		t.Fatalf("expected execute action to disable plan mode, got %q", surface.PlanMode)
	}
	if svc.activePlanProposal(surface) != nil {
		t.Fatal("expected execute action to clear active plan proposal runtime")
	}
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected execute action to dispatch continuation immediately, active=%q queued=%#v", surface.ActiveQueueItemID, surface.QueuedQueueItemIDs)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.FrozenThreadID != "thread-1" || item.FrozenCWD != "/data/dl/droid" {
		t.Fatalf("unexpected queued continuation item: %#v", item)
	}
	if item.Status != state.QueueItemDispatching {
		t.Fatalf("expected continuation item to be dispatching, got %#v", item)
	}
	if len(item.Inputs) != 1 || item.Inputs[0].Type != agentproto.InputText || item.Inputs[0].Text != planProposalDirectExecutePrompt() {
		t.Fatalf("unexpected queued continuation inputs: %#v", item.Inputs)
	}
	foundSeal := false
	foundDispatch := false
	for _, event := range events {
		if catalog, ok := eventCommandCatalog(event); ok && catalog.Sealed {
			foundSeal = true
		}
		if event.Kind == control.UIEventAgentCommand && event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			foundDispatch = true
		}
	}
	if !foundSeal {
		t.Fatalf("expected execute action to emit a sealed replacement card, got %#v", events)
	}
	if !foundDispatch {
		t.Fatalf("expected execute action to dispatch a prompt-send command, got %#v", events)
	}
}
