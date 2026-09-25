package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
)

func TestInboundGroupTopicsRemainIndependent(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	for _, tc := range []struct{ name, message, root, thread, chatType, want string }{
		{"topic A root", "om_a", "", "omt_a", "group", "feishu:app-1:chat:oc_chat@om_a"},
		{"topic B root", "om_b", "", "omt_b", "group", "feishu:app-1:chat:oc_chat@om_b"},
		{"topic A reply", "om_reply_a", "om_a", "omt_a", "group", "feishu:app-1:chat:oc_chat@om_a"},
		{"topic B reply", "om_reply_b", "om_b", "omt_b", "group", "feishu:app-1:chat:oc_chat@om_b"},
		{"root fallback", "om_reply", "om_legacy", "", "group", "feishu:app-1:chat:oc_chat@om_legacy"},
		{"message fallback", "om_plain", "", "", "group", "feishu:app-1:chat:oc_chat@om_plain"},
		{"p2p root", "om_p", "", "", "p2p", "feishu:app-1:user:ou_user"},
		{"p2p reply", "om_p2", "om_p", "omt_p", "p2p", "feishu:app-1:user:ou_user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := testTextMessageEvent(tc.name, tc.message, "hello")
			event.Event.Message.ChatType = stringRef(tc.chatType)
			event.Event.Message.RootId = stringRef(tc.root)
			event.Event.Message.ThreadId = stringRef(tc.thread)
			action, ok, err := gateway.parseMessageEvent(t.Context(), event)
			if err != nil || !ok || action.SurfaceSessionID != tc.want || action.ChatID != "oc_chat" {
				t.Fatalf("inbound = %#v, handled=%v, err=%v, want surface %q", action, ok, err, tc.want)
			}
			if got := gateway.lookupSurfaceMessage(tc.message); got != tc.want {
				t.Fatalf("message surface = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTopicCardCallbackSurvivesGatewayRebuild(t *testing.T) {
	for _, tc := range []struct{ surface, chat, want string }{
		{"feishu:app-1:chat:oc_chat@om_a", "oc_chat", "feishu:app-1:chat:oc_chat@om_a"},
		{"feishu:app-1:chat:oc_chat@om_b", "oc_chat", "feishu:app-1:chat:oc_chat@om_b"},
		{"feishu:app-1:chat:oc_chat@om_a", "oc_other", ""},
		{"feishu:app-2:chat:oc_chat@om_a", "oc_chat", ""},
	} {
		gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
		got := gateway.surfaceForCardAction(gatewaypkg.CardActionSurfaceLookup{MessageID: "om_card", PayloadSurfaceSessionID: tc.surface, ChatID: tc.chat, OperatorID: "ou_user"})
		if got != tc.want {
			t.Errorf("surface(%q, %q) = %q, want %q", tc.surface, tc.chat, got, tc.want)
		}
		if tc.want != "" && gateway.surfaceReplyAnchor(tc.want) != "om_card" {
			t.Fatal("rebuilt gateway did not retain callback message anchor")
		}
	}
}

func TestTopicRepliesUseThreadOnWire(t *testing.T) {
	for _, scope := range []string{"chat:oc_chat@om_a", "user:ou_user"} {
		for _, kind := range []OperationKind{OperationSendText, OperationSendCard, OperationSendImage} {
			t.Run(fmt.Sprintf("%s/%s", scope, kind), func(t *testing.T) {
				var body map[string]any
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/open-apis/auth/v3/tenant_access_token/internal":
						_, _ = w.Write([]byte(`{"code":0,"tenant_access_token":"test-token","expire":7200}`))
					case "/open-apis/im/v1/messages/om_source/reply", "/open-apis/im/v1/messages/om_a/reply":
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						_, _ = w.Write([]byte(`{"code":0,"data":{"message_id":"om_reply"}}`))
					default:
						t.Errorf("unexpected request: %s", r.URL.Path)
						http.Error(w, "unexpected request", http.StatusNotFound)
					}
				}))
				defer server.Close()
				gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1", AppID: "topic-test", AppSecret: "test", Domain: server.URL})
				gateway.uploadImagePathFn = func(context.Context, string) (string, error) { return "img_test", nil }
				err := gateway.Apply(t.Context(), []Operation{{Kind: kind, SurfaceSessionID: "feishu:app-1:" + scope, ChatID: "oc_chat", ReplyToMessageID: "om_source", Text: "hello", CardTitle: "hello", ImagePath: "/tmp/topic-test.png"}})
				if err != nil {
					t.Fatal(err)
				}
				if got, _ := body["reply_in_thread"].(bool); got != (scope == "chat:oc_chat@om_a") {
					t.Fatalf("reply_in_thread = %v, body=%#v", got, body)
				}
				if gateway.lookupSurfaceMessage("om_reply") != "feishu:app-1:"+scope {
					t.Fatal("reply lost surface mapping")
				}
			})
		}
	}
}

