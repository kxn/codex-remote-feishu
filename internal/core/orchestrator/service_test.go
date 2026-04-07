package orchestrator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newServiceForTest(now *time.Time) *Service {
	return NewService(func() time.Time { return *now }, Config{TurnHandoffWait: 800 * time.Millisecond}, renderer.NewPlanner())
}

func recordLocalFinalText(t *testing.T, svc *Service, instanceID, threadID, turnID, itemID, text string) []control.UIEvent {
	t.Helper()
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
		Delta:    text,
	}); len(events) != 0 {
		t.Fatalf("expected no UI events while collecting local text, got %#v", events)
	}
	if events := svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: threadID,
		TurnID:   turnID,
		ItemID:   itemID,
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected no UI events before local turn completion, got %#v", events)
	}
	return svc.ApplyAgentEvent(instanceID, agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  threadID,
		TurnID:    turnID,
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
}

func TestAttachPinsObservedFocusedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	if len(events) < 2 {
		t.Fatalf("expected snapshot and notice, got %d events", len(events))
	}
	surface := svc.root.Surfaces["feishu:chat:1"]
	if surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected selected thread to be pinned, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestAttachFallsBackToActiveThreadWhenFocusedThreadUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:     "inst-1",
		DisplayName:    "droid",
		WorkspaceRoot:  "/data/dl/droid",
		WorkspaceKey:   "/data/dl/droid",
		ShortName:      "droid",
		Online:         true,
		ActiveThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["feishu:chat:1"]
	if surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected selected thread to fall back to active thread, got %q", surface.SelectedThreadID)
	}
	if surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected route mode pinned, got %q", surface.RouteMode)
	}
}

func TestAttachBusyInstanceRejectsSecondSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-2"]
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected second surface to remain detached, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "instance_busy" {
		t.Fatalf("expected instance_busy notice, got %#v", events)
	}
}

func TestAttachWithoutDefaultThreadEntersUnboundAndPromptsUse(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface to enter attached_unbound, got attached=%q selected=%q route=%q", surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode)
	}
	var sawNotice, sawPrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" && strings.Contains(event.Notice.Text, "/use") {
			sawNotice = true
		}
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptUseThread {
			sawPrompt = true
		}
	}
	if !sawNotice || !sawPrompt {
		t.Fatalf("expected attach notice plus /use prompt, got %#v", events)
	}
}

func TestListInstancesMarksBusyClaimedInstanceDisabled(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修样式", CWD: "/data/dl/web"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected one selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Kind != control.SelectionPromptAttachInstance || len(prompt.Options) != 2 {
		t.Fatalf("unexpected instance prompt: %#v", prompt)
	}
	for _, option := range prompt.Options {
		switch option.OptionID {
		case "inst-1":
			if !option.Disabled || option.ButtonLabel != "已占用" || !strings.Contains(option.Subtitle, "已被其他飞书会话接管") {
				t.Fatalf("expected busy instance to be disabled, got %#v", option)
			}
		case "inst-2":
			if option.Disabled {
				t.Fatalf("expected free instance to remain selectable, got %#v", option)
			}
		default:
			t.Fatalf("unexpected instance option: %#v", option)
		}
	}
}

func TestUseBusyIdleThreadShowsKickPromptAndConfirmTransfersClaim(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
	svc.instanceClaims["inst-1"] = &instanceClaimRecord{InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	promptEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(promptEvents) != 1 || promptEvents[0].SelectionPrompt == nil || promptEvents[0].SelectionPrompt.Kind != control.SelectionPromptKickThread {
		t.Fatalf("expected kick confirmation prompt, got %#v", promptEvents)
	}

	confirm := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionConfirmKickThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})

	first := svc.root.Surfaces["surface-1"]
	second := svc.root.Surfaces["surface-2"]
	if first.SelectedThreadID != "" || first.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected victim surface to become unbound, got selected=%q route=%q", first.SelectedThreadID, first.RouteMode)
	}
	if second.SelectedThreadID != "thread-1" || second.RouteMode != state.RouteModePinned {
		t.Fatalf("expected requesting surface to claim thread-1, got selected=%q route=%q", second.SelectedThreadID, second.RouteMode)
	}
	var sawVictimNotice, sawWinnerNotice bool
	for _, event := range confirm {
		if event.SurfaceSessionID == "surface-1" && event.Notice != nil && event.Notice.Code == "thread_claim_lost" {
			sawVictimNotice = true
		}
		if event.SurfaceSessionID == "surface-2" && event.Notice != nil && event.Notice.Code == "thread_kicked" {
			sawWinnerNotice = true
		}
	}
	if !sawVictimNotice || !sawWinnerNotice {
		t.Fatalf("expected kick notices for both surfaces, got %#v", confirm)
	}
}

func TestUseBusyRunningThreadRejectsKick(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-2"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-2",
		AttachedInstanceID: "inst-1",
		RouteMode:          state.RouteModeUnbound,
		QueueItems:         map[string]*state.QueueItemRecord{},
		StagedImages:       map[string]*state.StagedImageRecord{},
		PendingRequests:    map[string]*state.RequestPromptRecord{},
	}
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-2",
		ThreadID:         "thread-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "thread_busy_running" {
		t.Fatalf("expected busy running thread to reject kick, got %#v", events)
	}
}

func TestListWithoutOnlineInstancesReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 {
		t.Fatalf("expected one notice event, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "no_online_instances" {
		t.Fatalf("expected no_online_instances notice, got %#v", events[0])
	}
}

func TestTextMessageFreezesThreadAndConsumesStagedImages(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-text",
		Text:             "请分析这张图",
	})

	if len(events) < 3 {
		t.Fatalf("expected queued + dispatch + command events, got %d", len(events))
	}
	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueueItems) != 1 {
		t.Fatalf("expected one queue item, got %d", len(surface.QueueItems))
	}
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item.FrozenThreadID != "thread-1" || item.FrozenCWD != "/data/dl/droid" {
		t.Fatalf("unexpected frozen route: %#v", item)
	}
	if len(item.Inputs) != 2 || item.Inputs[0].Type != agentproto.InputLocalImage || item.Inputs[1].Type != agentproto.InputText {
		t.Fatalf("unexpected inputs: %#v", item.Inputs)
	}
}

func TestTextMessageUsesProvidedInputsAlongsideStagedImages(t *testing.T) {
	now := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-staged", LocalPath: "/tmp/staged.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-post",
		Text:             "这是图文混合消息",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/inline.png", MIMEType: "image/png"},
			{Type: agentproto.InputText, Text: "这是图文混合消息"},
		},
	})

	if len(events) < 3 {
		t.Fatalf("expected queued + dispatch + command events, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected one queue item")
	}
	if len(item.Inputs) != 3 {
		t.Fatalf("expected staged image + inline image + text, got %#v", item.Inputs)
	}
	if item.Inputs[0].Type != agentproto.InputLocalImage || item.Inputs[0].Path != "/tmp/staged.png" {
		t.Fatalf("unexpected first input: %#v", item.Inputs[0])
	}
	if item.Inputs[1].Type != agentproto.InputLocalImage || item.Inputs[1].Path != "/tmp/inline.png" {
		t.Fatalf("unexpected second input: %#v", item.Inputs[1])
	}
	if item.Inputs[2].Type != agentproto.InputText || item.Inputs[2].Text != "这是图文混合消息" {
		t.Fatalf("unexpected third input: %#v", item.Inputs[2])
	}
}

func TestStatusReflectsObservedDefaultConfigAndSurfaceOverride(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:            agentproto.EventConfigObserved,
		CWD:             "/data/dl/droid",
		ConfigScope:     "cwd_default",
		Model:           "gpt-5.3-codex",
		ReasoningEffort: "medium",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.4 high",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.CreateThread || snapshot.NextPrompt.CWD != "/data/dl/droid" {
		t.Fatalf("expected unbound surface to stay blocked in workspace root, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.BaseModel != "gpt-5.3-codex" || snapshot.NextPrompt.BaseReasoningEffort != "medium" {
		t.Fatalf("expected base config from cwd default, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveModel != "gpt-5.4" || snapshot.NextPrompt.EffectiveReasoningEffort != "high" {
		t.Fatalf("expected effective config to use surface override, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveModelSource != "surface_override" || snapshot.NextPrompt.EffectiveReasoningEffortSource != "surface_override" {
		t.Fatalf("expected override sources in snapshot, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeFullAccess || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_default" {
		t.Fatalf("expected default full access in snapshot, got %#v", snapshot.NextPrompt)
	}
}

func TestStatusUsesSurfaceDefaultsWhenObservedConfigUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.EffectiveModel != "gpt-5.4" || snapshot.NextPrompt.EffectiveModelSource != "surface_default" {
		t.Fatalf("expected default model fallback, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveReasoningEffort != "xhigh" || snapshot.NextPrompt.EffectiveReasoningEffortSource != "surface_default" {
		t.Fatalf("expected xhigh reasoning fallback, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeFullAccess || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_default" {
		t.Fatalf("expected default full access, got %#v", snapshot.NextPrompt)
	}
}

func TestTextMessageFreezesObservedPromptConfigWhenNoSurfaceOverride(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		CWDDefaults: map[string]state.ModelConfigRecord{
			"/data/dl/droid": {Model: "gpt-5.4", ReasoningEffort: "high"},
		},
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.Model != "gpt-5.4" || item.FrozenOverride.ReasoningEffort != "high" {
		t.Fatalf("expected queued item to freeze observed config, got %#v", item.FrozenOverride)
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected queued item to freeze full access, got %#v", item.FrozenOverride)
	}
}

func TestTextMessageFreezesFallbackReasoningWhenConfigUnknown(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.Model != "gpt-5.4" {
		t.Fatalf("expected queued item to freeze default model, got %#v", item.FrozenOverride)
	}
	if item.FrozenOverride.ReasoningEffort != "xhigh" || item.FrozenOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("expected queued item to freeze fallback config, got %#v", item.FrozenOverride)
	}
}

func TestAccessCommandUpdatesSnapshotAndQueueFreeze(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAccessCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/access confirm",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected surface snapshot")
	}
	if snapshot.NextPrompt.OverrideAccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected access override in snapshot, got %#v", snapshot.NextPrompt)
	}
	if snapshot.NextPrompt.EffectiveAccessMode != agentproto.AccessModeConfirm || snapshot.NextPrompt.EffectiveAccessModeSource != "surface_override" {
		t.Fatalf("expected confirm access in snapshot, got %#v", snapshot.NextPrompt)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	surface := svc.root.Surfaces["surface-1"]
	var item *state.QueueItemRecord
	for _, current := range surface.QueueItems {
		item = current
	}
	if item == nil {
		t.Fatal("expected queue item")
	}
	if item.FrozenOverride.AccessMode != agentproto.AccessModeConfirm {
		t.Fatalf("expected queued item to freeze access override, got %#v", item.FrozenOverride)
	}
}

func TestQueuedMessageFreezesSurfaceOverrideAtEnqueue(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {
				ThreadID:                "thread-1",
				Name:                    "修复登录流程",
				CWD:                     "/data/dl/droid",
				ExplicitModel:           "gpt-5.3-codex",
				ExplicitReasoningEffort: "medium",
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.4 high",
	})

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued-only event while paused, got %#v", queued)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModelCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/model gpt-5.2-codex low",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	now = now.Add(900 * time.Millisecond)
	resumed := svc.Tick(now)
	var prompt *agentproto.Command
	for _, event := range resumed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			prompt = event.Command
			break
		}
	}
	if prompt == nil {
		t.Fatalf("expected resumed queue item to dispatch prompt command, got %#v", resumed)
	}
	if prompt.Overrides.Model != "gpt-5.4" || prompt.Overrides.ReasoningEffort != "high" {
		t.Fatalf("expected queued message to keep original frozen override, got %#v", prompt.Overrides)
	}
}

