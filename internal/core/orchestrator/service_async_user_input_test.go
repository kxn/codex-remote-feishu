package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func asyncQuestionService(t *testing.T) (*Service, *state.SurfaceConsoleRecord) {
	t.Helper()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1", Online: true, WorkspaceRoot: "/tmp/project",
		Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: "/tmp/project", Loaded: true}},
	})
	materializeVSCodeSurfaceForTest(svc, "surface-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.SelectedThreadID = "thread-1"
	surface.RouteMode = state.RouteModePinned
	svc.claimThread(surface, svc.root.Instances["inst-1"], "thread-1")
	startRemoteTurnForAutoWhipTest(t, svc, "initial-message", "请继续", "turn-1")
	return svc, surface
}

func asyncQuestionEvent(itemID string) agentproto.Event {
	return agentproto.Event{
		Kind: agentproto.EventItemCompleted, ThreadID: "thread-1", TurnID: "turn-1", ItemID: itemID, ItemKind: "agent_message",
		Metadata: map[string]any{"text": "允许切换吗？\n允许全部空闲后切换\n先保留候选", "delivery": "async", "questions": []any{map[string]any{"title": "允许切换吗？", "options": []any{"允许全部空闲后切换", "先保留候选"}}}},
	}
}

func asyncPage(t *testing.T, events []eventcontract.Event) *control.FeishuPageView {
	t.Helper()
	for _, event := range events {
		if event.PageView != nil {
			return event.PageView
		}
	}
	t.Fatalf("expected async question page, got %#v", events)
	return nil
}

func asyncAnswerAction(page *control.FeishuPageView, option int) control.Action {
	payload := page.Sections[0].Entries[0].Buttons[option].CallbackValue
	return control.Action{
		Kind: control.ActionKind("surface.message.async_user_input"), SurfaceSessionID: "surface-1", ActorUserID: "user-1", MessageID: "om-question",
		Inbound: &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "test-daemon", LifecycleVerdict: control.InboundLifecycleCurrent},
		Request: &control.ActionRequestResponse{RequestID: payload["request_id"].(string), RequestRevision: payload["request_revision"].(int), RequestOptionID: payload["option_id"].(string)},
	}
}

func TestAsyncUserInputAnswerSteersOnlyItsOwnQueueItem(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	if len(surface.PendingRequests) != 0 {
		t.Fatal("async questions must not create a synchronous input gate")
	}
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", ActorUserID: "user-1", MessageID: "ordinary", Text: "继续正常聊天"})
	if len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("ordinary input was intercepted: %#v", surface.QueuedQueueItemIDs)
	}
	events := svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
	var steer *agentproto.Command
	for _, event := range events {
		if event.Command != nil {
			steer = event.Command
		}
	}
	if steer == nil || steer.Kind != agentproto.CommandTurnSteer || steer.Target.ThreadID != "thread-1" || steer.Target.TurnID != "turn-1" {
		t.Fatalf("expected answer to steer original turn: %#v", events)
	}
	text := steer.Prompt.Inputs[0].Text
	if !strings.Contains(text, "允许切换吗？") || !strings.Contains(text, "允许全部空闲后切换") || strings.Contains(text, "继续正常聊天") {
		t.Fatalf("wrong answer contents: %q", text)
	}
	if len(surface.QueuedQueueItemIDs) != 1 || surface.QueueItems[surface.QueuedQueueItemIDs[0]].SourceMessageID != "ordinary" {
		t.Fatal("answer must not steer unrelated queued input")
	}
	if page := asyncPage(t, events); !page.Sealed {
		t.Fatal("answered question must be sealed")
	}
	if again := svc.ApplySurfaceAction(asyncAnswerAction(page, 0)); containsAsyncCommand(again) {
		t.Fatalf("duplicate answer dispatched: %#v", again)
	}
}

func containsAsyncCommand(events []eventcontract.Event) bool {
	for _, event := range events {
		if event.Command != nil {
			return true
		}
	}
	return false
}

func TestAsyncUserInputCanAnswerAfterTurnCompletes(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed"})
	events := svc.ApplySurfaceAction(asyncAnswerAction(page, 1))
	command := firstCodexCommand(events, agentproto.CommandPromptSend)
	if command == nil || command.Target.ThreadID != "thread-1" || command.Target.CreateThreadIfMissing {
		t.Fatalf("late answer must resume original thread: %#v", events)
	}
	if surface.SelectedThreadID != "thread-1" || len(surface.PendingRequests) != 0 {
		t.Fatal("late answer changed route or created a pending RPC")
	}
}

