package feishu

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func mustDecodeForwardedChatEnvelopeInput(t *testing.T, input agentproto.Input, tag string) forwardedChatEnvelope {
	t.Helper()
	if input.Type != agentproto.InputText {
		t.Fatalf("expected text envelope input, got %#v", input)
	}
	startTag := "<" + tag + ">\n"
	endTag := "\n</" + tag + ">"
	if !strings.HasPrefix(input.Text, startTag) || !strings.HasSuffix(input.Text, endTag) {
		t.Fatalf("unexpected envelope wrapper: %q", input.Text)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(input.Text, startTag), endTag)
	var envelope forwardedChatEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("decode forwarded envelope: %v", err)
	}
	return envelope
}

func TestParseMessageEventHandlesMergeForwardWithNestedImages(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.fetchMessageFn = func(_ context.Context, messageID string) (*gatewayMessage, error) {
		if messageID != "om-forward-image-1" {
			t.Fatalf("unexpected merge forward lookup: %s", messageID)
		}
		return &gatewayMessage{
			MessageID:   "om-forward-image-1",
			MessageType: "merge_forward",
			Content:     `{"title":"顶层合集"}`,
			Children: []*gatewayMessage{
				{
					MessageID:   "om-forward-image-child-1",
					MessageType: "image",
					SenderID:    "ou_user_a",
					SenderType:  "user",
					Content:     `{"image_key":"img-top"}`,
				},
				{
					MessageID:   "om-forward-image-child-2",
					MessageType: "merge_forward",
					Content:     `{"title":"内层合集"}`,
					Children: []*gatewayMessage{
						{
							MessageID:   "om-forward-image-child-3",
							MessageType: "image",
							SenderID:    "ou_user_b",
							SenderType:  "user",
							Content:     `{"image_key":"img-nested"}`,
						},
					},
				},
			},
		}, nil
	}
	gateway.downloadImageFn = func(_ context.Context, messageID, imageKey string) (string, string, error) {
		switch messageID {
		case "om-forward-image-child-1":
			if imageKey != "img-top" {
				t.Fatalf("unexpected top image key: %s", imageKey)
			}
			return "/tmp/top.png", "image/png", nil
		case "om-forward-image-child-3":
			if imageKey != "img-nested" {
				t.Fatalf("unexpected nested image key: %s", imageKey)
			}
			return "/tmp/nested.png", "image/png", nil
		default:
			t.Fatalf("unexpected image download: message=%s key=%s", messageID, imageKey)
			return "", "", nil
		}
	}
	event := &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-forward-image-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("group"),
				MessageType: stringRef("merge_forward"),
				Content:     stringRef("Merged and Forwarded Message"),
			},
		},
	}

	action, ok, err := gateway.parseMessageEvent(t.Context(), event)
	if err != nil {
		t.Fatalf("parseMessageEvent returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected merge forward with images to be handled")
	}
	if action.Text != "顶层合集\n用户(ou_user_a): [图片]\n内层合集\n用户(ou_user_b): [图片]" {
		t.Fatalf("unexpected image merge forward summary: %#v", action)
	}
	if len(action.Inputs) != 3 {
		t.Fatalf("expected envelope plus two image inputs, got %#v", action.Inputs)
	}
	envelope := mustDecodeForwardedChatEnvelopeInput(t, action.Inputs[0], forwardedChatInputTagV1)
	if envelope.Schema != forwardedChatSchemaV1 || envelope.Root.Title != "顶层合集" {
		t.Fatalf("unexpected forwarded envelope header: %#v", envelope)
	}
	if envelope.Assets == nil || len(envelope.Assets.Images) != 2 {
		t.Fatalf("expected two image assets, got %#v", envelope.Assets)
	}
	if envelope.Root.Items[0].MessageType != "image" || len(envelope.Root.Items[0].ImageRefs) != 1 || envelope.Root.Items[0].ImageRefs[0] != "img_001" {
		t.Fatalf("unexpected top image node: %#v", envelope.Root.Items[0])
	}
	if envelope.Root.Items[1].Kind != "bundle" || envelope.Root.Items[1].Title != "内层合集" {
		t.Fatalf("unexpected nested bundle node: %#v", envelope.Root.Items[1])
	}
	nested := envelope.Root.Items[1]
	if len(nested.Items) != 1 || nested.Items[0].MessageType != "image" || len(nested.Items[0].ImageRefs) != 1 || nested.Items[0].ImageRefs[0] != "img_002" {
		t.Fatalf("unexpected nested image node: %#v", nested)
	}
	if action.Inputs[1].Type != agentproto.InputLocalImage || action.Inputs[1].Path != "/tmp/top.png" {
		t.Fatalf("unexpected first image input: %#v", action.Inputs[1])
	}
	if action.Inputs[2].Type != agentproto.InputLocalImage || action.Inputs[2].Path != "/tmp/nested.png" {
		t.Fatalf("unexpected second image input: %#v", action.Inputs[2])
	}
}