func TestLocalInteractionPausesRemoteQueueAndHandoffResumes(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	localEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(localEvents) != 1 || localEvents[0].Notice == nil || localEvents[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("unexpected local pause events: %#v", localEvents)
	}

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].PendingInput == nil || queued[0].PendingInput.Status != "queued" {
		t.Fatalf("expected queued-only event while paused, got %#v", queued)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeHandoffWait {
		t.Fatalf("expected handoff wait, got %q", surface.DispatchMode)
	}

	now = now.Add(900 * time.Millisecond)
	tick := svc.Tick(now)
	if len(tick) < 3 {
		t.Fatalf("expected resume notice + dispatch events, got %#v", tick)
	}
	if tick[0].Notice == nil || tick[0].Notice.Code != "remote_queue_resumed" {
		t.Fatalf("expected resume notice, got %#v", tick[0])
	}
}

func TestLocalPauseNoticeIsNotRepeatedWhenTurnStartedArrives(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(first) != 1 || first[0].Notice == nil || first[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected first local pause notice, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "running",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(second) != 0 {
		t.Fatalf("expected no duplicate local pause notice, got %#v", second)
	}
}

func TestLocalPauseWatchdogResumesQueuedWorkWithoutLocalCompletion(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	now = now.Add(16 * time.Second)
	events := svc.Tick(now)

	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected watchdog to restore normal dispatch mode, got %q", surface.DispatchMode)
	}
	var sawResumeNotice, sawDispatch bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "local_activity_watchdog_resumed" {
			sawResumeNotice = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawDispatch = true
		}
	}
	if !sawResumeNotice || !sawDispatch {
		t.Fatalf("expected watchdog resume notice + dispatch, got %#v", events)
	}
}

func TestInternalHelperLocalInteractionDoesNotPauseSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventLocalInteractionObserved,
		ThreadID:     "thread-helper",
		CWD:          "/data/dl/droid",
		Action:       "turn_start",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(events) != 0 {
		t.Fatalf("expected helper interaction to stay out of product UI, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected helper interaction not to pause surface, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}
	if svc.root.Instances["inst-1"].ObservedFocusedThreadID != "" {
		t.Fatalf("expected helper interaction not to mutate observed focus, got %q", svc.root.Instances["inst-1"].ObservedFocusedThreadID)
	}
}

func TestInternalHelperThreadIsNotAddedToVisibleThreadState(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventThreadDiscovered,
		ThreadID:     "thread-helper",
		CWD:          "/data/dl/droid",
		Name:         "helper",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(events) != 0 {
		t.Fatalf("expected helper thread discovery not to emit UI events, got %#v", events)
	}
	if _, exists := svc.root.Instances["inst-1"].Threads["thread-helper"]; exists {
		t.Fatalf("expected helper thread not to enter visible thread state, got %#v", svc.root.Instances["inst-1"].Threads["thread-helper"])
	}
}

func TestInternalHelperTurnLifecycleDoesNotAffectRemoteQueue(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	foundDispatch := false
	for _, event := range queued {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			foundDispatch = true
			break
		}
	}
	if !foundDispatch {
		t.Fatalf("expected remote queue item to dispatch immediately, got %#v", queued)
	}
	surface := svc.root.Surfaces["surface-1"]
	activeQueueItemID := surface.ActiveQueueItemID

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnStarted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(started) != 0 {
		t.Fatalf("expected helper turn start to stay out of UI, got %#v", started)
	}
	item := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventItemCompleted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		ItemID:       "item-helper",
		ItemKind:     "agent_message",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
		Metadata:     map[string]any{"text": "{\"title\":\"helper\"}"},
	})
	if len(item) != 0 {
		t.Fatalf("expected helper item completion not to render, got %#v", item)
	}
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-helper",
		TurnID:       "turn-helper",
		Status:       "completed",
		TrafficClass: agentproto.TrafficClassInternalHelper,
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorInternalHelper},
	})
	if len(completed) != 0 {
		t.Fatalf("expected helper turn completion to stay out of UI, got %#v", completed)
	}
	if surface.ActiveQueueItemID != activeQueueItemID {
		t.Fatalf("expected helper lifecycle not to disturb remote active queue item, before=%q after=%q", activeQueueItemID, surface.ActiveQueueItemID)
	}
	if svc.root.Instances["inst-1"].ActiveTurnID != "" {
		t.Fatalf("expected helper lifecycle not to mutate instance active turn, got %q", svc.root.Instances["inst-1"].ActiveTurnID)
	}
}

func TestStopInterruptsActiveTurnAndDiscardsQueuedMessages(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveThreadID:          "thread-1",
		ActiveTurnID:            "turn-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].QueuedQueueItemIDs = []string{"queue-1"}
	svc.root.Surfaces["surface-1"].QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:              "queue-1",
		SourceMessageID: "msg-1",
		Status:          state.QueueItemQueued,
	}
	svc.root.Surfaces["surface-1"].StagedImages["img-1"] = &state.StagedImageRecord{
		ImageID:         "img-1",
		SourceMessageID: "msg-img",
		State:           state.ImageStaged,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 4 {
		t.Fatalf("expected interrupt + 2 discard events + notice, got %#v", events)
	}
	if events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnInterrupt {
		t.Fatalf("expected interrupt command, got %#v", events[0])
	}
	if events[3].Notice == nil || events[3].Notice.Code != "stop_requested" {
		t.Fatalf("expected stop_requested notice, got %#v", events[3])
	}
}

func TestStopWithoutActiveTurnReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "stop_no_active_turn" {
		t.Fatalf("expected stop_no_active_turn notice, got %#v", events)
	}
}

func TestMessageRecallCancelsQueuedItemAcrossTextAndImageSources(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	surface := svc.root.Surfaces["surface-1"]
	surface.QueuedQueueItemIDs = []string{"queue-1"}
	surface.QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:               "queue-1",
		SourceMessageID:  "msg-text",
		SourceMessageIDs: []string{"msg-text", "msg-img"},
		Status:           state.QueueItemQueued,
	}
	surface.StagedImages["img-1"] = &state.StagedImageRecord{
		ImageID:         "img-1",
		SourceMessageID: "msg-img",
		State:           state.ImageBound,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 2 {
		t.Fatalf("expected discard reactions for text and image sources, got %#v", events)
	}
	for _, event := range events {
		if event.PendingInput == nil || !event.PendingInput.QueueOff || !event.PendingInput.ThumbsDown || event.PendingInput.Status != string(state.QueueItemDiscarded) {
			t.Fatalf("unexpected queued item discard projection: %#v", events)
		}
	}
	if surface.QueueItems["queue-1"].Status != state.QueueItemDiscarded || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected queue item to be removed from queue, got item=%#v queue=%#v", surface.QueueItems["queue-1"], surface.QueuedQueueItemIDs)
	}
	if surface.StagedImages["img-1"].State != state.ImageDiscarded {
		t.Fatalf("expected bound image to be marked discarded, got %#v", surface.StagedImages["img-1"])
	}
}

func TestMessageRecallCancelsStagedImage(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages: map[string]*state.StagedImageRecord{
			"img-1": {
				ImageID:         "img-1",
				SourceMessageID: "msg-img",
				State:           state.ImageStaged,
			},
		},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 1 || events[0].PendingInput == nil {
		t.Fatalf("expected staged image cancellation event, got %#v", events)
	}
	if events[0].PendingInput.Status != string(state.ImageCancelled) || !events[0].PendingInput.QueueOff || !events[0].PendingInput.ThumbsDown {
		t.Fatalf("unexpected staged image cancellation projection: %#v", events[0].PendingInput)
	}
}

func TestUseThreadDiscardsStagedImagesOnRouteChange(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 45, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if _, ok := svc.root.Surfaces["surface-1"].StagedImages["img-1"]; ok {
		t.Fatalf("expected staged image to be dropped on /use route change")
	}
	var sawDiscard, sawNotice, sawSelection bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.Status == string(state.ImageDiscarded) && event.PendingInput.QueueOff && event.PendingInput.ThumbsDown {
			sawDiscard = true
		}
		if event.Notice != nil && event.Notice.Code == "staged_images_discarded_on_route_change" {
			sawNotice = true
		}
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-2" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawSelection = true
		}
	}
	if !sawDiscard || !sawNotice || !sawSelection {
		t.Fatalf("expected discard notice + new selection, got %#v", events)
	}
}

