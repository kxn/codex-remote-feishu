package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAutoContinueCommandUpdatesSnapshotWithoutAttach(t *testing.T) {
	now := time.Date(2026, 4, 9, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	enabled := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAutoContinueCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/autocontinue on",
	})
	if len(enabled) != 1 || enabled[0].Notice == nil || enabled[0].Notice.Code != "autocontinue_enabled" {
		t.Fatalf("expected enable notice, got %#v", enabled)
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot after enable")
	}
	if !snapshot.AutoContinue.Enabled {
		t.Fatalf("expected autocontinue enabled in snapshot, got %#v", snapshot.AutoContinue)
	}

	disabled := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAutoContinueCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/autocontinue off",
	})
	if len(disabled) != 1 || disabled[0].Notice == nil || disabled[0].Notice.Code != "autocontinue_disabled" {
		t.Fatalf("expected disable notice, got %#v", disabled)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.AutoContinue.Enabled {
		t.Fatalf("expected autocontinue disabled in snapshot, got %#v", snapshot)
	}
}

func TestSurfaceSnapshotIncludesAutoContinueSummary(t *testing.T) {
	now := time.Date(2026, 4, 9, 11, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID: "surface-1",
		DispatchMode:     state.DispatchModeNormal,
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
		AutoContinue: state.AutoContinueRuntimeRecord{
			Enabled: true,
			Episode: &state.PendingAutoContinueEpisodeRecord{
				EpisodeID:                  "autocontinue-1",
				State:                      state.AutoContinueEpisodeScheduled,
				AttemptCount:               3,
				ConsecutiveDryFailureCount: 2,
				PendingDueAt:               now.Add(5 * time.Second),
				TriggerKind:                state.AutoContinueTriggerKindUpstreamRetryableFailure,
			},
		},
	}

	snapshot := svc.SurfaceSnapshot("surface-1")
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if !snapshot.AutoContinue.Enabled ||
		snapshot.AutoContinue.State != string(state.AutoContinueEpisodeScheduled) ||
		!snapshot.AutoContinue.PendingDueAt.Equal(now.Add(5*time.Second)) ||
		snapshot.AutoContinue.AttemptCount != 3 ||
		snapshot.AutoContinue.ConsecutiveDryFailureCount != 2 ||
		snapshot.AutoContinue.TriggerKind != string(state.AutoContinueTriggerKindUpstreamRetryableFailure) {
		t.Fatalf("unexpected autocontinue snapshot: %#v", snapshot.AutoContinue)
	}
}

func TestAutoContinueDispatchesRetryableFailureImmediately(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")
	events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "interrupted", "upstream stream closed", "", &agentproto.ErrorInfo{
		Code:      "responseStreamDisconnected",
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   "upstream stream closed",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Retryable: true,
	})

	if surface.AutoWhip.PendingReason != "" {
		t.Fatalf("expected retryable failure to stay out of autowhip runtime, got %#v", surface.AutoWhip)
	}
	episode := surface.AutoContinue.Episode
	if episode == nil {
		t.Fatal("expected autocontinue episode")
	}
	if episode.State != state.AutoContinueEpisodeRunning || episode.AttemptCount != 1 || episode.ConsecutiveDryFailureCount != 1 {
		t.Fatalf("expected immediate first autocontinue attempt, got %#v", episode)
	}
	if surface.ActiveQueueItemID == "" {
		t.Fatalf("expected immediate autocontinue dispatch to occupy active queue")
	}
	active := surface.QueueItems[surface.ActiveQueueItemID]
	if active == nil || active.SourceKind != state.QueueItemSourceAutoContinue || active.AutoContinueEpisodeID != episode.EpisodeID {
		t.Fatalf("expected autocontinue queue item to dispatch, got %#v", active)
	}
	var sawTurnFailedNotice bool
	var sawAutoContinueCard bool
	var sawAutoContinuePrompt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawTurnFailedNotice = true
		}
		if event.PageView != nil && strings.TrimSpace(event.PageView.TrackingKey) == episode.EpisodeID {
			sawAutoContinueCard = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
			sawAutoContinuePrompt = true
		}
	}
	if sawTurnFailedNotice {
		t.Fatalf("expected autocontinue path to suppress direct turn_failed notice, got %#v", events)
	}
	if !sawAutoContinueCard || !sawAutoContinuePrompt {
		t.Fatalf("expected autocontinue card plus prompt dispatch, got %#v", events)
	}
}

func TestAutoContinueDoesNotScheduleAfterUserStopEvenWithRetryableProblem(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")

	stopEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(stopEvents) == 0 {
		t.Fatal("expected stop events")
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected before completion",
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected before completion",
			Retryable: true,
		},
	})
	if episode := surface.AutoContinue.Episode; episode != nil {
		t.Fatalf("expected /stop to suppress autocontinue scheduling, got %#v", episode)
	}
	if active := surface.ActiveQueueItemID; active != "" {
		t.Fatalf("expected no autocontinue dispatch after /stop, got active %q", active)
	}
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			t.Fatalf("expected user stop not to emit failure notice, got %#v", events)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
			t.Fatalf("expected user stop not to trigger autocontinue prompt, got %#v", events)
		}
	}
}

