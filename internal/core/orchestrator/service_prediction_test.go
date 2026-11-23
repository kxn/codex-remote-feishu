package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

const predictionRequestKind agentproto.CommandKind = "thread.prediction.request"
const predictionCancelKind agentproto.CommandKind = "thread.prediction.cancel"
const predictionAcceptKind control.ActionKind = "surface.prediction.accept"
const predictionSendKind control.ActionKind = "surface.prediction.send"

func predictionTestSetup(t *testing.T) (*Service, string, string) {
	t.Helper()
	svc, surfaceID, instanceID := goalInterlockTestSetup(t)
	inst := svc.root.Instances[instanceID]
	inst.Threads["thread-1"].ThreadGoal = nil
	inst.CapabilitiesDeclared = true
	if err := json.Unmarshal([]byte(`{"threadPrediction":true}`), &inst.Capabilities); err != nil {
		t.Fatal(err)
	}
	return svc, surfaceID, instanceID
}

func predictionCompleteTurn(t *testing.T, svc *Service, surfaceID, instanceID string) []eventcontract.Event {
	t.Helper()
	input := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID,
		ChatID: "chat-1", ActorUserID: "user-1", MessageID: "om-input", Text: "检查一下实现"})
	if findAgentCommand(input, agentproto.CommandPromptSend) == nil {
		t.Fatalf("expected normal prompt before prediction: %#v", input)
	}
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: surfaceID}
	svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Initiator: initiator})
	svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "thread-1", TurnID: "turn-1", ItemID: "answer", ItemKind: "agent_message", Metadata: map[string]any{"text": "检查完成，测试已通过。"}})
	return svc.ApplyAgentEvent(instanceID, agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed", Initiator: initiator})
}

func predictionTestResult(t *testing.T, commandID, sourceTurnID, suggestion string) agentproto.Event {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"kind": "thread.prediction.result", "commandId": commandID, "threadId": "thread-1",
		"prediction": map[string]any{"sourceTurnId": sourceTurnID, "suggestion": suggestion}})
	if err != nil {
		t.Fatal(err)
	}
	var event agentproto.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func predictionTestAction(t *testing.T, kind control.ActionKind, surfaceID, predictionID, actor, text string) control.Action {
	t.Helper()
	action := control.Action{Kind: kind, SurfaceSessionID: surfaceID, ChatID: "chat-1", ActorUserID: actor, MessageID: "om-prediction", Text: text,
		LocalPageAction: true, Inbound: &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: "lifecycle-1"}}
	// Populate the additive action carrier without relying on a pre-existing type.
	raw, _ := json.Marshal(map[string]any{"PredictionID": predictionID})
	if err := json.Unmarshal(raw, &action); err != nil {
		t.Fatal(err)
	}
	return action
}

func TestPredictionCompletesAsEditableDraftAndRequiresExplicitSend(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	completed := predictionCompleteTurn(t, svc, sid, iid)
	request := findAgentCommand(completed, predictionRequestKind)
	if request == nil {
		t.Fatal("successful remote reply must request one prediction")
	}
	result := svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "请再检查一下边界情况"))
	if findPageEvent(result) == nil || findAgentCommand(result, agentproto.CommandPromptSend) != nil {
		t.Fatal("prediction must show a card without sending a turn")
	}
	draft := svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, "user-1", ""))
	page := findPageEvent(draft)
	if page == nil || len(page.Sections) == 0 || page.Sections[0].Entries[0].Form == nil {
		t.Fatal("accept must open an editable form")
	}
	if page.Sections[0].Entries[0].Form.Field.DefaultValue != "请再检查一下边界情况" {
		t.Fatal("draft must contain the suggestion")
	}
	if findAgentCommand(draft, agentproto.CommandPromptSend) != nil {
		t.Fatal("accepting is not sending")
	}
	sent := svc.ApplySurfaceAction(predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "请补充两个边界测试"))
	command := findAgentCommand(sent, agentproto.CommandPromptSend)
	if command == nil || len(command.Prompt.Inputs) != 1 || command.Prompt.Inputs[0].Text != "请补充两个边界测试" {
		t.Fatalf("explicit submit must send edited input: %#v", sent)
	}
	repeat := svc.ApplySurfaceAction(predictionTestAction(t, predictionSendKind, sid, request.CommandID, "user-1", "重复"))
	if findAgentCommand(repeat, agentproto.CommandPromptSend) != nil {
		t.Fatal("draft submit must only be consumed once")
	}
}

