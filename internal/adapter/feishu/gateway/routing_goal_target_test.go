package gateway

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	frontstagecontract "github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestParsePageCallbacksPreserveOnlyGoalTarget(t *testing.T) {
	for _, test := range []struct {
		name         string
		callbackKind string
		actionKind   control.ActionKind
		actionArg    string
		argPrefix    string
		formValue    string
		wantText     string
		wantThread   string
		wantInstance string
	}{
		{
			name: "goal button", callbackKind: cardActionKindPageAction,
			actionKind: control.ActionGoalCommand, actionArg: "pause", wantText: "/goal pause",
			wantThread: "thread-original", wantInstance: "instance-original",
		},
		{
			name: "goal form", callbackKind: cardActionKindPageSubmit,
			actionKind: control.ActionGoalCommand, argPrefix: "edit", formValue: "更新目标", wantText: "/goal edit 更新目标",
			wantThread: "thread-original", wantInstance: "instance-original",
		},
		{
			name: "other command button", callbackKind: cardActionKindPageAction,
			actionKind: control.ActionModelCommand, actionArg: "gpt-5.5", wantText: "/model gpt-5.5",
		},
		{
			name: "other command form", callbackKind: cardActionKindPageSubmit,
			actionKind: control.ActionModelCommand, formValue: "gpt-5.5", wantText: "/model gpt-5.5",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			userID := "user-1"
			event := &larkcallback.CardActionTriggerEvent{
				EventV2Base: &larkevent.EventV2Base{Header: &larkevent.EventHeader{
					EventID: "event-1", EventType: "card.action.trigger",
				}},
				EventReq: &larkevent.EventReq{Header: map[string][]string{
					larkcore.HttpHeaderKeyRequestId: {"request-1"},
				}},
				Event: &larkcallback.CardActionTriggerRequest{
					Operator: &larkcallback.Operator{UserID: &userID},
					Context: &larkcallback.Context{
						OpenChatID: "chat-1", OpenMessageID: "message-1",
					},
					Action: &larkcallback.CallBackAction{
						Value: map[string]interface{}{
							frontstagecontract.CardActionPayloadKeyKind:              test.callbackKind,
							frontstagecontract.CardActionPayloadKeyActionKind:        string(test.actionKind),
							frontstagecontract.CardActionPayloadKeyActionArg:         test.actionArg,
							frontstagecontract.CardActionPayloadKeyActionArgPrefix:   test.argPrefix,
							frontstagecontract.CardActionPayloadKeyFieldName:         "command_args",
							frontstagecontract.CardActionPayloadKeyThreadID:          " thread-original ",
							frontstagecontract.CardActionPayloadKeyInstanceID:        " instance-original ",
							frontstagecontract.CardActionPayloadKeyGoalOperationID:   " goal-operation-original ",
							frontstagecontract.CardActionPayloadKeyCatalogFamilyID:   "source-family",
							frontstagecontract.CardActionPayloadKeyCatalogVariantID:  "source-variant",
							frontstagecontract.CardActionPayloadKeyCatalogBackend:    string(agentproto.BackendClaude),
							frontstagecontract.CardActionPayloadKeyDaemonLifecycleID: "daemon-1",
						},
						FormValue: map[string]interface{}{"command_args": test.formValue},
					},
				},
			}
			action, ok := ParseCardActionTriggerEvent(RoutingEnv{
				GatewayID: "app-1",
				SurfaceForCardAction: func(CardActionSurfaceLookup) string {
					return "surface-1"
				},
			}, event)
			if !ok || action.Kind != test.actionKind || action.Text != test.wantText {
				t.Fatalf("unexpected parsed callback: ok=%t action=%#v", ok, action)
			}
			if action.ThreadID != test.wantThread || action.InstanceID != test.wantInstance {
				t.Fatalf("target = (%q, %q), want (%q, %q)", action.ThreadID, action.InstanceID, test.wantThread, test.wantInstance)
			}
			wantOperation := ""
			if test.actionKind == control.ActionGoalCommand {
				wantOperation = "goal-operation-original"
			}
			if action.GoalOperationID != wantOperation {
				t.Fatalf("Goal operation = %q, want %q", action.GoalOperationID, wantOperation)
			}
			if action.GatewayID != "app-1" || action.SurfaceSessionID != "surface-1" ||
				action.ChatID != "chat-1" || action.ActorUserID != "user-1" || action.MessageID != "message-1" {
				t.Fatalf("callback routing was lost: %#v", action)
			}
			if action.CatalogFamilyID != "source-family" || action.CatalogVariantID != "source-variant" ||
				action.CatalogBackend != agentproto.BackendClaude {
				t.Fatalf("callback provenance was lost: %#v", action)
			}
			if action.Inbound == nil || !action.Inbound.CardCallback || action.Inbound.CardDaemonLifecycleID != "daemon-1" ||
				action.Inbound.OpenMessageID != "message-1" || action.Inbound.EventID != "event-1" || action.Inbound.RequestID != "request-1" {
				t.Fatalf("callback lifecycle or inbound metadata was lost: %#v", action.Inbound)
			}
		})
	}
}