func TestDetachedBranchAutoContinueKeepsSurfaceSelectionButRetriesExecutionThread(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 20, 0, 0, time.UTC)
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
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-main"})
	surface := svc.root.Surfaces["surface-1"]
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	startDetachedBranchRemoteTurnForTest(t, svc, surface, "thread-main", "thread-detour", "msg-1", "顺手问个岔题", "turn-detour")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-detour",
		TurnID:       "turn-detour",
		Status:       "interrupted",
		ErrorMessage: "upstream stream closed",
		Initiator:    agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "upstream stream closed",
			ThreadID:  "thread-detour",
			TurnID:    "turn-detour",
			Retryable: true,
		},
	})

	episode := surface.AutoContinue.Episode
	if episode == nil {
		t.Fatal("expected detached branch autocontinue episode")
	}
	if episode.State != state.AutoContinueEpisodeRunning {
		t.Fatalf("expected detached branch autocontinue to dispatch immediately, got %#v", episode)
	}
	if episode.ThreadID != "thread-detour" || episode.FrozenSourceThreadID != "thread-main" {
		t.Fatalf("expected detached branch autocontinue to keep execution+source split, got %#v", episode)
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected detached branch autocontinue not to steal current selection, got %q", surface.SelectedThreadID)
	}
	active := surface.QueueItems[surface.ActiveQueueItemID]
	if active == nil {
		t.Fatalf("expected detached branch autocontinue to create active queue item, got %#v", surface.QueueItems)
	}
	if active.FrozenThreadID != "thread-detour" || active.FrozenSourceThreadID != "thread-main" || active.FrozenSurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("expected detached branch autocontinue queue item to keep routing split, got %#v", active)
	}
	var sawPrompt bool
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			sawPrompt = true
			if event.Command.Target.ThreadID != "thread-detour" || event.Command.Target.SourceThreadID != "thread-main" || event.Command.Target.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
				t.Fatalf("expected detached branch autocontinue prompt to retry on execution thread without rebinding surface, got %#v", event.Command.Target)
			}
		}
	}
	if !sawPrompt {
		t.Fatalf("expected detached branch autocontinue prompt dispatch, got %#v", events)
	}
}

func TestDetachClearsPendingAutoContinueEpisode(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 18, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	surface.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{
		EpisodeID:       "autocontinue-1",
		InstanceID:      "inst-1",
		ThreadID:        "thread-1",
		FrozenCWD:       "/data/dl/droid",
		FrozenRouteMode: state.RouteModePinned,
		State:           state.AutoContinueEpisodeScheduled,
		PendingDueAt:    now.Add(5 * time.Second),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) == 0 {
		t.Fatal("expected detach events")
	}
	if !surface.AutoContinue.Enabled || surface.AutoContinue.Episode != nil {
		t.Fatalf("expected detach to preserve autocontinue toggle but clear pending episode, got %#v", surface.AutoContinue)
	}
}

func TestNewThreadReadyClearsPendingAutoContinueEpisode(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 19, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	surface.AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{
		EpisodeID:       "autocontinue-1",
		InstanceID:      "inst-1",
		ThreadID:        "thread-1",
		FrozenCWD:       "/data/dl/droid",
		FrozenRouteMode: state.RouteModePinned,
		State:           state.AutoContinueEpisodeScheduled,
		PendingDueAt:    now.Add(5 * time.Second),
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionNewThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) == 0 {
		t.Fatal("expected /new events")
	}
	if !surface.AutoContinue.Enabled || surface.AutoContinue.Episode != nil {
		t.Fatalf("expected /new to preserve autocontinue toggle but clear pending episode, got %#v", surface.AutoContinue)
	}
}

func TestAutoContinuePrioritizesQueuedUserInput(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-2",
		Text:             "后面的补充消息",
	})
	if len(queued) == 0 {
		t.Fatal("expected queued user input events")
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected before completion",
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected before completion",
			Retryable: true,
		},
	})
	active := surface.QueueItems[surface.ActiveQueueItemID]
	if active == nil || active.SourceKind != state.QueueItemSourceAutoContinue {
		t.Fatalf("expected autocontinue to dispatch before queued user input, got active=%#v queued=%#v", active, surface.QueuedQueueItemIDs)
	}
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("expected original queued user input to remain queued, got %#v", surface.QueuedQueueItemIDs)
	}
	queuedItem := surface.QueueItems[surface.QueuedQueueItemIDs[0]]
	if queuedItem == nil || queuedItem.SourceKind != state.QueueItemSourceUser || queuedItem.SourceMessageID != "msg-2" {
		t.Fatalf("expected queued user item to remain intact, got %#v", queuedItem)
	}
	var sawAutoContinuePrompt bool
	for _, event := range events {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
			sawAutoContinuePrompt = true
		}
	}
	if !sawAutoContinuePrompt {
		t.Fatalf("expected autocontinue dispatch command, got %#v", events)
	}
}

