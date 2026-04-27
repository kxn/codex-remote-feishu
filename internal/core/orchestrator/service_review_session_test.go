package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func newReviewSessionService(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	now := time.Date(2026, 4, 26, 15, 0, 0, 0, time.UTC)
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
			"thread-main": {
				ThreadID: "thread-main",
				Name:     "主线程",
				CWD:      "/data/dl/droid",
				Loaded:   true,
			},
			"thread-review": {
				ThreadID:     "thread-review",
				Name:         "审阅线程",
				CWD:          "/data/dl/droid",
				Loaded:       true,
				ForkedFromID: "thread-main",
				Source: &agentproto.ThreadSourceRecord{
					Kind:           agentproto.ThreadSourceKindReview,
					Name:           "review",
					ParentThreadID: "thread-main",
				},
			},
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
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-main",
	})
	return svc, svc.root.Surfaces["surface-1"]
}

func activateReviewSessionForTest(t *testing.T, svc *Service, surface *state.SurfaceConsoleRecord, sourceMessageID, turnID string) {
	t.Helper()
	if surface == nil {
		t.Fatal("expected surface")
	}
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		ParentThreadID:  "thread-main",
		ReviewThreadID:  "thread-review",
		SourceMessageID: sourceMessageID,
	}
	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-review",
		TurnID:    turnID,
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	for _, event := range events {
		if event.ThreadSelection != nil {
			t.Fatalf("expected review turn start not to steal selection, got %#v", events)
		}
	}
}

func TestReviewSessionTurnStartActivatesWithoutStealingSelection(t *testing.T) {
	svc, surface := newReviewSessionService(t)

	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	session := surface.ReviewSession
	if session == nil || session.Phase != state.ReviewSessionPhaseActive {
		t.Fatalf("expected active review session, got %#v", session)
	}
	if session.ParentThreadID != "thread-main" || session.ReviewThreadID != "thread-review" || session.ActiveTurnID != "turn-review-1" {
		t.Fatalf("unexpected review session runtime: %#v", session)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected parent thread selection to remain pinned, got %q", surface.SelectedThreadID)
	}
	inst := svc.root.Instances["inst-1"]
	if inst.ActiveThreadID != "thread-review" || inst.ActiveTurnID != "turn-review-1" {
		t.Fatalf("expected instance active turn to follow review thread, got thread=%q turn=%q", inst.ActiveThreadID, inst.ActiveTurnID)
	}
}

func TestReviewSessionTextRoutesToReviewThreadAndKeepsSelection(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "这里需要再看一下边界情况",
	})

	if len(events) != 3 {
		t.Fatalf("expected queue-on, queue-off, and prompt command, got %#v", events)
	}
	if events[2].Command == nil || events[2].Command.Kind != agentproto.CommandPromptSend {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	command := events[2].Command
	if command.Target.ThreadID != "thread-review" ||
		command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected review session command target: %#v", command.Target)
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil ||
		item.FrozenThreadID != "thread-review" ||
		item.FrozenSourceThreadID != "thread-main" ||
		item.FrozenSurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected queued review session item: %#v", item)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected review reply to keep parent selection, got %q", surface.SelectedThreadID)
	}
}

func TestReviewSessionLifecycleAndReplyAnchorFallback(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")

	entered := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-enter",
		ItemKind:  "entered_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "未提交变更"},
	})
	if len(entered) != 0 {
		t.Fatalf("expected entered review lifecycle item to stay internal, got %#v", entered)
	}
	if surface.ReviewSession.TargetLabel != "未提交变更" {
		t.Fatalf("expected review target label to persist, got %#v", surface.ReviewSession)
	}

	exited := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-exit",
		ItemKind:  "exited_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "建议把 review/start 的 translator 回归测试补齐"},
	})
	if len(exited) != 0 {
		t.Fatalf("expected exited review lifecycle item to stay internal, got %#v", exited)
	}
	if surface.ReviewSession.LastReviewText != "建议把 review/start 的 translator 回归测试补齐" {
		t.Fatalf("expected review result to persist on session, got %#v", surface.ReviewSession)
	}

	requestEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		RequestID: "req-review-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(requestEvents) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", requestEvents)
	}
	if requestEvents[0].SourceMessageID != "msg-review-start" {
		t.Fatalf("expected review request to reuse session reply anchor, got %#v", requestEvents[0])
	}
	record := surface.PendingRequests["req-review-1"]
	if record == nil || record.SourceMessageID != "msg-review-start" || record.ThreadID != "thread-review" {
		t.Fatalf("expected pending request to stay bound to review session surface, got %#v", record)
	}
	if svc.turnSurface("inst-1", "thread-review", "turn-review-1") != surface {
		t.Fatalf("expected review thread turnSurface fallback to resolve surface")
	}
}