func TestFollowLocalDiscardsStagedImagesOnRouteModeChange(t *testing.T) {
	now := time.Date(2026, 4, 6, 9, 50, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFollowLocal,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeFollowLocal || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected follow_local to keep thread and switch mode, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if _, ok := surface.StagedImages["img-1"]; ok {
		t.Fatalf("expected staged image to be dropped on /follow route change")
	}
	var sawDiscard, sawNotice, sawSelection bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.Status == string(state.ImageDiscarded) && event.PendingInput.QueueOff && event.PendingInput.ThumbsDown {
			sawDiscard = true
		}
		if event.Notice != nil && event.Notice.Code == "staged_images_discarded_on_route_change" {
			sawNotice = true
		}
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" && event.ThreadSelection.RouteMode == string(state.RouteModeFollowLocal) {
			sawSelection = true
		}
	}
	if !sawDiscard || !sawNotice || !sawSelection {
		t.Fatalf("expected discard notice + follow_local selection, got %#v", events)
	}
}

func TestMessageRecallRunningItemReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:  "surface-1",
		ActiveQueueItemID: "queue-1",
		QueueItems: map[string]*state.QueueItemRecord{
			"queue-1": {
				ID:               "queue-1",
				SourceMessageID:  "msg-text",
				SourceMessageIDs: []string{"msg-text", "msg-img"},
				Status:           state.QueueItemRunning,
			},
		},
		StagedImages:    map[string]*state.StagedImageRecord{},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-img",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "message_recall_too_late" {
		t.Fatalf("expected message_recall_too_late notice, got %#v", events)
	}
}

func TestMessageRecallCompletedItemIsIgnored(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		QueueItems: map[string]*state.QueueItemRecord{
			"queue-1": {
				ID:               "queue-1",
				SourceMessageID:  "msg-text",
				SourceMessageIDs: []string{"msg-text", "msg-img"},
				Status:           state.QueueItemCompleted,
			},
		},
		StagedImages:    map[string]*state.StagedImageRecord{},
		PendingRequests: map[string]*state.RequestPromptRecord{},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMessageRecalled,
		SurfaceSessionID: "surface-1",
		TargetMessageID:  "msg-text",
	})
	if len(events) != 0 {
		t.Fatalf("expected completed item recall to be ignored, got %#v", events)
	}
}

func TestAssistantTextIsBufferedFromDeltaUntilItemCompleted(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(started) != 0 {
		t.Fatalf("expected no UI events on item start, got %#v", started)
	}

	delta := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "您好",
	})
	if len(delta) != 0 {
		t.Fatalf("expected no UI events on item delta, got %#v", delta)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(completed) != 0 {
		t.Fatalf("expected no UI events until turn completion, got %#v", completed)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	if len(finished) != 1 || finished[0].Block == nil {
		t.Fatalf("expected final block without extra thread switch, got %#v", finished)
	}
	if finished[0].Block == nil || finished[0].Block.Text != "您好" || !finished[0].Block.Final {
		t.Fatalf("expected final rendered assistant block on turn completion, got %#v", finished)
	}
}

func TestAssistantProcessTextFlushesWhenTurnContinuesWithAnotherItem(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Delta:    "sort 在只读沙箱里没法创建临时文件。我改用不需要写临时文件的目录列表方式。",
	})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	})
	if len(completed) != 0 {
		t.Fatalf("expected pending process text after first agent message, got %#v", completed)
	}

	flushed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-2",
		ItemKind: "command_execution",
	})
	if len(flushed) != 1 || flushed[0].Block == nil {
		t.Fatalf("expected flushed process text without selection change, got %#v", flushed)
	}
	if flushed[0].Block == nil || flushed[0].Block.Final || flushed[0].Block.Text != "sort 在只读沙箱里没法创建临时文件。我改用不需要写临时文件的目录列表方式。" {
		t.Fatalf("expected process text to flush when turn continues, got %#v", flushed)
	}
}

func TestThreadSelectionNoticeIsNotRepeatedWhenOnlyPreviewChanges(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "旧预览", CWD: "/data/dl/droid"},
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
	events := svc.threadSelectionEvents(surface, "thread-1", string(state.RouteModePinned), "droid · 修复登录流程", "新预览")
	if len(events) != 0 {
		t.Fatalf("expected no repeated selection notice for same thread, got %#v", events)
	}
}

func TestThreadFocusDoesNotNarrowWorkspaceRootToChildDirectory(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid/subdir",
	})

	inst := svc.root.Instances["inst-1"]
	if inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected workspace root to remain original root, got %q", inst.WorkspaceRoot)
	}
}

func TestLocalInteractionUpdatesObservedFocusButDoesNotSwitchSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-2" {
		t.Fatalf("expected observed focused thread to be updated, got %q", inst.ObservedFocusedThreadID)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected surface selection to stay unchanged before actual turn starts, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestLocalTurnStartedDoesNotBindUnboundAttachedSurfaceToDetectedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-3" {
		t.Fatalf("expected observed focused thread to be updated, got %q", inst.ObservedFocusedThreadID)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected unbound surface to remain unchanged, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestLocalInteractionDoesNotSwitchPinnedSurfaceBeforeTurnStarts(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
		Action:   "turn_start",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to stay on prior thread until actual turn starts, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestFollowLocalSurfaceBindsOnLocalTurnStart(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-3": {ThreadID: "thread-3", Name: "补充文档", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-3" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected follow-local surface to bind to local-ui turn thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 2 || events[1].ThreadSelection == nil || events[1].ThreadSelection.ThreadID != "thread-3" {
		t.Fatalf("expected local pause notice plus follow selection update, got %#v", events)
	}
}

func TestLocalTurnStartedDoesNotSwitchPinnedSurfaceToDetectedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-3": {ThreadID: "thread-3", Name: "补充文档", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-3",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to remain on prior thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", events)
	}
}

func TestRenderTextItemDoesNotSwitchSurfaceToUnclaimedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.renderTextItem("inst-1", "thread-2", "turn-1", "item-1", "好的，我来整理日志。", false)

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected pinned surface to remain on claimed thread, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if len(events) != 0 {
		t.Fatalf("expected no projection for unclaimed local thread output, got %#v", events)
	}
}

func TestAttachReplaysUndeliveredFinalForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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

	finished := recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "这是离线期间的 final")
	if len(finished) != 0 {
		t.Fatalf("expected no UI events without attached surface, got %#v", finished)
	}
	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.UndeliveredReplay == nil || thread.UndeliveredReplay.Kind != state.ThreadReplayAssistantFinal {
		t.Fatalf("expected undelivered final replay to be stored, got %#v", thread.UndeliveredReplay)
	}

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	var sawBlock bool
	for _, event := range attach {
		if event.Block != nil && event.Block.Text == "这是离线期间的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected attach to replay stored final block, got %#v", attach)
	}
	if thread.UndeliveredReplay != nil {
		t.Fatalf("expected replay to clear after attach, got %#v", thread.UndeliveredReplay)
	}

	svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: "surface-1"})
	again := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range again {
		if event.Block != nil {
			t.Fatalf("expected replay to be one-shot, got %#v", again)
		}
	}
}

func TestUseThreadReplaysUndeliveredFinalForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "等待 /use 的 final")
	svc.root.Instances["inst-1"].ActiveThreadID = ""
	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil {
			t.Fatalf("expected unbound attach not to replay immediately, got %#v", attach)
		}
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected attach without default thread to remain unbound, got selected=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}

	use := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	var sawBlock bool
	for _, event := range use {
		if event.Block != nil && event.Block.Text == "等待 /use 的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected /use to replay stored final block, got %#v", use)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected replay to clear after /use, got %#v", replay)
	}
}

func TestBusyAttachDefersReplayUntilThreadIsIdle(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveTurnID:            "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-0", "item-0", "旧 final")
	svc.root.Instances["inst-1"].ActiveThreadID = "thread-1"
	svc.root.Instances["inst-1"].ActiveTurnID = "turn-running"
	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil {
			t.Fatalf("expected busy attach not to replay immediately, got %#v", attach)
		}
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil {
		t.Fatalf("expected replay to remain queued while thread is busy")
	}

	svc.root.Instances["inst-1"].ActiveTurnID = ""
	use := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})
	var sawBlock bool
	for _, event := range use {
		if event.Block != nil && event.Block.Text == "旧 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected replay after thread becomes idle, got %#v", use)
	}
}

func TestAttachReplaysUndeliveredProblemNoticeForIdleThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 15, 0, 0, time.UTC)
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:      "stdout_parse_failed",
		Layer:     "wrapper",
		Stage:     "observe_codex_stdout",
		Operation: "codex.stdout",
		ThreadID:  "thread-1",
		Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
		Details:   "invalid character 'x' looking for beginning of value",
	}))
	if len(events) != 0 {
		t.Fatalf("expected no UI events without attached surface, got %#v", events)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil || replay.Kind != state.ThreadReplayNotice {
		t.Fatalf("expected undelivered problem notice to be stored, got %#v", replay)
	}

	attach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	var sawNotice bool
	for _, event := range attach {
		if event.Notice != nil && event.Notice.Code == debugErrorNoticeCode && strings.Contains(event.Notice.Text, "invalid character") {
			sawNotice = true
		}
	}
	if !sawNotice {
		t.Fatalf("expected attach to replay stored problem notice, got %#v", attach)
	}
}

func TestDeliveredLocalFinalDoesNotCreateUndeliveredReplay(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 20, 0, 0, time.UTC)
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

	finished := recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "已实时投递的 final")
	var sawBlock bool
	for _, event := range finished {
		if event.Block != nil && event.Block.Text == "已实时投递的 final" && event.Block.Final {
			sawBlock = true
		}
	}
	if !sawBlock {
		t.Fatalf("expected local final to render on attached surface, got %#v", finished)
	}
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay != nil {
		t.Fatalf("expected delivered final not to be stored as replay, got %#v", replay)
	}
}