func TestAsyncUserInputRejectsStaleOrMisdirectedAnswers(t *testing.T) {
	for _, name := range []string{"actor", "thread", "instance", "revision", "option", "missing", "new_thread"} {
		t.Run(name, func(t *testing.T) {
			svc, surface := asyncQuestionService(t)
			page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
			action := asyncAnswerAction(page, 0)
			switch name {
			case "actor":
				action.ActorUserID = "someone-else"
			case "thread":
				surface.SelectedThreadID = "thread-2"
			case "instance":
				surface.AttachedInstanceID = "inst-2"
			case "revision":
				action.Request.RequestRevision++
			case "option":
				action.Request.RequestOptionID = "50"
			case "missing":
				action.Request.RequestID = "missing"
			case "new_thread":
				surface.RouteMode = state.RouteModeNewThreadReady
			}
			before := len(surface.QueueItems)
			events := svc.ApplySurfaceAction(action)
			if containsAsyncCommand(events) || len(surface.QueueItems) != before {
				t.Fatalf("invalid answer dispatched: %#v", events)
			}
		})
	}
}

func TestAsyncUserInputMultipleQuestionsAndIndependentCards(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	event := asyncQuestionEvent("item-1")
	event.Metadata["questions"] = []any{
		map[string]any{"title": "是否切换？", "options": []any{"允许", "拒绝"}},
		map[string]any{"title": "补充要求"},
	}
	first := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	other := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-2")))
	before := len(surface.QueueItems)
	action := asyncAnswerAction(first, 0)
	second := asyncPage(t, svc.ApplySurfaceAction(action))
	if len(surface.QueueItems) != before || len(second.Sections[0].Entries[0].Buttons) != 0 {
		t.Fatal("first answer should only advance to free text question")
	}
	if containsAsyncCommand(svc.ApplySurfaceAction(action)) {
		t.Fatal("old question revision must not answer next question")
	}
	payload := second.Sections[0].Entries[0].Form.SubmitValue
	action.Request = &control.ActionRequestResponse{RequestID: payload["request_id"].(string), RequestRevision: payload["request_revision"].(int), Answers: map[string][]string{"answer": {"等所有实例空闲"}}}
	command := firstCodexCommand(svc.ApplySurfaceAction(action), agentproto.CommandTurnSteer)
	if command == nil || !strings.Contains(command.Prompt.Inputs[0].Text, "补充要求\n回答：等所有实例空闲") || !strings.Contains(command.Prompt.Inputs[0].Text, "是否切换？\n回答：允许") {
		t.Fatalf("expected one aggregated answer: %#v", command)
	}
	// Another question stays independently answerable while the first steer is pending.
	events := svc.ApplySurfaceAction(asyncAnswerAction(other, 1))
	if !asyncPage(t, events).Sealed || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("second card answer must queue separately: %#v", events)
	}
}

func TestAsyncUserInputLeavesStagedAttachmentsUntouched(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	surface.StagedFiles["file-1"] = &state.StagedFileRecord{FileID: "file-1", ActorUserID: "user-1", State: state.FileStaged, LocalPath: "/tmp/reference.txt"}
	events := svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
	if !containsAsyncCommand(events) || surface.StagedFiles["file-1"].State != state.FileStaged {
		t.Fatalf("answer must not consume staged files: %#v", events)
	}
}

func TestAsyncUserInputMalformedAndOrdinaryMessagesKeepTextFallback(t *testing.T) {
	for _, metadata := range []map[string]any{
		{"text": "普通消息"},
		{"text": "问题文字", "delivery": "async", "questions": "unknown-shape"},
		{"text": "问题文字", "delivery": "async", "questions": []any{map[string]any{"title": "问题", "options": []any{map[string]any{"label": "未知格式"}}}}},
	} {
		svc, _ := asyncQuestionService(t)
		event := asyncQuestionEvent("item-1")
		event.Metadata = metadata
		svc.ApplyAgentEvent("inst-1", event)
		pending := svc.progress.takePendingTurnText("inst-1", "thread-1", "turn-1")
		if pending == nil || pending.Text != metadata["text"] {
			t.Fatalf("text fallback lost: %#v", pending)
		}
	}
}

