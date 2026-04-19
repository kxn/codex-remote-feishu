package feishu

import (
	"context"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleInboundMessageEventDeliversFileAction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.downloadFileFn = func(_ context.Context, messageID, fileKey, fileName string) (string, error) {
		if messageID != "om-file-2" || fileKey != "file-key-2" || fileName != "design.pdf" {
			t.Fatalf("unexpected download args: message=%q key=%q name=%q", messageID, fileKey, fileName)
		}
		return "/tmp/design.pdf", nil
	}

	called := make(chan control.Action, 1)
	handler := func(_ context.Context, action control.Action) *ActionResult {
		called <- action
		return nil
	}

	event := &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:   "evt-file-2",
				EventType: "im.message.receive_v1",
			},
		},
		EventReq: &larkevent.EventReq{
			Header: map[string][]string{
				larkcore.HttpHeaderKeyRequestId: {"req-file-2"},
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-file-2"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("file"),
				Content:     stringRef(`{"file_key":"file-key-2","file_name":"design.pdf"}`),
			},
		},
	}

	if err := gateway.handleInboundMessageEvent(t.Context(), event, handler, nil); err != nil {
		t.Fatalf("handleInboundMessageEvent returned error: %v", err)
	}

	select {
	case action := <-called:
		if action.Kind != control.ActionFileMessage || action.LocalPath != "/tmp/design.pdf" || action.FileName != "design.pdf" {
			t.Fatalf("unexpected delivered file action: %#v", action)
		}
	case <-t.Context().Done():
		t.Fatal("context cancelled before file action delivery")
	}
}
