package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestReviewSessionTextWithoutExplicitFollowUpIsBlocked(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	surface.ReviewSession.LastReviewText = "初始审阅意见"
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

	if noticeCode(events, "review_follow_up_not_requested") == "" {
		t.Fatalf("expected ready review to require an explicit follow-up action, got %#v", events)
	}
	if hasAgentCommand(events) || surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("ordinary review text must not dispatch or queue work, events=%#v surface=%#v", events, surface)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected blocked review text to keep parent selection, got %q", surface.SelectedThreadID)
	}
}

func TestReviewSessionExplicitFollowUpCapturesExactlyOneText(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	surface.ReviewSession.LastReviewText = "初始审阅意见"
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})

	begin := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	if noticeCode(begin, "review_follow_up_waiting_text") == "" {
		t.Fatalf("expected explicit follow-up action to enter text capture, got %#v", begin)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-2",
		Text:             "这里需要再看一下边界情况",
	})
	var command *agentproto.Command
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = event.Command
			break
		}
	}
	if command == nil {
		t.Fatalf("expected captured review follow-up to dispatch, got %#v", events)
	}
	if command.Target.ThreadID != "thread-review" ||
		command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting ||
		command.Target.SourceThreadID != "thread-main" ||
		command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("unexpected review follow-up target: %#v", command.Target)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected review follow-up to keep parent selection, got %q", surface.SelectedThreadID)
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-3",
		Text:             "这条不能继续静默投给审阅线程",
	})
	if hasAgentCommand(second) {
		t.Fatalf("expected one-shot follow-up capture to be consumed, got %#v", second)
	}
}

func TestReviewSessionExplicitFollowUpRejectsNonTextInputWithoutStaging(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	activateReviewSessionForTest(t, svc, surface, "msg-review-start", "turn-review-1")
	surface.ReviewSession.LastReviewText = "初始审阅意见"
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-review",
		TurnID:    "turn-review-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surface.SurfaceSessionID},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})

	imageEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-image",
		LocalPath:        "/tmp/review.png",
		MIMEType:         "image/png",
	})
	if noticeCode(imageEvents, "review_follow_up_waiting_text") == "" || len(surface.StagedImages) != 0 {
		t.Fatalf("expected image input to remain unstaged while review awaits text, events=%#v images=%#v", imageEvents, surface.StagedImages)
	}

	fileEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionFileMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-review-file",
		LocalPath:        "/tmp/review.txt",
		FileName:         "review.txt",
	})
	if noticeCode(fileEvents, "review_follow_up_waiting_text") == "" || len(surface.StagedFiles) != 0 {
		t.Fatalf("expected file input to remain unstaged while review awaits text, events=%#v files=%#v", fileEvents, surface.StagedFiles)
	}
}

func TestReviewSessionFollowUpDefersToActiveRequestCapture(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		LastReviewText: "初始审阅意见",
	}
	surface.ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "req-review-1"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-1",
	})
	if noticeCode(events, "request_capture_waiting_text") == "" {
		t.Fatalf("expected active request capture to keep input ownership, got %#v", events)
	}
	if surface.ReviewSession.AwaitingFollowUpText {
		t.Fatal("review follow-up must not open a second text capture")
	}
}

func TestStopCancelsAwaitingReviewFollowUpText(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:                state.ReviewSessionPhaseReady,
		ParentThreadID:       "thread-main",
		ReviewThreadID:       "thread-review",
		LastReviewText:       "初始审阅意见",
		AwaitingFollowUpText: true,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: surface.SurfaceSessionID,
	})
	if noticeCode(events, "review_follow_up_cancelled") == "" {
		t.Fatalf("expected /stop to cancel awaiting review follow-up, got %#v", events)
	}
	if surface.ReviewSession.AwaitingFollowUpText {
		t.Fatal("expected /stop to clear review follow-up capture")
	}

	textEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-after-stop",
		Text:             "这条消息不能再自动进入审阅线程",
	})
	if noticeCode(textEvents, "review_follow_up_not_requested") == "" || hasAgentCommand(textEvents) {
		t.Fatalf("expected text after stopped capture to remain blocked, got %#v", textEvents)
	}
}

func TestReviewSessionBackendMismatchFailsClosedAndClearsIdleOverlay(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		LastReviewText: "旧 Codex 审阅意见",
	}
	svc.MaterializeSurfaceResume(surface.SurfaceSessionID, surface.GatewayID, surface.ChatID, surface.ActorUserID, state.ProductModeNormal, agentproto.BackendClaude, "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "msg-claude",
		Text:             "这条消息不能落到残留的审阅线程",
	})
	if noticeCode(events, "review_backend_mismatch") == "" || hasAgentCommand(events) {
		t.Fatalf("expected backend-mismatched review overlay to fail closed, got %#v", events)
	}
	if surface.ReviewSession != nil {
		t.Fatalf("expected idle mismatched review overlay to clear, got %#v", surface.ReviewSession)
	}
}

func TestReviewSessionActionsRejectOlderResultCard(t *testing.T) {
	svc, surface, _ := newReviewSessionService(t)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:          state.ReviewSessionPhaseReady,
		ParentThreadID: "thread-main",
		ReviewThreadID: "thread-review",
		ThreadCWD:      "/data/dl/droid",
		LastReviewText: "初始审阅意见",
	}
	first := render.Block{
		InstanceID: "inst-1",
		ThreadID:   "thread-review",
		TurnID:     "turn-review-1",
		ItemID:     "item-review-1",
		Final:      true,
	}
	second := first
	second.TurnID = "turn-review-2"
	second.ItemID = "item-review-2"
	svc.RecordFinalCardMessage(surface.SurfaceSessionID, first, "msg-review-1", "om-review-final-1", "life-1")
	svc.RecordFinalCardMessage(surface.SurfaceSessionID, second, "msg-review-2", "om-review-final-2", "life-1")

	for _, kind := range []control.ActionKind{
		control.ActionReviewFollowUp,
		control.ActionReviewDiscard,
		control.ActionReviewApply,
	} {
		events := svc.ApplySurfaceAction(control.Action{
			Kind:             kind,
			SurfaceSessionID: surface.SurfaceSessionID,
			MessageID:        "om-review-final-1",
			Inbound: &control.ActionInboundMeta{
				CardDaemonLifecycleID: "life-1",
			},
		})
		if noticeCode(events, "review_action_card_expired") == "" || hasAgentCommand(events) {
			t.Fatalf("expected stale %q action to fail closed, got %#v", kind, events)
		}
		if surface.ReviewSession == nil {
			t.Fatalf("stale %q action must preserve the current review session", kind)
		}
	}
	current := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionReviewFollowUp,
		SurfaceSessionID: surface.SurfaceSessionID,
		MessageID:        "om-review-final-2",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "life-1",
		},
	})
	if noticeCode(current, "review_follow_up_waiting_text") == "" {
		t.Fatalf("expected latest review result card to remain actionable, got %#v", current)
	}
}
