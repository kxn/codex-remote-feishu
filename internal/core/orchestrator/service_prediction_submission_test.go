package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPredictionQueuedReceiptSurvivesActivityBeforeCommandBinding(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	surface := svc.root.Surfaces[sid]
	surface.DispatchMode = state.DispatchModePausedForLocal
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "排队后实际发送的正文")
	svc.ApplySurfaceAction(action)
	svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, ThreadID: "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeActive}})
	svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventThreadRuntimeStatusUpdated, ThreadID: "thread-1",
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeIdle}})
	if findAgentCommand(svc.ResumeSurfaceDispatch(sid, nil), agentproto.CommandPromptSend) == nil {
		t.Fatal("queued submission did not dispatch after resume")
	}
	svc.BindPendingRemoteCommand(sid, "queued-draft-command")
	events := svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}})
	page := findPageEvent(events)
	if page == nil || page.MessageID != action.MessageID || page.StatusText != "已发送，任务已开始处理。" || len(page.Sections) != 0 {
		t.Fatal("the submitted queue lost its read-only receipt when suggestion freshness expired")
	}
}

func TestPredictionReceiptRequiresItsOwnNewTurn(t *testing.T) {
	for _, kind := range []string{"source-turn-replay", "other-command", "other-surface", "local-turn", "other-thread"} {
		t.Run(kind, func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
			svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
			action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "原始编辑内容")
			svc.ApplySurfaceAction(action)
			svc.BindPendingRemoteCommand(sid, "draft-command")
			event := agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2",
				Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}}
			switch kind {
			case "source-turn-replay":
				event.TurnID = "turn-1"
			case "other-command":
				event.CommandID = "other-command"
			case "other-surface":
				event.Initiator.SurfaceSessionID = "other-surface"
			case "local-turn":
				event.Initiator.Kind = agentproto.InitiatorLocalUI
			case "other-thread":
				event.ThreadID = "other-thread"
			}
			for _, event := range svc.ApplyAgentEvent(iid, event) {
				if event.PageView != nil && event.PageView.MessageID == action.MessageID {
					t.Fatal("unrelated or replayed turn changed this submission receipt")
				}
			}
		})
	}
}

func TestPredictionConfirmedReceiptCannotRestoreEditorOnLateRejection(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议内容"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "已开始执行的原文")
	svc.ApplySurfaceAction(action)
	svc.BindPendingRemoteCommand(sid, "draft-command")
	started := svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}})
	if page := findPageEvent(started); page == nil || page.MessageID != action.MessageID || len(page.Sections) != 0 {
		t.Fatal("confirmed start did not seal the original card")
	}
	for _, event := range svc.HandleCommandRejected(iid, agentproto.CommandAck{CommandID: "draft-command"}) {
		if event.PageView != nil && len(event.PageView.Sections) != 0 {
			t.Fatal("late rejection restored a draft whose task already started")
		}
	}
	if findAgentCommand(svc.ApplySurfaceAction(control.Action{Kind: predictionSendKind, SurfaceSessionID: sid,
		ChatID: "chat-1", ActorUserID: "user-1", MessageID: action.MessageID, PredictionID: request.CommandID, Text: action.Text}), agentproto.CommandPromptSend) != nil {
		t.Fatal("late rejection allowed the old card to send again")
	}
}

func TestPredictionSendKeepsOriginalThreadAndLiteralText(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	text := "请继续 " + detourTriggerBlankText
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", text)
	command := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend)
	if command == nil || command.Target.ThreadID != "thread-1" || command.Target.ExecutionMode != agentproto.PromptExecutionModeResumeExisting {
		t.Fatalf("expected dispatch only to original thread, got %#v", command)
	}
	if len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != text {
		t.Fatalf("expected literal text %q, got %#v", text, command.Prompt.Inputs)
	}
}

func TestPredictionSendRejectsIncompatibleModelGroupWithoutRetargeting(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议"))
	svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	svc.root.Instances[iid].Threads["thread-1"].ExplicitModel = "gpt-5.5"
	surface := svc.root.Surfaces[sid]
	surface.CodexThreadPolicy = &state.CodexThreadPolicy{
		ThreadPolicyID: "policy-other-model", ModelMode: state.CodexThreadValueExplicit, Model: "deepseek-v4-flash",
		ReviewModelMode: state.CodexReviewModelConfig,
		ReasoningMode:   state.CodexThreadValueDefault, ContextMode: state.CodexContextModeDefault,
	}
	action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "原来的正文")
	events := svc.ApplySurfaceAction(action)
	if findAgentCommand(events, agentproto.CommandPromptSend) != nil || surface.SelectedThreadID != "thread-1" {
		t.Fatal("incompatible model group redirected the submission")
	}
	if page := findPageEvent(events); page == nil || len(page.Sections) == 0 || page.StatusText == "确认内容后点击发送。" {
		t.Fatalf("expected preserved editor with a model conflict explanation, got %#v", page)
	}
}

func TestPredictionNativeStartupFailureRestoresEditor(t *testing.T) {
	for _, origin := range []agentproto.TurnCompletionOrigin{
		agentproto.TurnCompletionOriginTurnStartRejected,
		agentproto.TurnCompletionOriginThreadResumeRejected,
	} {
		t.Run(string(origin), func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "建议"))
			svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
			action := predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "编辑后的正文")
			if findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend) == nil {
				t.Fatal("fixture did not dispatch")
			}
			svc.BindPendingRemoteCommand(sid, "draft-command")
			events := svc.ApplyAgentEvent(iid, agentproto.Event{
				Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", Status: "failed",
				ErrorMessage: "native rejection", TurnCompletionOrigin: origin,
			})
			page := findPageEvent(events)
			if page == nil || page.MessageID != action.MessageID || len(page.Sections) == 0 {
				t.Fatal("native startup rejection did not restore the editor on the original card")
			}
			if field := page.Sections[0].Entries[0].Form.Field; field.DefaultValue != action.Text {
				t.Fatalf("restored draft changed edited text to %q", field.DefaultValue)
			}
			if findAgentCommand(events, agentproto.CommandPromptSend) != nil {
				t.Fatal("startup rejection retried automatically")
			}
			if retry := findAgentCommand(svc.ApplySurfaceAction(action), agentproto.CommandPromptSend); retry == nil || retry.Target.ThreadID != "thread-1" {
				t.Fatalf("expected explicit retry in original thread, got %#v", retry)
			}
		})
	}
}