func TestAutoContinueOutputResetsDryFailureBackoff(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected before completion",
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected before completion",
			Retryable: true,
		},
	})
	if len(first) == 0 {
		t.Fatal("expected first autocontinue dispatch")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-2",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if delta := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-2",
		ItemID:   "item-2",
		Delta:    "先输出一点内容",
	}); len(delta) != 0 {
		t.Fatalf("expected delta to stay buffered, got %#v", delta)
	}
	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:         agentproto.EventTurnCompleted,
		ThreadID:     "thread-1",
		TurnID:       "turn-2",
		Status:       "interrupted",
		ErrorMessage: "stream disconnected again",
		Problem: &agentproto.ErrorInfo{
			Code:      "responseStreamDisconnected",
			Layer:     "codex",
			Stage:     "runtime_error",
			Message:   "stream disconnected again",
			Retryable: true,
		},
	})
	episode := surface.AutoContinue.Episode
	if episode == nil {
		t.Fatal("expected autocontinue episode after second failure")
	}
	if episode.State != state.AutoContinueEpisodeRunning || episode.AttemptCount != 2 || episode.ConsecutiveDryFailureCount != 1 {
		t.Fatalf("expected output to reset dry failure backoff before next retry, got %#v", episode)
	}
	var sawAutoContinuePrompt bool
	for _, event := range second {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
			sawAutoContinuePrompt = true
		}
	}
	if !sawAutoContinuePrompt {
		t.Fatalf("expected second autocontinue attempt to dispatch immediately after outputful failure, got %#v", second)
	}
}

func TestAutoContinueStatusCardStopsPatchingAfterTailMoves(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")

	_ = completeRemoteTurnWithFinalText(t, svc, "turn-1", "interrupted", "upstream stream closed", "", &agentproto.ErrorInfo{
		Code:      "responseStreamDisconnected",
		Layer:     "codex",
		Stage:     "runtime_error",
		Message:   "upstream stream closed",
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Retryable: true,
	})
	episode := surface.AutoContinue.Episode
	if episode == nil {
		t.Fatal("expected autocontinue episode")
	}

	svc.RecordSurfaceOutboundMessage(surface.SurfaceSessionID, "om-autocontinue-1", state.SurfaceMessageKindCard, "msg-1")
	svc.RecordPageTrackingMessage(surface.SurfaceSessionID, episode.EpisodeID, "om-autocontinue-1")
	if got := autoContinueStatusMessageID(surface, episode); got != "om-autocontinue-1" {
		t.Fatalf("expected tail autocontinue card to remain patchable, got %q", got)
	}

	svc.RecordSurfaceOutboundMessage(surface.SurfaceSessionID, "om-next-1", state.SurfaceMessageKindText, "msg-1")
	event := svc.autoContinueStatusCardEvent(surface, episode)
	if event.PageView == nil {
		t.Fatalf("expected autocontinue status page event, got %#v", event)
	}
	if event.PageView.MessageID != "" {
		t.Fatalf("expected autocontinue status card to stop patching after tail moves, got %#v", event.PageView)
	}
}

func TestStartupFailureDoesNotEnterAutoContinue(t *testing.T) {
	now := time.Date(2026, 4, 9, 12, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.AutoWhip.Enabled = false
	surface.AutoContinue.Enabled = true

	queued := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "继续处理",
	})
	if len(queued) == 0 {
		t.Fatal("expected initial dispatch")
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:                 agentproto.EventTurnCompleted,
		ThreadID:             "thread-1",
		Status:               "failed",
		ErrorMessage:         "thread resume rejected",
		TurnCompletionOrigin: agentproto.TurnCompletionOriginThreadResumeRejected,
		Problem: &agentproto.ErrorInfo{
			Code:      "thread_resume_rejected",
			Layer:     "codex",
			Stage:     "thread_resume",
			Message:   "thread resume rejected",
			Retryable: true,
		},
	})
	if episode := surface.AutoContinue.Episode; episode != nil {
		t.Fatalf("expected startup failure to avoid autocontinue lane, got %#v", episode)
	}
	item := surface.QueueItems["queue-1"]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected startup failure to fail queue item, got %#v", item)
	}
	var sawTurnFailed bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "turn_failed" {
			sawTurnFailed = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
			t.Fatalf("expected startup failure not to dispatch autocontinue prompt, got %#v", events)
		}
	}
	if !sawTurnFailed {
		t.Fatalf("expected startup failure to emit explicit failure notice, got %#v", events)
	}
}
