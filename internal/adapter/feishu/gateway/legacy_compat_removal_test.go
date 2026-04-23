package gateway

import (
	"strings"
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestPlanInboundMessageEventRemovedLegacyCompatQueuesPlainTextMessage(t *testing.T) {
	env := InboundEnv{
		GatewayID:       "app-2",
		ParseTextAction: parseTextAction,
		RecordSurfaceMessage: func(messageID, surfaceSessionID string) {
			t.Helper()
			if messageID != "om-msg-compat" {
				t.Fatalf("unexpected recorded message id: %q", messageID)
			}
			if surfaceSessionID == "" {
				t.Fatal("expected surface session id to be recorded")
			}
		},
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-compat"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":" /newinstance "}`),
			},
		},
	}

	planned, ok, err := PlanInboundMessageEvent(env, event)
	if err != nil {
		t.Fatalf("PlanInboundMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected removed compat command to remain handled")
	}
	if planned.Action != nil {
		t.Fatalf("expected removed compat command to avoid direct action fallback, got %#v", planned.Action)
	}
	if planned.Queue == nil {
		t.Fatal("expected removed compat command to queue as plain text work")
	}
	if planned.Queue.messageType != "text" || strings.TrimSpace(planned.Queue.text) != "/newinstance" {
		t.Fatalf("unexpected queued plain text work: %#v", planned.Queue)
	}
}

func stringRef(value string) *string {
	return &value
}