func TestUndeliveredReplayDoesNotSurviveServiceRebuild(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 25, 0, 0, time.UTC)
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
	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "重建前的 final")
	if replay := svc.root.Instances["inst-1"].Threads["thread-1"].UndeliveredReplay; replay == nil {
		t.Fatal("expected replay to exist before rebuild")
	}

	rebuilt := newServiceForTest(&now)
	rebuilt.UpsertInstance(&state.InstanceRecord{
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
	attach := rebuilt.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	for _, event := range attach {
		if event.Block != nil || (event.Notice != nil && event.Notice.Code == debugErrorNoticeCode) {
			t.Fatalf("expected rebuilt service not to recover old replay, got %#v", attach)
		}
	}
}

func TestDispatchingRemoteTurnOverridesStaleLocalClassification(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(started) == 0 || started[0].PendingInput == nil || started[0].PendingInput.Status != string(state.QueueItemRunning) {
		t.Fatalf("expected active queue item to be promoted to running despite stale local initiator, got %#v", started)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected no local pause for queued remote turn, got %q", surface.DispatchMode)
	}

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(completed) == 0 || completed[0].PendingInput == nil || !completed[0].PendingInput.TypingOff {
		t.Fatalf("expected queued remote turn to complete normally, got %#v", completed)
	}
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected surface to remain in normal mode, got %q", surface.DispatchMode)
	}
}

func TestApprovalRequestPromptUsesAttachedSurfaceForLocalTurn(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType":  "approval",
			"title":        "需要确认",
			"body":         "本地 Codex 想执行 `git push`。",
			"acceptLabel":  "允许执行",
			"declineLabel": "拒绝",
		},
	})
	if len(events) != 1 || events[0].RequestPrompt == nil {
		t.Fatalf("expected one request prompt event, got %#v", events)
	}
	prompt := events[0].RequestPrompt
	if prompt.RequestID != "req-1" || prompt.ThreadTitle != "droid · 修复登录流程" {
		t.Fatalf("unexpected request prompt: %#v", prompt)
	}
	if len(prompt.Options) != 3 || prompt.Options[0].OptionID != "accept" || prompt.Options[1].OptionID != "decline" || prompt.Options[2].OptionID != "captureFeedback" {
		t.Fatalf("unexpected request options: %#v", prompt.Options)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.RequestType != "approval" {
		t.Fatalf("expected pending request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
	if len(record.Options) != 3 {
		t.Fatalf("expected request options in state, got %#v", record)
	}
}

func TestRespondRequestDispatchesCommandAndClearsOnResolve(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		RequestID:        "req-1",
		RequestOptionID:  "accept",
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one agent command event, got %#v", events)
	}
	command := events[0].Command
	if command.Kind != agentproto.CommandRequestRespond || command.Request.RequestID != "req-1" {
		t.Fatalf("unexpected request respond command: %#v", command)
	}
	if command.Request.Response["decision"] != "accept" || command.Request.Response["type"] != "approval" {
		t.Fatalf("unexpected request respond payload: %#v", command.Request.Response)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected request state to be cleared, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}

	expired := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		RequestID:        "req-1",
		RequestOptionID:  "decline",
	})
	if len(expired) != 1 || expired[0].Notice == nil || expired[0].Notice.Code != "request_expired" {
		t.Fatalf("expected expired notice after resolve, got %#v", expired)
	}
}

func TestRespondRequestAcceptForSessionDispatchesDecision(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
			"options": []map[string]any{
				{"id": "accept", "label": "允许一次"},
				{"id": "acceptForSession", "label": "本会话允许"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		RequestID:        "req-1",
		RequestOptionID:  "acceptForSession",
	})
	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one agent command event, got %#v", events)
	}
	if events[0].Command.Request.Response["decision"] != "acceptForSession" {
		t.Fatalf("unexpected request decision payload: %#v", events[0].Command.Request.Response)
	}
}

func TestPendingRequestBlocksTextUntilHandled(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-1",
		Text:             "你先看看当前有哪些文件没有提交",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected request_pending notice, got %#v", events)
	}
	if len(svc.root.Surfaces["surface-1"].QueueItems) != 0 {
		t.Fatalf("expected no queued text while request is pending, got %#v", svc.root.Surfaces["surface-1"].QueueItems)
	}
}

func TestCaptureFeedbackQueuesFollowupAndDeclinesRequest(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveThreadID:          "thread-1",
		ActiveTurnID:            "turn-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata:  map[string]any{"requestType": "approval", "title": "需要确认"},
	})

	startCapture := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		RequestID:        "req-1",
		RequestOptionID:  "captureFeedback",
	})
	if len(startCapture) != 1 || startCapture[0].Notice == nil || startCapture[0].Notice.Code != "request_capture_started" {
		t.Fatalf("expected request_capture_started notice, got %#v", startCapture)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture == nil {
		t.Fatalf("expected active request capture to be created")
	}

	feedback := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-2",
		Text:             "不要直接执行，先列一下当前未提交文件，再继续。",
	})
	var command *agentproto.Command
	var pending *control.PendingInputState
	var gotNotice bool
	for _, event := range feedback {
		if event.Command != nil {
			command = event.Command
		}
		if event.PendingInput != nil {
			pending = event.PendingInput
		}
		if event.Notice != nil && event.Notice.Code == "request_feedback_queued" {
			gotNotice = true
		}
	}
	if command == nil || command.Request.Response["decision"] != "decline" {
		t.Fatalf("expected decline command from captured feedback, got %#v", feedback)
	}
	if pending == nil || pending.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected queued follow-up input, got %#v", feedback)
	}
	if !gotNotice {
		t.Fatalf("expected feedback queued notice, got %#v", feedback)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveRequestCapture != nil {
		t.Fatalf("expected capture to be cleared, got %#v", surface.ActiveRequestCapture)
	}
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected one queued follow-up item, got %#v", surface.QueuedQueueItemIDs)
	}
	item := surface.QueueItems[surface.QueuedQueueItemIDs[0]]
	if item == nil || item.FrozenThreadID != "thread-1" {
		t.Fatalf("expected queued follow-up to stay on original thread, got %#v", item)
	}
	if item.SourceMessageID != "om-msg-2" || len(item.Inputs) != 1 || item.Inputs[0].Text == "" {
		t.Fatalf("unexpected follow-up queue item: %#v", item)
	}
}

func TestTickExpiresRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		PendingRequests:  map[string]*state.RequestPromptRecord{},
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		ActiveRequestCapture: &state.RequestCaptureRecord{
			RequestID: "req-1",
			Mode:      requestCaptureModeDeclineWithFeedback,
			CreatedAt: now.Add(-11 * time.Minute),
			ExpiresAt: now.Add(-1 * time.Second),
		},
	}

	events := svc.Tick(now)
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_expired" {
		t.Fatalf("expected request capture expiry notice, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture != nil {
		t.Fatalf("expected expired capture to be cleared")
	}
}

func TestThreadTitleUsesPreviewSummaryWhenNameMissing(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:   "dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		WorkspaceRoot: "/data/dl",
	}, &state.ThreadRecord{
		ThreadID: "019d5679-370c-7b03-b86f-15a33a017c83",
		Preview:  "当前目录 `/data/dl` 下的内容如下：",
		CWD:      "/data/dl",
	}, "019d5679-370c-7b03-b86f-15a33a017c83")

	if title != "dl · 当前目录 `/data/dl` 下的内容如下：" {
		t.Fatalf("expected preview summary fallback, got %q", title)
	}
}

func TestThreadDiscoveredDoesNotEraseExistingName(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-1",
		CWD:      "/data/dl/droid",
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.Name != "修复登录流程" {
		t.Fatalf("expected existing thread name to be preserved, got %#v", thread)
	}
}

func TestThreadFocusRequestsMetadataRefreshOnlyOnce(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
	})
	if len(first) != 1 {
		t.Fatalf("expected refresh command only, got %#v", first)
	}
	if first[0].Command == nil || first[0].Command.Kind != agentproto.CommandThreadsRefresh {
		t.Fatalf("expected threads refresh command, got %#v", first[0])
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadFocused,
		ThreadID: "thread-2",
		CWD:      "/data/dl/droid",
	})
	if len(second) != 0 {
		t.Fatalf("expected no duplicate UI events, got %#v", second)
	}
}

func TestDigitsTextAfterShowingThreadsIsSentAsNormalMessage(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionShowThreads, SurfaceSessionID: "surface-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "1",
	})

	if len(events) != 3 || events[0].PendingInput == nil || events[1].PendingInput == nil || events[2].Command == nil {
		t.Fatalf("expected normal queued message flow, got %#v", events)
	}
	if events[2].Command.Kind != agentproto.CommandPromptSend || events[2].Command.Prompt.Inputs[0].Text != "1" {
		t.Fatalf("expected digits to be sent as normal text, got %#v", events[2].Command)
	}
}

func TestLegacyPromptSelectionActionShowsExpiredNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	useEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSelectPrompt,
		SurfaceSessionID: "surface-1",
		PromptID:         "prompt-1",
		OptionID:         "thread-1",
	})
	if len(useEvents) != 1 || useEvents[0].Notice == nil || useEvents[0].Notice.Code != "selection_expired" {
		t.Fatalf("expected selection_expired notice for legacy prompt action, got %#v", useEvents)
	}
}

func TestConfirmKickThreadActionRequiresThreadID(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	useEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionConfirmKickThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "",
	})
	if len(useEvents) != 1 || useEvents[0].Notice == nil || useEvents[0].Notice.Code != "selection_invalid" {
		t.Fatalf("expected selection_invalid for missing kick thread id, got %#v", useEvents)
	}
}