func TestTopicReplyFailureDoesNotEscapeToMainChat(t *testing.T) {
	for _, kind := range []OperationKind{OperationSendText, OperationSendCard, OperationSendImage} {
		t.Run(string(kind), func(t *testing.T) {
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			failure := errors.New("reply failed")
			gateway.recordSurfaceMessage("om_a", "feishu:app-1:chat:oc_chat@om_a")
			gateway.uploadImagePathFn = func(context.Context, string) (string, error) { return "img_test", nil }
			gateway.replyMessageFn = func(context.Context, string, string, string, bool) (*larkim.ReplyMessageResp, error) {
				return nil, failure
			}
			gateway.createMessageFn = func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error) {
				t.Fatal("topic reply escaped to main chat")
				return nil, nil
			}
			err := gateway.Apply(t.Context(), []Operation{{Kind: kind, SurfaceSessionID: "feishu:app-1:chat:oc_chat@om_a", ChatID: "oc_chat", ReplyToMessageID: "om_a", Text: "hello", ImagePath: "/tmp/topic-test.png"}})
			if !errors.Is(err, failure) {
				t.Fatalf("error = %v, want original reply error", err)
			}
			if err := gateway.Apply(t.Context(), []Operation{{Kind: kind, SurfaceSessionID: "feishu:app-1:chat:oc_chat@om_a", ChatID: "oc_chat", ImagePath: "/tmp/topic-test.png"}}); !errors.Is(err, failure) {
				t.Fatalf("topic root reply error = %v", err)
			}
		})
	}
}

func TestInboundGroupTopicsDoNotBlockEachOther(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	startedA, finishedB, releaseA := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(releaseA)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	lane := newSurfaceInboundLane(ctx, gateway, func(_ context.Context, action control.Action) *ActionResult {
		switch action.MessageID {
		case "om_a":
			close(startedA)
			<-releaseA
		case "om_b":
			close(finishedB)
		}
		return nil
	})
	for _, id := range []string{"om_a", "om_b"} {
		event := testTextMessageEvent("evt-"+id, id, "hello")
		event.Event.Message.ChatType = stringRef("group")
		plan, ok, err := gateway.planInboundMessageEvent(event)
		if err != nil || !ok || plan.queue == nil || !lane.enqueue(plan.queue) {
			t.Fatalf("enqueue %s: ok=%v err=%v", id, ok, err)
		}
		done := startedA
		if id == "om_b" {
			done = finishedB
		}
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("topic %s blocked", id)
		}
	}
}

func TestTopicAppendUsesObservedMessageAnchor(t *testing.T) {
	for _, root := range []string{"om_root", ""} {
		for _, kind := range []OperationKind{OperationSendText, OperationSendCard, OperationSendImage} {
			t.Run(fmt.Sprintf("root=%s/%s", root, kind), func(t *testing.T) {
				gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
				event := testTextMessageEvent("evt-menu", "om_incoming", "/menu")
				event.Event.Message.ChatType = stringRef("group")
				event.Event.Message.RootId = stringRef(root)
				event.Event.Message.ThreadId = stringRef("omt_thread")
				action, ok, err := gateway.parseMessageEvent(t.Context(), event)
				if err != nil || !ok {
					t.Fatalf("inbound: handled=%v err=%v", ok, err)
				}
				gateway.uploadImagePathFn = func(context.Context, string) (string, error) { return "img_test", nil }
				called := false
				gateway.replyMessageFn = func(_ context.Context, messageID, _, _ string, inThread bool) (*larkim.ReplyMessageResp, error) {
					called = true
					if messageID != "om_incoming" || !inThread {
						t.Fatalf("reply target=%q inThread=%v", messageID, inThread)
					}
					return &larkim.ReplyMessageResp{}, nil
				}
				if err := gateway.Apply(t.Context(), []Operation{{Kind: kind, SurfaceSessionID: action.SurfaceSessionID, ChatID: action.ChatID, Text: "menu", ImagePath: "/tmp/topic-test.png"}}); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("no topic reply")
				}
			})
		}
	}
}

