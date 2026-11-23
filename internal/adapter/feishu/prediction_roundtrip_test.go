package feishu

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestPredictionCardDraftAndExplicitEditedSendRoundTrip(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	svc := orchestrator.NewService(func() time.Time { return now }, orchestrator.Config{}, renderer.NewPlanner())
	const sid = "feishu:app-1:user:user-1"
	root := t.TempDir()
	svc.MaterializeSurfaceResume(sid, "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "", state.SurfaceVerbosityNormal, "")
	svc.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-1", Online: true, Backend: agentproto.BackendCodex, Source: "headless", Managed: true, WorkspaceRoot: root,
		CapabilitiesDeclared: true, Capabilities: agentproto.Capabilities{ThreadPrediction: true},
		Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: root, Loaded: true}}})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", Text: "请检查实现"})
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Initiator: initiator})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed", Initiator: initiator})
	var id string
	for _, event := range completed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandThreadPredictionRequest {
			id = event.Command.CommandID
		}
	}
	if id == "" {
		t.Fatal("missing background prediction")
	}
	suggested := svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventThreadPredictionResult, CommandID: id, ThreadID: "thread-1",
		Prediction: &agentproto.PredictionResult{SourceTurnID: "turn-1", Suggestion: "请补充边界测试"}})
	projector := NewProjector()
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om-prediction", sid)
	callback := func(events []eventcontract.Event, kind control.ActionKind, edited string) control.Action {
		t.Helper()
		for _, event := range events {
			if event.PageView == nil {
				continue
			}
			event.DaemonLifecycleID = "lifecycle-1"
			ops := projector.ProjectEvent("chat-1", event)
			for _, op := range ops {
				payload := findPredictionCallback(op.CardElements, kind)
				if payload == nil {
					continue
				}
				if payload["daemon_lifecycle_id"] != "lifecycle-1" {
					t.Fatal("prediction callback must be stamped")
				}
				raw, _ := json.Marshal(payload)
				var wire map[string]interface{}
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				userID := "user-1"
				trigger := &larkcallback.CallBackAction{Value: wire}
				if edited != "" {
					trigger.FormValue = map[string]interface{}{"prediction_text": edited}
				}
				action, ok := gateway.parseCardActionTriggerEvent(&larkcallback.CardActionTriggerEvent{Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID}, Action: trigger, Context: &larkcallback.Context{OpenChatID: "chat-1", OpenMessageID: "om-prediction"},
				}})
				if !ok || action.Kind != kind || action.PredictionID != id || !action.LocalPageAction || !control.AllowsInlineCardReplacement(action) {
					t.Fatalf("callback lost token or lifecycle: %#v", action)
				}
				return action
			}
		}
		t.Fatal("rendered card has no callback")
		return control.Action{}
	}
	draft := svc.ApplySurfaceAction(callback(suggested, control.ActionPredictionAccept, ""))
	for _, event := range draft {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("adopting suggestion sent a message")
		}
	}
	sent := svc.ApplySurfaceAction(callback(draft, control.ActionPredictionSend, "请测试并发取消"))
	for _, event := range sent {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			if event.Command.Target.ThreadID != "thread-1" || len(event.Command.Prompt.Inputs) != 1 || event.Command.Prompt.Inputs[0].Text != "请测试并发取消" {
				t.Fatalf("edited send changed target/content: %#v", event.Command)
			}
			return
		}
	}
	t.Fatal("explicit form submit did not dispatch edited message")
}

func findPredictionCallback(value any, kind control.ActionKind) map[string]any {
	switch value := value.(type) {
	case []map[string]any:
		for _, item := range value {
			if found := findPredictionCallback(item, kind); found != nil {
				return found
			}
		}
	case []any:
		for _, item := range value {
			if found := findPredictionCallback(item, kind); found != nil {
				return found
			}
		}
	case map[string]any:
		if value["action_kind"] == string(kind) {
			return value
		}
		for _, item := range value {
			if found := findPredictionCallback(item, kind); found != nil {
				return found
			}
		}
	}
	return nil
}