func TestLocalTurnCompletedWithoutLocalPauseDoesNotEnterHandoff(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(events) != 0 {
		t.Fatalf("expected no UI events for stray local completion, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected dispatch mode to stay normal, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}

	now = now.Add(2 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no resume notice after stray local completion, got %#v", tick)
	}
}

func TestLocalPauseWithoutQueuedMessagesDoesNotEmitResumeNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-1",
		Action:   "turn_start",
	})
	if len(first) != 1 || first[0].Notice == nil || first[0].Notice.Code != "local_activity_detected" {
		t.Fatalf("expected only local pause notice, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(second) != 0 {
		t.Fatalf("expected no handoff events when queue is empty, got %#v", second)
	}
	if svc.root.Surfaces["surface-1"].DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected surface to return directly to normal mode, got %q", svc.root.Surfaces["surface-1"].DispatchMode)
	}

	now = now.Add(2 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no delayed resume notice with empty queue, got %#v", tick)
	}
}

func TestDisplayThreadTitleDisambiguatesDuplicateTitles(t *testing.T) {
	inst := &state.InstanceRecord{
		DisplayName:   "dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		WorkspaceRoot: "/data/dl",
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "新会话", CWD: "/data/dl"},
			"019d56f0-e48d-7e51-be84-04a5658e4c96": {ThreadID: "019d56f0-e48d-7e51-be84-04a5658e4c96", Name: "新会话", CWD: "/data/dl"},
		},
	}

	first := displayThreadTitle(inst, inst.Threads["019d56f0-de5e-7943-bc9a-18c42ef11acb"], "019d56f0-de5e-7943-bc9a-18c42ef11acb")
	second := displayThreadTitle(inst, inst.Threads["019d56f0-e48d-7e51-be84-04a5658e4c96"], "019d56f0-e48d-7e51-be84-04a5658e4c96")
	if first == second {
		t.Fatalf("expected duplicate thread titles to be disambiguated, got %q and %q", first, second)
	}
	if !strings.Contains(first, "de5e…1acb") || !strings.Contains(second, "e48d…4c96") {
		t.Fatalf("expected disambiguated titles to include short ids, got %q and %q", first, second)
	}
}

func TestThreadTitleFallsBackToPreviewSummary(t *testing.T) {
	title := threadTitle(&state.InstanceRecord{
		DisplayName:  "droid",
		WorkspaceKey: "/data/dl/droid",
		ShortName:    "droid",
	}, &state.ThreadRecord{
		ThreadID: "thread-1",
		Preview:  "我先按 fschannel 这个工程统计了入口文件和模块边界。",
		CWD:      "/data/dl/droid",
	}, "thread-1")

	if title != "droid · 我先按 fschannel 这个工程统计了入口文件和模块边界。" {
		t.Fatalf("unexpected preview-based title: %q", title)
	}
}

func TestPresentThreadSelectionIncludesStableShortIDInSubtitle(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "新会话", CWD: "/data/dl"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 1 || events[0].SelectionPrompt == nil || len(events[0].SelectionPrompt.Options) != 1 {
		t.Fatalf("expected one thread selection prompt, got %#v", events)
	}
	if events[0].SelectionPrompt.Title != "最近会话" {
		t.Fatalf("expected recent session prompt title, got %#v", events[0].SelectionPrompt)
	}
	if events[0].SelectionPrompt.Options[0].Subtitle != "/data/dl\n可切换" {
		t.Fatalf("expected subtitle to prefer cwd, got %#v", events[0].SelectionPrompt.Options[0])
	}
}

func TestPresentThreadSelectionShowsMostRecentFive(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	inst := &state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	}
	for i := 1; i <= 6; i++ {
		threadID := "thread-" + string(rune('0'+i))
		inst.Threads[threadID] = &state.ThreadRecord{
			ThreadID:   threadID,
			Name:       "会话" + string(rune('0'+i)),
			CWD:        "/data/dl",
			LastUsedAt: now.Add(time.Duration(i) * time.Minute),
			ListOrder:  i,
		}
	}
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if len(prompt.Options) != 5 {
		t.Fatalf("expected recent prompt to show five sessions, got %#v", prompt.Options)
	}
	if prompt.Title != "最近会话" || prompt.Hint != "发送 `/useall` 查看全部会话。" {
		t.Fatalf("unexpected recent prompt metadata: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-6" || prompt.Options[4].OptionID != "thread-2" {
		t.Fatalf("expected most recent sessions first, got %#v", prompt.Options)
	}
}

func TestPresentAllThreadSelectionShowsAllSessionsByRecency(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "较早会话", CWD: "/data/dl", LastUsedAt: now.Add(1 * time.Minute), ListOrder: 2},
			"thread-2": {ThreadID: "thread-2", Name: "最新会话", CWD: "/data/dl", LastUsedAt: now.Add(2 * time.Minute), ListOrder: 1},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "全部会话" || prompt.Hint != "" {
		t.Fatalf("unexpected all-session prompt metadata: %#v", prompt)
	}
	if len(prompt.Options) != 2 || prompt.Options[0].OptionID != "thread-2" || prompt.Options[1].OptionID != "thread-1" {
		t.Fatalf("expected all sessions sorted by recency, got %#v", prompt.Options)
	}
}

func TestUseThreadSameAsCurrentStillAcknowledgesSelection(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "dl",
		WorkspaceRoot:           "/data/dl",
		WorkspaceKey:            "/data/dl",
		ShortName:               "dl",
		Online:                  true,
		ObservedFocusedThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb",
		Threads: map[string]*state.ThreadRecord{
			"019d56f0-de5e-7943-bc9a-18c42ef11acb": {ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", Name: "修复登录流程", CWD: "/data/dl"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "019d56f0-de5e-7943-bc9a-18c42ef11acb",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "selection_unchanged" {
		t.Fatalf("expected unchanged selection notice, got %#v", events)
	}
}

func TestNewLocalThreadSequenceAnnouncesSelectionOnlyOnce(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	var selectionEvents []control.UIEvent
	selectionEvents = append(selectionEvents, svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventThreadDiscovered,
		ThreadID: "thread-2",
		CWD:      "/data/dl",
	})...)
	selectionEvents = append(selectionEvents, svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})...)
	selectionEvents = append(selectionEvents, svc.renderTextItem("inst-1", "thread-2", "turn-1", "item-1", "你好", true)...)

	count := 0
	for _, event := range selectionEvents {
		if event.ThreadSelection != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one selection change announcement, got %d from %#v", count, selectionEvents)
	}
}

func TestLocalPlaceholderInteractionDoesNotStealSelectionFromRunningThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "dl",
		WorkspaceRoot: "/data/dl",
		WorkspaceKey:  "/data/dl",
		ShortName:     "dl",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-6d13": {ThreadID: "thread-6d13", Name: "主线程", CWD: "/data/dl"},
			"thread-81a0": {ThreadID: "thread-81a0", Name: "占位线程", CWD: "/home/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionFollowLocal, SurfaceSessionID: "surface-1"})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-6d13",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})
	if len(started) != 2 || started[1].ThreadSelection == nil || started[1].ThreadSelection.ThreadID != "thread-6d13" {
		t.Fatalf("expected selection to switch to executing thread, got %#v", started)
	}

	later := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventLocalInteractionObserved,
		ThreadID: "thread-81a0",
		CWD:      "/home/dl/droid",
		Action:   "turn_start",
	})
	if len(later) != 0 {
		t.Fatalf("expected placeholder interaction during running local turn not to emit extra selection updates, got %#v", later)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-6d13" || surface.RouteMode != state.RouteModeFollowLocal {
		t.Fatalf("expected selected thread to remain on executing thread, got %q", surface.SelectedThreadID)
	}
	inst := svc.root.Instances["inst-1"]
	if inst.ObservedFocusedThreadID != "thread-81a0" {
		t.Fatalf("expected observed focus to still record latest local placeholder thread, got %q", inst.ObservedFocusedThreadID)
	}
	if inst.ActiveThreadID != "thread-6d13" {
		t.Fatalf("expected active thread to remain executing thread, got %q", inst.ActiveThreadID)
	}
}

func TestThreadsSnapshotDoesNotDropPreviouslyObservedThread(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	})

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread == nil {
		t.Fatal("expected observed thread to be preserved after empty snapshot")
	}
	if thread.Name != "修复登录流程" || thread.CWD != "/data/dl/droid" {
		t.Fatalf("expected thread metadata to be preserved, got %#v", thread)
	}
	if thread.Loaded {
		t.Fatalf("expected preserved thread to be marked not loaded after empty snapshot, got %#v", thread)
	}
}

func TestPendingRemoteDispatchKeepsLaterMessageQueuedUntilTurnStarts(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	dispatched := false
	for _, event := range first {
		if event.Command != nil {
			dispatched = true
			break
		}
	}
	if !dispatched {
		t.Fatalf("expected first surface to dispatch immediately, got %#v", first)
	}
	if binding := svc.pendingRemote["inst-1"]; binding == nil || binding.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected pending remote binding for surface-1, got %#v", binding)
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "排队",
	})
	if len(second) != 1 || second[0].PendingInput == nil || second[0].PendingInput.Status != string(state.QueueItemQueued) {
		t.Fatalf("expected follow-up message to stay queued, got %#v", second)
	}
	for _, event := range second {
		if event.Command != nil {
			t.Fatalf("expected no second dispatch while instance reserved, got %#v", second)
		}
	}
	if svc.root.Surfaces["surface-1"].ActiveQueueItemID == "" {
		t.Fatalf("expected first queue item to remain active while turn start is pending")
	}
	if len(svc.root.Surfaces["surface-1"].QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected queue to retain one waiting item, got %#v", svc.root.Surfaces["surface-1"].QueuedQueueItemIDs)
	}
}