func TestPredictionRejectsWrongActorTargetAndLateResult(t *testing.T) {
	for _, stale := range []string{"actor", "thread", "instance", "turn", "input", "disconnect", "timeout", "surface"} {
		t.Run(stale, func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			if request == nil {
				t.Fatal("missing prediction request")
			}
			actor := "user-1"
			switch stale {
			case "actor":
				actor = "other-user"
			case "thread":
				svc.root.Surfaces[sid].SelectedThreadID = "other-thread"
			case "instance":
				svc.root.Surfaces[sid].AttachedInstanceID = "other-instance"
			case "turn":
				svc.ApplyAgentEvent(iid, agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "new-turn"})
			case "input":
				svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ActorUserID: actor, Text: "我自己输入"})
			case "disconnect":
				svc.ApplyInstanceDisconnected(iid)
			case "timeout":
				svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", ""))
			case "surface":
				sid = "other-surface"
			}
			result := svc.ApplyAgentEvent(iid, predictionTestResult(t, request.CommandID, "turn-1", "过期建议"))
			if stale != "actor" && stale != "surface" && findPageEvent(result) != nil {
				t.Fatal("late result must not render")
			}
			accepted := svc.ApplySurfaceAction(predictionTestAction(t, predictionAcceptKind, sid, request.CommandID, actor, ""))
			if page := findPageEvent(accepted); page != nil && len(page.Sections) != 0 {
				t.Fatal("stale or foreign suggestion must not become a draft")
			}
			if findAgentCommand(accepted, agentproto.CommandPromptSend) != nil {
				t.Fatal("invalid accept must not send")
			}
		})
	}
}

func TestPredictionNewInputCancelsOwnedSamplingWithoutBlockingInput(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	if request == nil {
		t.Fatal("missing prediction")
	}
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", Text: "继续我的任务"})
	if findAgentCommand(events, predictionCancelKind) == nil {
		t.Fatal("new input must cancel previous sample")
	}
	if findAgentCommand(events, agentproto.CommandPromptSend) == nil {
		t.Fatal("sample must never block real input")
	}
}

func TestPredictionOldWrapperIsNotSentUnknownCommands(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	svc.root.Instances[iid].Capabilities = agentproto.Capabilities{}
	if findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind) != nil {
		t.Fatal("old wrapper must not get prediction command")
	}
}

func TestPredictionCancellationKeepsOriginalInstanceAndIsNotATurn(t *testing.T) {
	svc, sid, iid := predictionTestSetup(t)
	request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
	if request == nil {
		t.Fatal("missing request")
	}
	events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionDetach, SurfaceSessionID: sid, ActorUserID: "user-1"})
	cancel := findAgentCommand(events, predictionCancelKind)
	if cancel == nil || cancel.Prediction.InstanceID != iid || cancel.Prediction.RequestCommandID != request.CommandID {
		t.Fatal("cancel must target original instance/request after detach")
	}
	if !NonTurnAgentCommand(request.Kind) || !NonTurnAgentCommand(cancel.Kind) {
		t.Fatal("sampling must not bind ordinary turn ownership")
	}
	if failed := svc.HandleCommandDispatchFailure(sid, cancel.CommandID, nil); len(failed) != 0 {
		t.Fatal("cancel failure must not fail normal work")
	}
}

func TestPredictionUnsupportedAndInvalidResultsDoNotAffectMainTurn(t *testing.T) {
	for _, value := range []string{"unsupported", "error", "source", "empty"} {
		t.Run(value, func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			request := findAgentCommand(predictionCompleteTurn(t, svc, sid, iid), predictionRequestKind)
			if request == nil {
				t.Fatal("missing request")
			}
			event := predictionTestResult(t, request.CommandID, "turn-1", "可以继续")
			switch value {
			case "unsupported":
				event.Prediction.Unsupported = true
				event.ErrorMessage = "method not found"
			case "error":
				event.ErrorMessage = "provider refused"
			case "source":
				event.Prediction.SourceTurnID = "old-turn"
			case "empty":
				event.Prediction.Suggestion = ""
			}
			result := svc.ApplyAgentEvent(iid, event)
			if findPageEvent(result) != nil || findAgentCommand(result, agentproto.CommandPromptSend) != nil {
				t.Fatal("invalid sample must not produce draft or main turn")
			}
			if value == "unsupported" && !svc.predictionUnsupported[iid] {
				t.Fatal("unsupported must be remembered for connection")
			}
			if svc.root.Surfaces[sid].ActiveQueueItemID != "" {
				t.Fatal("prediction must leave completed input completed")
			}
		})
	}
}

func TestPredictionSkipsAsyncQuestionsAndActiveGoal(t *testing.T) {
	for _, blocker := range []string{"question", "goal"} {
		t.Run(blocker, func(t *testing.T) {
			svc, sid, iid := predictionTestSetup(t)
			// Evaluate the admission boundary using the same completed source.
			predictionCompleteTurn(t, svc, sid, iid)
			p := svc.predictions[sid]
			if p == nil {
				t.Fatal("missing prediction")
			}
			if blocker == "goal" {
				svc.root.Instances[iid].Threads["thread-1"].ThreadGoal = &agentproto.ThreadGoalUpdate{Status: "active"}
			} else {
				ui := svc.ensureSurfaceUIRuntime(svc.root.Surfaces[sid])
				ui.AsyncUserInputs = map[string]*asyncUserInputRecord{"question": {ThreadID: "thread-1"}}
			}
			result := svc.ApplyAgentEvent(iid, predictionTestResult(t, p.ID, "turn-1", "不应盖过问题"))
			if findPageEvent(result) != nil {
				t.Fatal("prediction must not distract from active goal/question")
			}
		})
	}
}
