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