func TestRemoteTurnLifecycleUsesExplicitSurfaceBinding(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-2"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "你好",
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if binding := svc.activeRemote["inst-1"]; binding == nil || binding.SurfaceSessionID != "surface-1" || binding.TurnID != "turn-1" || binding.ThreadID != "thread-2" {
		t.Fatalf("expected active remote binding to follow the queued route, got %#v", binding)
	}
	if len(started) == 0 || started[0].PendingInput == nil || started[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected running state to project to queued surface, got %#v", started)
	}

	mid := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-2",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "您好"},
	})
	if len(mid) != 0 {
		t.Fatalf("expected assistant text to stay buffered until turn completion, got %#v", mid)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-2",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if svc.activeRemote["inst-1"] != nil {
		t.Fatalf("expected active remote binding to clear after completion, got %#v", svc.activeRemote["inst-1"])
	}
	var sawFinal, sawTypingOff bool
	for _, event := range finished {
		if event.Block != nil && event.Block.Final {
			sawFinal = true
			if event.SurfaceSessionID != "surface-1" || event.Block.Text != "您好" {
				t.Fatalf("expected final block on queued surface, got %#v", event)
			}
		}
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
			if event.SurfaceSessionID != "surface-1" {
				t.Fatalf("expected typing-off on queued surface, got %#v", event)
			}
		}
	}
	if !sawFinal || !sawTypingOff {
		t.Fatalf("expected final block and typing-off on queued surface, got %#v", finished)
	}
}

func TestTurnCompletedEmbedsFileChangeSummaryIntoFinalAssistantBlock(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1,2 @@\n-old\n+new\n+more",
		}},
	}); len(events) != 0 {
		t.Fatalf("expected file change completion to stay buffered until turn completion, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成修改。"},
	}); len(events) != 0 {
		t.Fatalf("expected assistant final text to stay buffered until turn completion, got %#v", events)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		event := finished[i]
		if event.Block != nil && event.Block.Final && event.Block.Text == "已完成修改。" {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected final assistant block, got %#v", finished)
	}
	if finalBlockEvent.FileChangeSummary == nil {
		t.Fatalf("expected final assistant block to embed file summary, got %#v", finalBlockEvent)
	}
	if finalBlockEvent.FileChangeSummary.FileCount != 1 || finalBlockEvent.FileChangeSummary.AddedLines != 2 || finalBlockEvent.FileChangeSummary.RemovedLines != 1 {
		t.Fatalf("unexpected embedded file change summary payload: %#v", finalBlockEvent.FileChangeSummary)
	}
}

func TestTurnCompletedAggregatesMultipleCompletedFileChangeItemsIntoFinalBlock(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-2",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "docs/guide.md",
			Kind: agentproto.FileChangeAdd,
			Diff: "line 1\nline 2",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "已完成修改。"},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var summary *control.FileChangeSummary
	for _, event := range finished {
		if event.Block != nil && event.Block.Final {
			summary = event.FileChangeSummary
		}
	}
	if summary == nil {
		t.Fatalf("expected aggregated file change summary on final block, got %#v", finished)
	}
	if summary.FileCount != 2 || summary.AddedLines != 3 || summary.RemovedLines != 1 {
		t.Fatalf("unexpected aggregated summary totals: %#v", summary)
	}
	if len(summary.Files) != 2 {
		t.Fatalf("expected two file entries, got %#v", summary.Files)
	}
	if summary.Files[0].Path != "docs/guide.md" || summary.Files[1].Path != "pkg/app.go" {
		t.Fatalf("expected summary files to be sorted by path, got %#v", summary.Files)
	}
}

func TestTurnCompletedSynthesizesFinalBlockWhenOnlyFileSummaryExists(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "completed",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	var finalBlockEvent *control.UIEvent
	for i := range finished {
		if finished[i].Block != nil && finished[i].Block.Final {
			finalBlockEvent = &finished[i]
		}
	}
	if finalBlockEvent == nil {
		t.Fatalf("expected synthetic final block when only file summary exists, got %#v", finished)
	}
	if finalBlockEvent.Block.Text != "已完成文件修改。" {
		t.Fatalf("expected synthetic final block text, got %#v", finalBlockEvent.Block)
	}
	if finalBlockEvent.FileChangeSummary == nil || finalBlockEvent.FileChangeSummary.FileCount != 1 {
		t.Fatalf("expected synthetic final block to carry file summary, got %#v", finalBlockEvent)
	}
}

func TestDeclinedFileChangeDoesNotEmbedFinalSummary(t *testing.T) {
	now := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "处理一下",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "file-1",
		ItemKind: "file_change",
		Status:   "declined",
		FileChanges: []agentproto.FileChangeRecord{{
			Path: "pkg/app.go",
			Kind: agentproto.FileChangeUpdate,
			Diff: "@@ -1 +1 @@\n-old\n+new",
		}},
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Metadata: map[string]any{"text": "未执行文件改动。"},
	})

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	for _, event := range finished {
		if event.FileChangeSummary != nil {
			t.Fatalf("expected declined file change not to produce final summary, got %#v", finished)
		}
	}
}

func TestHandleCommandDispatchFailureClearsPendingRemoteState(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})

	events := svc.HandleCommandDispatchFailure("surface-1", errors.New("relay unavailable"))
	if svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected pending remote binding to clear after dispatch failure")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected surface active queue to clear after dispatch failure")
	}
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected queue item to be marked failed, got %#v", item)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "dispatch_failed" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and failure notice, got %#v", events)
	}
}

func TestHandleCommandRejectedClearsPendingRemoteState(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.BindPendingRemoteCommand("surface-1", "cmd-1")

	events := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-1",
		Accepted:  false,
		Error:     "translator failed",
		Problem: &agentproto.ErrorInfo{
			Code:      "translate_command_failed",
			Layer:     "wrapper",
			Stage:     "translate_command",
			Message:   "wrapper 无法把 relay 命令转换成 Codex 请求。",
			Details:   "translator failed",
			CommandID: "cmd-1",
		},
	})
	if svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected pending remote binding to clear after rejected command")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected active queue to clear after rejected command")
	}
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected queue item to be marked failed, got %#v", item)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "command_rejected" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and rejection notice, got %#v", events)
	}
	for _, event := range events {
		if event.Notice == nil || event.Notice.Code != "command_rejected" {
			continue
		}
		if !strings.Contains(event.Notice.Title, "wrapper.translate_command") || !strings.Contains(event.Notice.Text, "translator failed") {
			t.Fatalf("expected structured rejection notice, got %#v", event.Notice)
		}
	}
}

func TestApplyAgentSystemErrorTargetsAttachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
		Code:      "stdout_parse_failed",
		Layer:     "wrapper",
		Stage:     "observe_codex_stdout",
		Operation: "codex.stdout",
		Message:   "wrapper 无法解析 Codex 子进程输出的 JSON-RPC 帧。",
		Details:   "invalid character 'x' looking for beginning of value",
	}))
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected one problem notice, got %#v", events)
	}
	if events[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected notice on attached surface, got %#v", events[0])
	}
	if events[0].Notice.Code != debugErrorNoticeCode {
		t.Fatalf("unexpected notice code: %#v", events[0].Notice)
	}
	if !strings.Contains(events[0].Notice.Title, "wrapper.observe_codex_stdout") || !strings.Contains(events[0].Notice.Text, "invalid character") {
		t.Fatalf("expected structured problem text, got %#v", events[0].Notice)
	}
}

func TestRemoteTurnInterruptedWithProblemFailsQueueAndEmitsStructuredNotice(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected before completion: stream closed before response.completed",
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected before completion: stream closed before response.completed",
			Details:   "stream disconnected before completion: stream closed before response.completed",
			ThreadID:  "thread-1",
			TurnID:    "turn-1",
			Retryable: true,
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected interrupted remote turn with problem to fail queue item, got %#v", item)
	}

	var sawFailedPending, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.Status == string(state.QueueItemFailed) {
			sawFailedPending = true
		}
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawNotice = true
			if !strings.Contains(event.Notice.Title, "codex.runtime_error") || !strings.Contains(event.Notice.Text, "responseStreamDisconnected") {
				t.Fatalf("expected structured turn failure notice, got %#v", event.Notice)
			}
		}
	}
	if !sawFailedPending || !sawNotice {
		t.Fatalf("expected failed queue state and structured notice, got %#v", events)
	}
}

func TestApplyInstanceDisconnectedFailsActiveRemoteItem(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条",
	})

	events := svc.ApplyInstanceDisconnected("inst-1")
	if svc.activeRemote["inst-1"] != nil || svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected remote ownership to clear on disconnect")
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected active queue to clear on disconnect")
	}
	if surface.DispatchMode != state.DispatchModeNormal {
		t.Fatalf("expected dispatch mode to reset on disconnect, got %s", surface.DispatchMode)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" {
		t.Fatalf("expected surface to detach on disconnect, got %+v", surface)
	}
	active := surface.QueueItems["queue-1"]
	if active == nil || active.Status != state.QueueItemFailed {
		t.Fatalf("expected active queue item to fail on disconnect, got %#v", active)
	}
	queued := surface.QueueItems["queue-2"]
	if queued == nil || queued.Status != state.QueueItemQueued {
		t.Fatalf("expected queued item to remain queued on disconnect, got %#v", queued)
	}
	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "attached_instance_offline" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and offline notice, got %#v", events)
	}
}