func TestTopicReplyRejectsKnownOtherSurfaceAnchor(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	gateway.recordSurfaceMessage("om_b", "feishu:app-1:chat:oc_chat@om_b")
	gateway.replyMessageFn = func(context.Context, string, string, string, bool) (*larkim.ReplyMessageResp, error) {
		t.Fatal("replied in another topic")
		return nil, nil
	}
	gateway.createMessageFn = func(context.Context, string, string, string, string) (*larkim.CreateMessageResp, error) {
		t.Fatal("escaped to main chat")
		return nil, nil
	}
	err := gateway.Apply(t.Context(), []Operation{{Kind: OperationSendText, SurfaceSessionID: "feishu:app-1:chat:oc_chat@om_a", ChatID: "oc_chat", ReplyToMessageID: "om_b", Text: "hello"}})
	if err == nil {
		t.Fatal("expected mismatched topic anchor to be rejected")
	}
}

func TestInboundTopicRootAndReplyShareRootSurface(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	for _, messageID := range []string{"om_root", "om_reply"} {
		event := testTextMessageEvent("evt-"+messageID, messageID, "hello")
		event.Event.Message.ChatType = stringRef("group")
		event.Event.Message.ThreadId = stringRef("omt_shared")
		if messageID == "om_reply" {
			event.Event.Message.RootId = stringRef("om_root")
			event.Event.Message.ParentId = stringRef("om_root")
		}
		action, ok, err := gateway.parseMessageEvent(t.Context(), event)
		if err != nil || !ok {
			t.Fatalf("parse %s: handled=%v err=%v", messageID, ok, err)
		}
		if action.SurfaceSessionID != "feishu:app-1:chat:oc_chat@om_root" {
			t.Fatalf("%s surface = %q, want shared root surface", messageID, action.SurfaceSessionID)
		}
	}
}

func TestOrdinaryMessagePromotedToTopicKeepsSurface(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	initial := testTextMessageEvent("evt-root", "om_root", "hello")
	initial.Event.Message.ChatType = stringRef("group")
	first, ok, err := gateway.parseMessageEvent(t.Context(), initial)
	if err != nil || !ok {
		t.Fatalf("root: handled=%v err=%v", ok, err)
	}
	gateway.replyMessageFn = func(_ context.Context, messageID, _, _ string, inThread bool) (*larkim.ReplyMessageResp, error) {
		if messageID != "om_root" || !inThread {
			t.Fatalf("promotion reply: %q, inThread=%v", messageID, inThread)
		}
		return &larkim.ReplyMessageResp{Data: &larkim.ReplyMessageRespData{MessageId: stringRef("om_bot")}}, nil
	}
	if err := gateway.Apply(t.Context(), []Operation{{Kind: OperationSendText, SurfaceSessionID: first.SurfaceSessionID, ChatID: first.ChatID, Text: "answer"}}); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"om_root", ""} {
		reply := testTextMessageEvent("evt-reply-"+root, "om_reply_"+root, "continue")
		reply.Event.Message.ChatType = stringRef("group")
		reply.Event.Message.RootId = stringRef(root)
		reply.Event.Message.ParentId = stringRef("om_bot")
		reply.Event.Message.ThreadId = stringRef("omt_created")
		next, ok, err := gateway.parseMessageEvent(t.Context(), reply)
		if err != nil || !ok || next.SurfaceSessionID != first.SurfaceSessionID {
			t.Fatalf("root=%q reply surface=%q, want %q, handled=%v err=%v", root, next.SurfaceSessionID, first.SurfaceSessionID, ok, err)
		}
	}
}

func TestParentOnlyGroupReplyKeepsParentWithinCurrentChat(t *testing.T) {
	for _, parentSurface := range []string{"", "feishu:app-1:chat:oc_chat", "feishu:app-2:chat:oc_chat@om_root", "feishu:app-1:chat:oc_other@om_root"} {
		gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
		gateway.recordSurfaceMessage("om_parent", parentSurface)
		event := testTextMessageEvent("evt-incomplete", "om_reply", "hello")
		event.Event.Message.ChatType = stringRef("group")
		event.Event.Message.ParentId = stringRef("om_parent")
		event.Event.Message.ThreadId = stringRef("omt_thread")
		action, ok, err := gateway.parseMessageEvent(t.Context(), event)
		if err != nil || !ok || action.SurfaceSessionID != "feishu:app-1:chat:oc_chat@om_parent" {
			t.Fatalf("parent %q routed incorrectly: %#v, err=%v", parentSurface, action, err)
		}
	}
}
