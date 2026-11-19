package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestAsyncUserInputRejectedCallbackPreservesQuestionCard(t *testing.T) {
	for _, reason := range []string{"empty", "actor", "sync_request"} {
		t.Run(reason, func(t *testing.T) {
			gateway := &recordingGateway{}
			app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
			seedSelectedThreadSurfaceForInboundTests(app)
			surfaceID := "feishu:app-1:chat:1"
			app.service.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID, ActorUserID: "user-1", MessageID: "initial", Text: "请继续"})
			app.service.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1"})
			events := app.service.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind: agentproto.EventItemCompleted, ThreadID: "thread-1", TurnID: "turn-1", ItemID: "item-1", ItemKind: "agent_message",
				Metadata: map[string]any{"delivery": "async", "questions": []any{map[string]any{"title": "是否切换？", "options": []any{"允许"}}}},
			})
			if len(events) != 1 || events[0].PageView == nil {
				t.Fatalf("expected question page: %#v", events)
			}
			payload := events[0].PageView.Sections[0].Entries[0].Buttons[0].CallbackValue
			action := control.Action{
				Kind: control.ActionAsyncUserInput, SurfaceSessionID: surfaceID, ActorUserID: "user-1", MessageID: "om-question",
				Inbound: &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: app.daemonLifecycleID},
				Request: &control.ActionRequestResponse{RequestID: payload["request_id"].(string), RequestRevision: payload["request_revision"].(int), RequestOptionID: "0"},
			}
			switch reason {
			case "empty":
				action.Request.RequestOptionID = ""
			case "actor":
				action.ActorUserID = "another-user"
			case "sync_request":
				app.service.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventRequestStarted, ThreadID: "thread-1", TurnID: "turn-1", RequestID: "sync-1", Metadata: map[string]any{"requestType": "approval", "title": "先确认命令"}})
			}
			gateway.operations = nil
			if result := app.HandleGatewayAction(context.Background(), action); result != nil {
				t.Fatalf("rejected answer must not replace original question: %#v", result)
			}
			if len(gateway.operations) == 0 {
				t.Fatal("rejected answer must show a notice")
			}
			for _, op := range gateway.operations {
				if op.Kind == feishu.OperationUpdateCard || op.MessageID == "om-question" {
					t.Fatalf("rejection overwrote original question: %#v", op)
				}
			}
			if reason == "sync_request" {
				app.service.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventRequestResolved, RequestID: "sync-1", ThreadID: "thread-1", TurnID: "turn-1"})
			}
			action.ActorUserID, action.Request.RequestOptionID = "user-1", "0"
			gateway.operations = nil
			if result := app.HandleGatewayAction(context.Background(), action); result != nil {
				t.Fatalf("successful answer uses explicit message update: %#v", result)
			}
			for _, op := range gateway.operations {
				if op.Kind == feishu.OperationUpdateCard && op.MessageID == "om-question" {
					return
				}
			}
			t.Fatalf("original draft must remain answerable and update its own card: %#v", gateway.operations)
		})
	}
}