func TestApplyInstanceTransportDegradedKeepsAttachmentAndQueuedWork(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
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
		ItemKind: "agent_message",
		Delta:    "部分输出",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条",
	})

	events := svc.ApplyInstanceTransportDegraded("inst-1", true)
	if svc.activeRemote["inst-1"] != nil || svc.pendingRemote["inst-1"] != nil {
		t.Fatalf("expected remote ownership to clear on transport degrade")
	}
	if svc.root.Instances["inst-1"].Online || svc.root.Instances["inst-1"].ActiveTurnID != "" {
		t.Fatalf("expected instance to become offline without active turn, got %#v", svc.root.Instances["inst-1"])
	}
	if len(svc.itemBuffers) != 0 {
		t.Fatalf("expected turn buffers to clear on transport degrade, got %#v", svc.itemBuffers)
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID != "" {
		t.Fatalf("expected active queue to clear on transport degrade")
	}
	if surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected attachment and selected thread to stay, got %+v", surface)
	}
	active := surface.QueueItems["queue-1"]
	if active == nil || active.Status != state.QueueItemFailed {
		t.Fatalf("expected active queue item to fail on transport degrade, got %#v", active)
	}
	queued := surface.QueueItems["queue-2"]
	if queued == nil || queued.Status != state.QueueItemQueued {
		t.Fatalf("expected queued item to remain queued on transport degrade, got %#v", queued)
	}

	var sawTypingOff, sawNotice bool
	for _, event := range events {
		if event.PendingInput != nil && event.PendingInput.QueueItemID == "queue-1" && event.PendingInput.TypingOff {
			sawTypingOff = true
		}
		if event.Notice != nil && event.Notice.Code == "attached_instance_transport_degraded" {
			sawNotice = true
		}
	}
	if !sawTypingOff || !sawNotice {
		t.Fatalf("expected typing-off and degraded notice, got %#v", events)
	}

	recovery := svc.ApplyInstanceConnected("inst-1")
	if surface.ActiveQueueItemID != "queue-2" {
		t.Fatalf("expected queued work to resume after reconnect, got active=%s", surface.ActiveQueueItemID)
	}
	resumed := surface.QueueItems["queue-2"]
	if resumed == nil || resumed.Status != state.QueueItemDispatching {
		t.Fatalf("expected queued item to re-dispatch after reconnect, got %#v", resumed)
	}
	var sawCommand bool
	for _, event := range recovery {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawCommand = true
		}
	}
	if !sawCommand {
		t.Fatalf("expected reconnect to resume queued work, got %#v", recovery)
	}
}

func TestApplyInstanceConnectedDoesNotResumeDetachedSurfaceQueue(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	svc.ApplyInstanceDisconnected("inst-1")
	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(queued) != 1 || queued[0].Notice == nil || queued[0].Notice.Code != "not_attached" {
		t.Fatalf("expected detached surface to reject new input, got %#v", queued)
	}

	events := svc.ApplyInstanceConnected("inst-1")
	if len(events) != 0 {
		t.Fatalf("expected reconnect not to resume a detached surface, got %#v", events)
	}
}

func TestNewInstanceRequiresDetachedSurface(t *testing.T) {
	now := time.Date(2026, 4, 5, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_requires_detach" {
		t.Fatalf("expected headless_requires_detach notice, got %#v", events)
	}
}

func TestNewInstanceStartsHeadlessAndBlocksNormalInput(t *testing.T) {
	now := time.Date(2026, 4, 5, 9, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 2 || events[0].Notice == nil || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected notice + headless start command, got %#v", events)
	}
	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID == "" || snapshot.PendingHeadless.Status != string(state.HeadlessLaunchStarting) {
		t.Fatalf("expected pending headless snapshot, got %#v", snapshot)
	}
	if !strings.HasPrefix(snapshot.PendingHeadless.InstanceID, "inst-headless-") {
		t.Fatalf("expected generated headless instance id, got %#v", snapshot.PendingHeadless)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	if len(blocked) != 1 || blocked[0].Notice == nil || blocked[0].Notice.Code != "headless_starting" {
		t.Fatalf("expected headless_starting notice while launch pending, got %#v", blocked)
	}
}

func TestApplyInstanceConnectedAttachesPendingHeadlessAndRequestsRefresh(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	events := svc.ApplyInstanceConnected(pending.InstanceID)

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "" {
		t.Fatalf("expected headless instance to auto-attach, got %#v", snapshot)
	}
	if snapshot.Attachment.Source != "headless" || !snapshot.Attachment.Managed {
		t.Fatalf("expected managed headless attachment, got %#v", snapshot.Attachment)
	}
	if snapshot.PendingHeadless.InstanceID != pending.InstanceID || snapshot.PendingHeadless.Status != string(state.HeadlessLaunchSelecting) {
		t.Fatalf("expected pending headless selection state, got %#v", snapshot.PendingHeadless)
	}
	var attached bool
	var requestedRefresh bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "headless_attached" {
			attached = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandThreadsRefresh {
			requestedRefresh = true
		}
	}
	if !attached || !requestedRefresh {
		t.Fatalf("expected headless_attached notice + refresh command, got %#v", events)
	}
}

func TestPendingHeadlessSelectingBlocksUseThreadUntilResumeChosen(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_selection_waiting" {
		t.Fatalf("expected headless selection gate to block /use, got %#v", events)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.PendingHeadless.Status != string(state.HeadlessLaunchSelecting) {
		t.Fatalf("expected pending headless to remain selecting, got %#v", snapshot)
	}
}

func TestHeadlessThreadSnapshotPromptsForResumeSelection(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)

	events := svc.ApplyAgentEvent(pending.InstanceID, agentproto.Event{
		Kind: agentproto.EventThreadsSnapshot,
		Threads: []agentproto.ThreadSnapshotRecord{{
			ThreadID:  "thread-1",
			Name:      "修复登录流程",
			Preview:   "修登录",
			CWD:       "/data/dl/droid",
			Loaded:    true,
			ListOrder: 1,
		}},
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.NextPrompt.ThreadID != "" || snapshot.PendingHeadless.Status != string(state.HeadlessLaunchSelecting) {
		t.Fatalf("expected pending selection snapshot, got %#v", snapshot)
	}
	var selectionPrompt bool
	for _, event := range events {
		if event.SelectionPrompt != nil && event.SelectionPrompt.Kind == control.SelectionPromptNewInstance {
			selectionPrompt = true
		}
	}
	if !selectionPrompt {
		t.Fatalf("expected headless selection prompt, got %#v", events)
	}
}

func TestHeadlessThreadSnapshotWithoutRecoverableThreadsFailsAndKillsInstance(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 18, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)

	events := svc.ApplyAgentEvent(pending.InstanceID, agentproto.Event{
		Kind:    agentproto.EventThreadsSnapshot,
		Threads: nil,
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected failed headless selection to detach surface, got %#v", snapshot)
	}
	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].Notice == nil || events[1].Notice.Code != "no_recoverable_threads" {
		t.Fatalf("expected kill command + no_recoverable_threads notice, got %#v", events)
	}
}

func TestHeadlessThreadSelectionCompletesLaunch(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)
	svc.presentHeadlessResumeSelection(svc.root.Surfaces["surface-1"], svc.root.Instances[pending.InstanceID])

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionResumeHeadless,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.InstanceID != "" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected headless launch to complete after selection, got %#v", snapshot)
	}
	if snapshot.Attachment.DisplayName != "droid" {
		t.Fatalf("expected managed headless instance to retarget workspace metadata, got %#v", snapshot.Attachment)
	}
	var changed bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("expected thread selection change, got %#v", events)
	}
}

func TestDetachTimeoutWatchdogForcesFinalizeAfterRunningTurn(t *testing.T) {
	now := time.Date(2026, 4, 5, 11, 30, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	detach := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})
	if len(detach) < 2 {
		t.Fatalf("expected interrupt + detach_pending flow, got %#v", detach)
	}
	surface := svc.root.Surfaces["surface-1"]
	if !surface.Abandoning {
		t.Fatalf("expected surface to enter abandoning state")
	}

	now = now.Add(21 * time.Second)
	events := svc.Tick(now)

	surface = svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning {
		t.Fatalf("expected watchdog to force detach, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected instance claim to be released, got %#v", claim)
	}
	var sawForced bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detach_timeout_forced" {
			sawForced = true
		}
	}
	if !sawForced {
		t.Fatalf("expected detach_timeout_forced notice, got %#v", events)
	}
}

func TestKillInstanceCancelsPendingHeadlessLaunch(t *testing.T) {
	now := time.Date(2026, 4, 5, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionKillInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.Kind != control.DaemonCommandKillHeadless || events[1].Notice == nil || events[1].Notice.Code != "headless_cancelled" {
		t.Fatalf("expected kill command + cancellation notice, got %#v", events)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected pending headless to clear, got %#v", snapshot)
	}
}

func TestKillInstanceDetachesManagedHeadless(t *testing.T) {
	now := time.Date(2026, 4, 5, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "headless",
		Managed:       true,
		PID:           4321,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-headless-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionKillInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 2 || events[0].DaemonCommand == nil || events[0].DaemonCommand.InstanceID != "inst-headless-1" || events[1].Notice == nil || events[1].Notice.Code != "headless_kill_requested" {
		t.Fatalf("expected kill command + headless notice, got %#v", events)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.Attachment.InstanceID != "" {
		t.Fatalf("expected surface to detach after kill request, got %#v", snapshot)
	}
}

func TestKillInstanceRejectsNormalInstance(t *testing.T) {
	now := time.Date(2026, 4, 5, 11, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		Source:        "vscode",
		Managed:       false,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionKillInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "headless_kill_forbidden" {
		t.Fatalf("expected headless_kill_forbidden notice, got %#v", events)
	}
}

func TestNewThreadReadyDiscardsDraftsAndPreparesCreate(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || surface.SelectedThreadID != "" {
		t.Fatalf("expected new_thread_ready without selected thread, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if surface.PreparedThreadCWD != "/data/dl/droid" || surface.PreparedFromThreadID != "thread-1" {
		t.Fatalf("expected prepared cwd/thread to be captured, got %#v", surface)
	}
	if claim := svc.threadClaims["thread-1"]; claim != nil {
		t.Fatalf("expected prior thread claim to be released, got %#v", claim)
	}
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected staged images to be discarded, got %#v", surface.StagedImages)
	}
	var sawSelection, sawNotice bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.RouteMode == string(state.RouteModeNewThreadReady) && event.ThreadSelection.Title == preparedNewThreadSelectionTitle() {
			sawSelection = true
		}
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawNotice = true
		}
	}
	if !sawSelection || !sawNotice {
		t.Fatalf("expected new-thread selection change plus notice, got %#v", events)
	}
}

func TestNewThreadReadyFirstTextQueuesCreateAndBlocksSecondInput(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 30, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "开一个新会话",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected active queue item after first /new text, got %#v", surface)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil {
		t.Fatal("expected active queue item record")
	}
	if item.FrozenThreadID != "" || item.FrozenCWD != "/data/dl/droid" || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		t.Fatalf("expected create-thread queue item, got %#v", item)
	}
	var sawCreateThread bool
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && event.Command.Target.CreateThreadIfMissing {
			sawCreateThread = true
		}
	}
	if !sawCreateThread {
		t.Fatalf("expected prompt send with CreateThreadIfMissing, got %#v", events)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "第二条不该进来",
	})
	if len(blocked) != 1 || blocked[0].Notice == nil || blocked[0].Notice.Code != "new_thread_first_input_pending" {
		t.Fatalf("expected second input to be blocked, got %#v", blocked)
	}
}

