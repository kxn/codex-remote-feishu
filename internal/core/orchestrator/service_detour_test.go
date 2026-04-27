package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newDetourTextService(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	now := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	return svc, svc.root.Surfaces["surface-1"]
}

func newDetachedBranchService(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	svc, surface := newDetourTextService(t)
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-main",
	})
	return svc, surface
}

func TestTextDetourForkEnqueuesForkEphemeralAndStripsEmoji(t *testing.T) {
	svc, surface := newDetachedBranchService(t)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-1",
		Text:             "⁉️ 顺手问个岔题",
	})

	if len(events) != 3 {
		t.Fatalf("expected queue-on, queue-off, and prompt command, got %#v", events)
	}
	if events[2].Command == nil || events[2].Command.Kind != agentproto.CommandPromptSend {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	command := events[2].Command
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeForkEphemeral ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.ThreadID != "" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected detour target: %#v", command.Target)
	}
	if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != "顺手问个岔题" {
		t.Fatalf("expected stripped detour prompt, got %#v", command.Prompt.Inputs)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.FrozenExecutionMode != agentproto.PromptExecutionModeForkEphemeral || item.FrozenSourceThreadID != "thread-main" {
		t.Fatalf("expected active detached queue item, got %#v", item)
	}
	if item.SourceMessagePreview != normalizeSourceMessagePreview("顺手问个岔题") {
		t.Fatalf("expected sanitized source preview, got %#v", item)
	}
}

func TestTextDetourForkAcceptsFeishuTextAlias(t *testing.T) {
	for _, raw := range []string{"[什么？] 顺手问个岔题", "[什么?] 顺手问个岔题"} {
		t.Run(raw, func(t *testing.T) {
			svc, surface := newDetachedBranchService(t)

			events := svc.ApplySurfaceAction(control.Action{
				Kind:             control.ActionTextMessage,
				SurfaceSessionID: surface.SurfaceSessionID,
				MessageID:        "msg-1",
				Text:             raw,
				Inputs: []agentproto.Input{
					{Type: agentproto.InputText, Text: raw},
				},
			})

			if len(events) != 3 {
				t.Fatalf("expected queue-on, queue-off, and prompt command, got %#v", events)
			}
			if events[2].Command == nil || events[2].Command.Kind != agentproto.CommandPromptSend {
				t.Fatalf("expected prompt send command, got %#v", events)
			}
			command := events[2].Command
			if command.Target.ExecutionMode != agentproto.PromptExecutionModeForkEphemeral ||
				command.Target.SourceThreadID != "thread-main" ||
				command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
				t.Fatalf("unexpected detour target: %#v", command.Target)
			}
			if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != "顺手问个岔题" {
				t.Fatalf("expected stripped detour prompt, got %#v", command.Prompt.Inputs)
			}
			item := surface.QueueItems[surface.ActiveQueueItemID]
			if item == nil || item.SourceMessagePreview != normalizeSourceMessagePreview("顺手问个岔题") {
				t.Fatalf("expected sanitized source preview, got %#v", item)
			}
			if len(item.Inputs) != 1 || item.Inputs[0].Text != "顺手问个岔题" {
				t.Fatalf("expected sanitized queued inputs, got %#v", item.Inputs)
			}
		})
	}
}

func TestTextDetourBlankWorksWhileSurfaceUnbound(t *testing.T) {
	svc, surface := newDetourTextService(t)
	surface.RouteMode = state.RouteModeUnbound
	surface.SelectedThreadID = ""

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-1",
		Text:             "🤷 临时问一句",
	})

	if len(events) != 3 {
		t.Fatalf("expected queue-on, queue-off, and prompt command, got %#v", events)
	}
	if events[2].Command == nil {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	command := events[2].Command
	if command.Target.ExecutionMode != agentproto.PromptExecutionModeStartEphemeral ||
		command.Target.SourceThreadID != "" ||
		command.Target.ThreadID != "" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected blank detour target: %#v", command.Target)
	}
	if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != "临时问一句" {
		t.Fatalf("expected stripped blank detour prompt, got %#v", command.Prompt.Inputs)
	}
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detour not to mutate unbound surface state, got thread=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
}

func TestTextDetourRejectsAmbiguousEmoji(t *testing.T) {
	svc, surface := newDetachedBranchService(t)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-1",
		Text:             "⁉️ 🤷 到底用哪个",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Text != detourAmbiguousTriggerText {
		t.Fatalf("expected ambiguous detour rejection, got %#v", events)
	}
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected no queue item after ambiguous detour, got active=%q queued=%#v", surface.ActiveQueueItemID, surface.QueuedQueueItemIDs)
	}
}