func TestReviewSessionLifecycleActivatesPendingSessionWithoutRemoteTurnOwnership(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		SourceMessageID: "msg-review-start",
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		ItemID:    "review-enter",
		ItemKind:  "entered_review_mode",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
		Metadata:  map[string]any{"review": "未提交变更"},
	})
	if len(events) != 0 {
		t.Fatalf("expected entered review lifecycle item to stay internal, got %#v", events)
	}
	session := surface.ReviewSession
	if session == nil || session.Phase != state.ReviewSessionPhaseActive {
		t.Fatalf("expected lifecycle item to activate pending review session, got %#v", session)
	}
	if session.ParentThreadID != "thread-main" || session.ReviewThreadID != "thread-review" || session.ActiveTurnID != "turn-review-1" {
		t.Fatalf("unexpected activated review session runtime: %#v", session)
	}
	if session.TargetLabel != "未提交变更" {
		t.Fatalf("expected review target label to persist, got %#v", session)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	replyEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "这里继续看一下",
	})
	if len(replyEvents) != 3 || replyEvents[2].Command == nil {
		t.Fatalf("expected review reply command after lifecycle activation, got %#v", replyEvents)
	}
	command := replyEvents[2].Command
	if command.Target.ThreadID != "thread-review" ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected review reply command target: %#v", command.Target)
	}
}

func TestStartReviewFromFinalCardBuildsDetachedReviewCommand(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	finalBlock := render.Block{
		Kind:       render.BlockAssistantMarkdown,
		InstanceID: "inst-1",
		ThreadID:   "thread-main",
		TurnID:     "turn-main-1",
		ItemID:     "item-main-1",
		Text:       "已经处理完成。",
		Final:      true,
	}
	svc.RecordFinalCardMessage(surface.SurfaceSessionID, finalBlock, "msg-user-1", "om-final-1", "life-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewStart,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-final-1",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + review start command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandReviewStart {
		t.Fatalf("expected review start command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" {
		t.Fatalf("unexpected review start target: %#v", command.Target)
	}
	if command.Review.Delivery != agentproto.ReviewDeliveryDetached || command.Review.Target.Kind != agentproto.ReviewTargetKindUncommittedChanges {
		t.Fatalf("unexpected review request: %#v", command.Review)
	}
	if surface.ReviewSession == nil || surface.ReviewSession.Phase != state.ReviewSessionPhasePending || surface.ReviewSession.ParentThreadID != "thread-main" || surface.ReviewSession.SourceMessageID != "om-final-1" {
		t.Fatalf("unexpected pending review session: %#v", surface.ReviewSession)
	}
}

func TestApplyReviewSessionResultBuildsParentPromptAndClearsSession(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "建议先补一条 review 回归测试。",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewApply,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	if len(events) != 2 {
		t.Fatalf("expected notice + prompt send command, got %#v", events)
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandPromptSend {
		t.Fatalf("expected prompt send command, got %#v", events)
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-main" || command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting || command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected apply-review target: %#v", command.Target)
	}
	if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != reviewApplyPromptPrefix+"建议先补一条 review 回归测试。" {
		t.Fatalf("unexpected apply-review prompt: %#v", command.Prompt)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected review session to clear after apply, got %#v", surface.ReviewSession)
	}
}

func TestDiscardReviewSessionClearsRuntime(t *testing.T) {
	svc, surface := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseActive,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewDiscard,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "review_discarded" {
		t.Fatalf("expected discard notice, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected review session to clear, got %#v", surface.ReviewSession)
	}
}
