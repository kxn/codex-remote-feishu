package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestPrivateOperationFreezesRealRenderedCard(t *testing.T) {
	op := Operation{Kind: OperationSendCard, SurfaceSessionID: "private", ChatID: "chat", CardTitle: "exported fallback", card: rawCardDocument("real title", "real body", "info", nil)}
	want := trimCardPayloadForMessageTransport(renderOperationCard(op, op.effectiveCardEnvelope()))
	raw, err := FreezePrivateOperation(op, "persistent-id")
	if err != nil {
		t.Fatal(err)
	}
	op.card.Title = "changed after freezing"
	got, err := ThawPrivateOperation(raw)
	if err != nil {
		t.Fatal(err)
	}
	actual := renderOperationCard(got, got.effectiveCardEnvelope())
	a, _ := json.Marshal(want)
	b, _ := json.Marshal(actual)
	if string(a) != string(b) {
		t.Fatalf("card wire lost actual document: %s / %s", a, b)
	}
}

func TestPrivateRemoveReactionUsesPersistedIDWithoutGatewayCache(t *testing.T) {
	g := NewLiveGateway(LiveGatewayConfig{GatewayID: "g"})
	calls := 0
	g.deleteReactionFn = func(_ context.Context, message, reaction string) (*larkim.DeleteMessageReactionResp, error) {
		calls++
		if message != "source" || reaction != "actual-reaction" {
			t.Errorf("wrong identity: %s %s", message, reaction)
		}
		return &larkim.DeleteMessageReactionResp{ApiResp: &larkcore.ApiResp{}}, nil
	}
	op := Operation{Kind: OperationRemoveReaction, GatewayID: "g", SurfaceSessionID: "s", ChatID: "chat", MessageID: "source", EmojiType: "THINKING", ReactionID: "actual-reaction"}
	raw, err := FreezePrivateOperation(op, "persisted-remove")
	if err != nil {
		t.Fatal(err)
	}
	op, err = ThawPrivateOperation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.Apply(t.Context(), []Operation{op}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("empty process cache silently skipped reaction removal")
	}
}

func TestPrivateDurableOperationUsesRealSDKUUIDAndOrdinaryReply(t *testing.T) {
	for _, reply := range []bool{false, true} {
		t.Run(fmt.Sprint("reply=", reply), func(t *testing.T) {
			calls := 0
			api := projectChatTestAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				body := projectReadJSON(t, r)
				if body["uuid"] != "persisted-operation" {
					t.Errorf("missing stable UUID: %#v", body)
				}
				if reply {
					if body["reply_in_thread"] != false || r.URL.Path != "/open-apis/im/v1/messages/source/reply" {
						t.Errorf("private reply became topic: %s %#v", r.URL.Path, body)
					}
				} else if body["receive_id"] != "oc_chat" || r.URL.Query().Get("receive_id_type") != "chat_id" {
					t.Errorf("wrong destination: %#v", body)
				}
				fmt.Fprint(w, `{"code":0,"data":{"message_id":"actual-platform-id","chat_id":"oc_chat"}}`)
			})
			g := NewLiveGateway(LiveGatewayConfig{GatewayID: "project-test"})
			g.broker = api.broker
			op := Operation{Kind: OperationSendText, GatewayID: "project-test", SurfaceSessionID: "s", ChatID: "oc_chat", Text: "hello"}
			if reply {
				op.ReplyToMessageID = "source"
			}
			raw, err := FreezePrivateOperation(op, "persisted-operation")
			if err != nil {
				t.Fatal(err)
			}
			op, err = ThawPrivateOperation(raw)
			if err != nil {
				t.Fatal(err)
			}
			operations := []Operation{op}
			if err = g.Apply(t.Context(), operations); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || operations[0].MessageID != "actual-platform-id" {
				t.Fatalf("receipt: %d %#v", calls, operations)
			}
		})
	}
}

func TestPrivateReplyUnknownNeverFallsBackToSecondMessage(t *testing.T) {
	for _, kind := range []OperationKind{OperationSendText, OperationSendCard, OperationSendImage} {
		t.Run(string(kind), func(t *testing.T) {
			g := NewLiveGateway(LiveGatewayConfig{GatewayID: "g"})
			want := errors.New("response lost after write")
			g.replyMessageFn = func(ctx context.Context, _, _, _ string) (*larkim.ReplyMessageResp, error) {
				if ctx.Value(privateTransportContextKey{}) != "durable-id" {
					t.Error("lost stable transport ID")
				}
				return nil, want
			}
			g.createMessageFn = func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error) {
				t.Fatal("unknown reply duplicated as a new message")
				return nil, nil
			}
			g.uploadImagePathFn = func(context.Context, string) (string, error) { return "image-key", nil }
			op := Operation{Kind: kind, GatewayID: "g", SurfaceSessionID: "s", ChatID: "oc_chat", ReplyToMessageID: "source", Text: "hi", ImagePath: "/image.png", CardTitle: "Menu"}
			raw, err := FreezePrivateOperation(op, "durable-id")
			if err != nil {
				t.Fatal(err)
			}
			op, err = ThawPrivateOperation(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err = g.Apply(t.Context(), []Operation{op}); !errors.Is(err, want) {
				t.Fatalf("lost unknown result: %v", err)
			}
		})
	}
}

func TestPrivateCardCodecKeepsUpdateTarget(t *testing.T) {
	op := Operation{Kind: OperationUpdateCard, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", MessageID: "existing-card", ProgressCardStartSeq: 7, ProgressCardEndSeq: 11, CardTitle: "working"}
	raw, err := FreezePrivateOperation(op, "stable")
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ThawPrivateOperation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]any{restored.Kind, restored.MessageID, restored.ProgressCardStartSeq, restored.ProgressCardEndSeq}, []any{op.Kind, op.MessageID, op.ProgressCardStartSeq, op.ProgressCardEndSeq}) {
		t.Fatal("update became new card or lost window")
	}
}

func TestStampPrivateViewPreservesLegacyCallbackValues(t *testing.T) {
	op := Operation{Kind: OperationSendCard, SurfaceSessionID: "surface", ChatID: "chat", CardTitle: "title", CardElements: []map[string]any{{"tag": "button", "text": map[string]any{"tag": "plain_text", "content": "go"}, "value": map[string]any{"kind": "page.action", "action_kind": "surface.command.menu", "action_arg": "settings"}}}}
	raw, err := FreezePrivateOperation(op, "transport")
	if err != nil {
		t.Fatal(err)
	}
	stamped, has, err := StampPrivateView(raw, "token")
	if err != nil || !has {
		t.Fatalf("missing callback: %v", err)
	}
	restored, err := ThawPrivateOperation(stamped)
	if err != nil {
		t.Fatal(err)
	}
	card := renderOperationCard(restored, restored.effectiveCardEnvelope())
	encoded, _ := json.Marshal(card)
	var root map[string]any
	if err = json.Unmarshal(encoded, &root); err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	var find func(any)
	find = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["kind"] == "page.action" {
				value = n
			}
			for _, child := range n {
				find(child)
			}
		case []any:
			for _, child := range n {
				find(child)
			}
		}
	}
	find(root)
	if value == nil {
		t.Fatal("callback value missing")
	}
	if value["private_view"] != "token" || value["action_arg"] != "settings" || value["kind"] != "page.action" {
		t.Fatalf("callback changed: %+v", value)
	}
	if _, _, err = StampPrivateView(stamped, "different"); err == nil {
		t.Fatal("token replaced")
	}
}