func TestNewThreadTurnStartedBindsSurfaceBackToPinnedThread(t *testing.T) {
	now := time.Date(2026, 4, 6, 11, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "开新会话"})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-created",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-created" {
		t.Fatalf("expected surface to rebind to created thread, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if surface.PreparedThreadCWD != "" || surface.PreparedFromThreadID != "" {
		t.Fatalf("expected prepared new-thread state to clear, got %#v", surface)
	}
	if claim := svc.threadClaims["thread-created"]; claim == nil || claim.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected created thread claim to belong to surface, got %#v", claim)
	}
	var sawPinnedSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-created" && event.ThreadSelection.RouteMode == string(state.RouteModePinned) {
			sawPinnedSelection = true
		}
	}
	if !sawPinnedSelection {
		t.Fatalf("expected pinned selection change after turn started, got %#v", events)
	}
}

func TestUseThreadFromNewThreadReadyDiscardsQueuedDraft(t *testing.T) {
	now := time.Date(2026, 4, 6, 11, 30, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
			"thread-2": {ThreadID: "thread-2", Name: "整理日志", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.DispatchMode = state.DispatchModePausedForLocal
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "先排队一个新会话"})

	var queued *state.QueueItemRecord
	for _, item := range surface.QueueItems {
		if item.Status == state.QueueItemQueued {
			queued = item
			break
		}
	}
	if queued == nil {
		t.Fatalf("expected queued create-thread item, got %#v", surface.QueueItems)
	}

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if surface.RouteMode != state.RouteModePinned || surface.SelectedThreadID != "thread-2" {
		t.Fatalf("expected surface to switch to thread-2, got route=%q selected=%q", surface.RouteMode, surface.SelectedThreadID)
	}
	if queued.Status != state.QueueItemDiscarded {
		t.Fatalf("expected queued create-thread item to be discarded, got %#v", queued)
	}
}

func TestRepeatNewThreadReadyDiscardsQueuedDraft(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.DispatchMode = state.DispatchModePausedForLocal
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", MessageID: "msg-1", Text: "先排队"})

	var queued *state.QueueItemRecord
	for _, item := range surface.QueueItems {
		if item.Status == state.QueueItemQueued {
			queued = item
			break
		}
	}
	if queued == nil {
		t.Fatalf("expected queued create-thread item, got %#v", surface.QueueItems)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	if surface.RouteMode != state.RouteModeNewThreadReady || surface.PreparedThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected surface to stay new_thread_ready, got %#v", surface)
	}
	if queued.Status != state.QueueItemDiscarded {
		t.Fatalf("expected queued draft to be discarded, got %#v", queued)
	}
	if len(events) == 0 || events[len(events)-1].Notice == nil || events[len(events)-1].Notice.Code != "new_thread_ready_reset" {
		t.Fatalf("expected reset notice, got %#v", events)
	}
}

func TestNewThreadRejectedDuringRequestCapture(t *testing.T) {
	now := time.Date(2026, 4, 6, 12, 30, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].ActiveRequestCapture = &state.RequestCaptureRecord{
		RequestID: "req-1",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_capture_waiting_text" {
		t.Fatalf("expected request capture gate to reject /new, got %#v", events)
	}
}

func TestShowThreadsDetachedShowsGlobalMergedRecentThreads(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", LastUsedAt: now.Add(1 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected detached /use to show global thread prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	if prompt.Title != "最近会话" || len(prompt.Options) != 2 {
		t.Fatalf("unexpected prompt: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "thread-2" || prompt.Options[0].ButtonLabel != "启动" {
		t.Fatalf("expected newer offline thread to require headless start, got %#v", prompt.Options[0])
	}
	if prompt.Options[1].OptionID != "thread-1" || prompt.Options[1].ButtonLabel != "接管" {
		t.Fatalf("expected online free thread to support direct attach, got %#v", prompt.Options[1])
	}
}

func TestPresentGlobalThreadSelectionMarksBusyThreadDisabled(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-free": {ThreadID: "thread-free", Name: "空闲会话", CWD: "/data/dl/droid", LastUsedAt: now.Add(1 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-2",
		DisplayName:             "web",
		WorkspaceRoot:           "/data/dl/web",
		WorkspaceKey:            "/data/dl/web",
		ShortName:               "web",
		Online:                  true,
		ObservedFocusedThreadID: "thread-busy",
		Threads: map[string]*state.ThreadRecord{
			"thread-busy": {ThreadID: "thread-busy", Name: "忙碌会话", CWD: "/data/dl/web", LastUsedAt: now.Add(2 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-busy", ChatID: "chat-busy", ActorUserID: "user-busy", InstanceID: "inst-2"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].SelectionPrompt == nil {
		t.Fatalf("expected selection prompt, got %#v", events)
	}
	prompt := events[0].SelectionPrompt
	var busyOption *control.SelectionOption
	for i := range prompt.Options {
		option := prompt.Options[i]
		if option.OptionID == "thread-busy" {
			busyOption = &option
			break
		}
	}
	if busyOption == nil {
		t.Fatalf("expected busy thread in global prompt, got %#v", prompt.Options)
	}
	if !busyOption.Disabled || busyOption.ButtonLabel != "已占用" || !strings.Contains(busyOption.Subtitle, "占用") {
		t.Fatalf("expected busy thread to be disabled in global prompt, got %#v", busyOption)
	}
}

func TestUseThreadDetachedAttachesFreeVisibleInstanceAndReplays(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	recordLocalFinalText(t, svc, "inst-1", "thread-1", "turn-1", "item-1", "全局 /use 的 replay")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" || snapshot.Attachment.SelectedThreadID != "thread-1" {
		t.Fatalf("expected detached /use to attach visible instance and thread, got %#v", snapshot)
	}
	var sawReplay bool
	for _, event := range events {
		if event.Block != nil && event.Block.Text == "全局 /use 的 replay" && event.Block.Final {
			sawReplay = true
		}
	}
	if !sawReplay {
		t.Fatalf("expected global /use attach to replay stored final, got %#v", events)
	}
}

func TestUseThreadCrossInstanceAttachesResolvedTargetAfterDetachSemantics(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 30, 0, 0, time.UTC)
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionImageMessage, SurfaceSessionID: "surface-1", MessageID: "msg-img", LocalPath: "/tmp/img.png", MIMEType: "image/png"})
	svc.root.Surfaces["surface-1"].PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "inst-2" || surface.SelectedThreadID != "thread-2" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected cross-instance /use to retarget attachment, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected detach-style migration to clear prompt override, got %#v", surface.PromptOverride)
	}
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected drafts to be discarded on cross-instance attach, got %#v", surface.StagedImages)
	}
	if svc.instanceClaimSurface("inst-1") != nil || svc.instanceClaimSurface("inst-2") == nil {
		t.Fatalf("expected old instance claim released and new instance claimed")
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-2" {
			sawSelection = true
		}
	}
	if !sawSelection {
		t.Fatalf("expected thread selection event after cross-instance attach, got %#v", events)
	}
}

func TestUseThreadCrossInstanceBlockedByPendingRequest(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 35, 0, 0, time.UTC)
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-2",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].PendingRequests["req-1"] = &state.RequestPromptRecord{RequestID: "req-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-2",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "request_pending" {
		t.Fatalf("expected pending request gate to block cross-instance attach, got %#v", events)
	}
	if surface := svc.root.Surfaces["surface-1"]; surface.AttachedInstanceID != "inst-1" || surface.SelectedThreadID != "thread-1" {
		t.Fatalf("expected surface to remain on original attachment, got %#v", surface)
	}
}

func TestUseThreadDetachedStartsPreselectedHeadlessWhenOnlyOfflineSnapshotAvailable(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.PendingHeadless.ThreadID != "thread-1" || snapshot.PendingHeadless.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected detached /use to start preselected headless flow, got %#v", snapshot)
	}
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected headless start flow, got %#v", events)
	}
	if events[1].DaemonCommand.ThreadID != "thread-1" || events[1].DaemonCommand.ThreadCWD != "/data/dl/droid" {
		t.Fatalf("expected daemon command to carry preselected thread info, got %#v", events[1].DaemonCommand)
	}
}

func TestApplyInstanceConnectedAttachesPreselectedHeadlessThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 45, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	pending := svc.SurfaceSnapshot("surface-1").PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp",
		WorkspaceKey:  "/tmp",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplyInstanceConnected(pending.InstanceID)

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != pending.InstanceID || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected pending headless to auto-attach target thread, got %#v", snapshot)
	}
	if inst := svc.root.Instances[pending.InstanceID]; inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected managed headless metadata retargeted to thread cwd, got %#v", inst)
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" {
			sawSelection = true
		}
	}
	if !sawSelection {
		t.Fatalf("expected thread selection event when preselected headless connects, got %#v", events)
	}
}

func TestUseThreadDetachedReusesManagedHeadlessForKnownThread(t *testing.T) {
	now := time.Date(2026, 4, 7, 18, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp/headless",
		WorkspaceKey:  "/tmp/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-headless-1" || snapshot.Attachment.SelectedThreadID != "thread-1" || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected detached /use to reuse idle managed headless, got %#v", snapshot)
	}
	if inst := svc.root.Instances["inst-headless-1"]; inst.WorkspaceRoot != "/data/dl/droid" {
		t.Fatalf("expected reused headless metadata to retarget to thread cwd, got %#v", inst)
	}
	var sawSelection bool
	for _, event := range events {
		if event.ThreadSelection != nil && event.ThreadSelection.ThreadID == "thread-1" {
			sawSelection = true
		}
	}
	if !sawSelection {
		t.Fatalf("expected thread selection when reusing headless, got %#v", events)
	}
}