func TestDetourTextSkipsReplyAutoSteer(t *testing.T) {
	now := time.Date(2026, 4, 26, 14, 20, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-1",
		TargetMessageID:  "msg-active",
		Text:             "⁉️ 请重点看最后一段",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "<被引用内容>\n原始消息\n</被引用内容>"},
			{Type: agentproto.InputText, Text: "⁉️ 请重点看最后一段"},
		},
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "请重点看最后一段"},
		},
	})

	if len(events) != 1 || events[0].PendingInput == nil || !events[0].PendingInput.QueueOn {
		t.Fatalf("expected ordinary queued detour input, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected one queued detour item, got %#v", surface.QueuedQueueItemIDs)
	}
	item := surface.QueueItems[surface.QueuedQueueItemIDs[0]]
	if item == nil || item.FrozenExecutionMode != agentproto.PromptExecutionModeForkEphemeral || item.FrozenSourceThreadID != "thread-1" {
		t.Fatalf("expected queued fork detour item, got %#v", item)
	}
	if len(item.Inputs) != 2 || item.Inputs[1].Text != "请重点看最后一段" {
		t.Fatalf("expected sanitized queued inputs instead of steer inputs, got %#v", item.Inputs)
	}
}

func TestDetachedBranchCompletedTurnCarriesDetourLabelAndReturnNotice(t *testing.T) {
	svc, surface := newDetachedBranchService(t)
	startDetachedBranchRemoteTurnForTest(t, svc, surface, "thread-main", "thread-detour", "msg-1", "顺手问个岔题", "turn-detour")

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemDelta,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		ItemID:    "item-turn-detour",
		ItemKind:  "agent_message",
		Delta:     "已经处理完了。",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	}); len(events) != 0 {
		t.Fatalf("expected no UI events while buffering final text, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		ItemID:    "item-turn-detour",
		ItemKind:  "agent_message",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	}); len(events) != 0 {
		t.Fatalf("expected no UI events before detached branch turn completion, got %#v", events)
	}
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	var finalBlock *render.Block
	var finalIndex int
	var returnNotice *control.Notice
	var returnIndex int
	for i := range events {
		if events[i].Block != nil && events[i].Block.Final {
			finalBlock = events[i].Block
			finalIndex = i
		}
		if events[i].Notice != nil && events[i].Notice.Code == "detour_returned" {
			returnNotice = events[i].Notice
			returnIndex = i
		}
	}
	if finalBlock == nil || finalBlock.DetourLabel != detourForkLabel {
		t.Fatalf("expected final detached branch block to carry detour label, got %#v", events)
	}
	if returnNotice == nil || returnNotice.Text != detourReturnNoticeText {
		t.Fatalf("expected detour return notice, got %#v", events)
	}
	if finalIndex >= returnIndex {
		t.Fatalf("expected return notice after final block, got %#v", events)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected detached branch completion to keep main selection, got %q", surface.SelectedThreadID)
	}
}

func TestDetachedBranchTerminalTurnEmitsReturnNoticeForFailureAndInterrupt(t *testing.T) {
	cases := []struct {
		name          string
		status        string
		errorMessage  string
		interruptSelf bool
		expectFailure bool
	}{
		{name: "failed", status: "failed", errorMessage: "boom", expectFailure: true},
		{name: "interrupted", status: "interrupted", errorMessage: "stopped", interruptSelf: true, expectFailure: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, surface := newDetachedBranchService(t)
			startDetachedBranchRemoteTurnForTest(t, svc, surface, "thread-main", "thread-detour", "msg-1", "顺手问个岔题", "turn-detour")
			if tc.interruptSelf {
				svc.markRemoteTurnInterruptRequested("inst-1", "thread-detour", "turn-detour")
			}

			events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind:         agentproto.EventTurnCompleted,
				ThreadID:     "thread-detour",
				TurnID:       "turn-detour",
				Status:       tc.status,
				ErrorMessage: tc.errorMessage,
				Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
			})

			foundReturn := false
			foundFailure := false
			for _, event := range events {
				if event.Notice == nil {
					continue
				}
				switch event.Notice.Code {
				case "detour_returned":
					foundReturn = event.Notice.Text == detourReturnNoticeText
				case "turn_failed":
					foundFailure = true
					if len(event.Notice.Sections) == 0 || len(event.Notice.Sections[0].Lines) == 0 || event.Notice.Sections[0].Lines[0] != detourForkLabel {
						t.Fatalf("expected failure notice to keep detour badge, got %#v", event.Notice)
					}
				}
			}
			if !foundReturn {
				t.Fatalf("expected detour return notice, got %#v", events)
			}
			if foundFailure != tc.expectFailure {
				t.Fatalf("unexpected failure-notice presence: got=%v want=%v events=%#v", foundFailure, tc.expectFailure, events)
			}
		})
	}
}