func TestAsyncUserInputDuplicateItemDoesNotReopenAnsweredCard(t *testing.T) {
	svc, _ := asyncQuestionService(t)
	event := asyncQuestionEvent("item-1")
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
	if events := svc.ApplyAgentEvent("inst-1", event); len(events) != 0 {
		t.Fatalf("replayed item must not reopen answered question: %#v", events)
	}
}

func TestAsyncUserInputRespectsExistingDispatchAndRequestGates(t *testing.T) {
	for _, name := range []string{"paused_for_local", "handoff_wait", "sync_request", "request_capture"} {
		t.Run(name, func(t *testing.T) {
			svc, surface := asyncQuestionService(t)
			page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
			switch name {
			case "paused_for_local":
				surface.DispatchMode = state.DispatchModePausedForLocal
			case "handoff_wait":
				surface.DispatchMode = state.DispatchModeHandoffWait
			case "sync_request":
				svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "thread-1", TurnID: "turn-1", RequestID: "sync-1", Metadata: map[string]any{"requestType": "approval", "title": "先确认命令"}})
			case "request_capture":
				surface.ActiveRequestCapture = &state.RequestCaptureRecord{}
			}
			events := svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
			if containsAsyncCommand(events) {
				t.Fatalf("answer bypassed %s: %#v", name, events)
			}
			if name == "paused_for_local" || name == "handoff_wait" {
				if len(surface.QueuedQueueItemIDs) != 1 {
					t.Fatal("paused answer should remain queued")
				}
			} else if len(surface.QueuedQueueItemIDs) != 0 {
				t.Fatal("synchronous request must be handled before submitting async answer")
			}
		})
	}
}

func TestAsyncUserInputBindsOriginalActorWhenAnotherUserChecksStatus(t *testing.T) {
	svc, _ := asyncQuestionService(t)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ActorUserID: "user-2"})
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	if !containsAsyncCommand(svc.ApplySurfaceAction(asyncAnswerAction(page, 0))) {
		t.Fatal("original turn owner must remain able to answer after another user's status action")
	}
}

func TestAsyncUserInputSupportsFirstOutputOfNewThread(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-1", Online: true, WorkspaceRoot: "/tmp/project", ObservedFocusedThreadID: "thread-1", Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: "/tmp/project", Loaded: true}}})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionNewThread, SurfaceSessionID: "surface-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: "surface-1", ActorUserID: "user-1", MessageID: "new-message", Text: "新任务"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-new", TurnID: "turn-new", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"}})
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeNewThreadReady {
		t.Fatal("fixture must exercise the uncommitted bootstrap route")
	}
	event := asyncQuestionEvent("new-item")
	event.ThreadID, event.TurnID = "thread-new", "turn-new"
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	command := firstCodexCommand(svc.ApplySurfaceAction(asyncAnswerAction(page, 0)), agentproto.CommandTurnSteer)
	if command == nil || command.Target.ThreadID != "thread-new" || command.Target.TurnID != "turn-new" {
		t.Fatalf("first question answer did not reach the bootstrapped thread: %#v", command)
	}
}

func TestAsyncUserInputModelMismatchDoesNotCreateAnotherThread(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	svc.root.Instances["inst-1"].Threads["thread-1"].ExplicitModel = "deepseek-v4-flash"
	surface.PromptOverride.Model = "gpt-6.1-sol"
	before := len(surface.QueueItems)
	events := svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
	if containsAsyncCommand(events) || len(surface.QueueItems) != before || surface.SelectedThreadID != "thread-1" || surface.RouteMode != state.RouteModePinned {
		t.Fatal("incompatible model must reject answer without queueing or changing thread")
	}
}

func TestAsyncUserInputSteerRaceRestoresAnswerToOriginalThread(t *testing.T) {
	svc, surface := asyncQuestionService(t)
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	svc.ApplySurfaceAction(asyncAnswerAction(page, 0))
	svc.BindPendingRemoteCommand(surface.SurfaceSessionID, "async-steer-command")
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed"})
	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{CommandID: "async-steer-command", Accepted: false, Error: "turn is no longer active"})
	commands := 0
	for _, event := range append(completed, rejected...) {
		if event.Command == nil || event.Command.Kind != agentproto.CommandPromptSend {
			continue
		}
		commands++
		if event.Command.Target.ThreadID != "thread-1" || !strings.Contains(event.Command.Prompt.Inputs[0].Text, "允许全部空闲后切换") {
			t.Fatal("restored answer changed target or contents")
		}
	}
	if commands != 1 {
		t.Fatalf("expected exactly one resumed prompt after rejected steer, got %d", commands)
	}
}
