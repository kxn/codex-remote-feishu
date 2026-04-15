package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestSurfaceInboundLanePreservesFIFOForQueuedMessageAndReaction(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	done := make(chan struct{})

	var (
		mu    sync.Mutex
		order []control.ActionKind
	)
	handler := func(_ context.Context, action control.Action) *ActionResult {
		mu.Lock()
		order = append(order, action.Kind)
		mu.Unlock()
		if action.Kind == control.ActionTextMessage {
			close(firstStarted)
			<-releaseFirst
		}
		if action.Kind == control.ActionReactionCreated {
			close(done)
		}
		return nil
	}

	lane := newSurfaceInboundLane(ctx, gateway, handler)
	textPlan, ok, err := gateway.planInboundMessageEvent(testTextMessageEvent("evt-text-1", "om-msg-1", "你好"))
	if err != nil {
		t.Fatalf("planInboundMessageEvent returned error: %v", err)
	}
	if !ok || textPlan.queue == nil {
		t.Fatalf("expected queued ordinary text plan, got %#v", textPlan)
	}
	if !lane.enqueue(textPlan.queue) {
		t.Fatal("expected queued text to be accepted")
	}

	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first queued message to start")
	}

	reactionAction, ok := gateway.parseMessageReactionCreatedEvent(testReactionCreatedEvent("evt-react-1", "om-msg-1"))
	if !ok {
		t.Fatal("expected reaction event to parse")
	}
	if !lane.enqueue(&queuedActionWork{action: reactionAction}) {
		t.Fatal("expected reaction work to be accepted")
	}

	time.Sleep(150 * time.Millisecond)
	mu.Lock()
	if len(order) != 1 || order[0] != control.ActionTextMessage {
		t.Fatalf("expected reaction to wait behind queued text, got %#v", order)
	}
	mu.Unlock()

	close(releaseFirst)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued reaction to run")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != control.ActionTextMessage || order[1] != control.ActionReactionCreated {
		t.Fatalf("unexpected queued action order: %#v", order)
	}
}

func TestSurfaceInboundLaneSuppressesDuplicateQueuedMessageEvent(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan control.ActionKind, 2)
	handler := func(_ context.Context, action control.Action) *ActionResult {
		called <- action.Kind
		return nil
	}
	lane := newSurfaceInboundLane(ctx, gateway, handler)

	plan, ok, err := gateway.planInboundMessageEvent(testTextMessageEvent("evt-dup-1", "om-msg-dup", "重复消息"))
	if err != nil {
		t.Fatalf("planInboundMessageEvent returned error: %v", err)
	}
	if !ok || plan.queue == nil {
		t.Fatalf("expected queued ordinary text plan, got %#v", plan)
	}
	if !lane.enqueue(plan.queue) {
		t.Fatal("expected first queued event to be accepted")
	}
	if !lane.enqueue(plan.queue) {
		t.Fatal("expected duplicate queued event to be accepted and suppressed")
	}

	select {
	case kind := <-called:
		if kind != control.ActionTextMessage {
			t.Fatalf("unexpected first handler kind: %s", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for queued message handler")
	}

	select {
	case kind := <-called:
		t.Fatalf("expected duplicate event suppression, got extra handler call: %s", kind)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestHandleInboundMessageEventSuppressesDuplicateCommandEvent(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	called := make(chan control.ActionKind, 2)
	handler := func(_ context.Context, action control.Action) *ActionResult {
		called <- action.Kind
		return nil
	}
	lane := newSurfaceInboundLane(ctx, gateway, handler)

	event := testTextMessageEvent("evt-cmd-dup-1", "om-msg-cmd", "/cron")
	if err := gateway.handleInboundMessageEvent(ctx, event, handler, lane); err != nil {
		t.Fatalf("first handleInboundMessageEvent returned error: %v", err)
	}
	if err := gateway.handleInboundMessageEvent(ctx, event, handler, lane); err != nil {
		t.Fatalf("duplicate handleInboundMessageEvent returned error: %v", err)
	}

	select {
	case kind := <-called:
		if kind != control.ActionCronCommand {
			t.Fatalf("unexpected handler kind: %s", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for command handler call")
	}

	select {
	case kind := <-called:
		t.Fatalf("expected duplicate command event suppression, got extra handler call: %s", kind)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestQueuedInboundFailureSendsReplyCard(t *testing.T) {
	gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
	var (
		downloadCtx context.Context
		replyCtx    context.Context
	)
	gateway.downloadImageFn = func(ctx context.Context, _, _ string) (string, string, error) {
		downloadCtx = ctx
		return "", "", errors.New("boom")
	}

	replyCalled := make(chan string, 1)
	gateway.replyMessageFn = func(ctx context.Context, messageID, msgType, content string) (*larkim.ReplyMessageResp, error) {
		replyCtx = ctx
		if messageID != "om-img-1" {
			t.Fatalf("unexpected reply target: %q", messageID)
		}
		if msgType != "interactive" {
			t.Fatalf("unexpected reply message type: %q", msgType)
		}
		replyCalled <- content
		return &larkim.ReplyMessageResp{
			ApiResp: &larkcore.ApiResp{},
			CodeError: larkcore.CodeError{
				Code: 0,
				Msg:  "ok",
			},
			Data: &larkim.ReplyMessageRespData{
				MessageId: stringRef("om-failure-1"),
			},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handlerCalled := make(chan struct{}, 1)
	lane := newSurfaceInboundLane(ctx, gateway, func(context.Context, control.Action) *ActionResult {
		handlerCalled <- struct{}{}
		return nil
	})

	plan, ok, err := gateway.planInboundMessageEvent(testImageMessageEvent("evt-image-1", "om-img-1", "img-key-1"))
	if err != nil {
		t.Fatalf("planInboundMessageEvent returned error: %v", err)
	}
	if !ok || plan.queue == nil {
		t.Fatalf("expected queued image plan, got %#v", plan)
	}
	if !lane.enqueue(plan.queue) {
		t.Fatal("expected queued image event to be accepted")
	}

	select {
	case payload := <-replyCalled:
		var card map[string]any
		if err := json.Unmarshal([]byte(payload), &card); err != nil {
			t.Fatalf("failure reply content is not valid json: %v", err)
		}
		header := card["header"].(map[string]any)
		title := header["title"].(map[string]any)
		if title["content"] != "消息未处理" {
			t.Fatalf("unexpected failure card title: %#v", card)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async failure reply card")
	}

	select {
	case <-handlerCalled:
		t.Fatal("expected async parse failure to stop before calling handler")
	case <-time.After(150 * time.Millisecond):
	}

	assertContextHasDeadlineWithin(t, downloadCtx, inboundMessageParseTimeout)
	assertContextHasDeadlineWithin(t, replyCtx, asyncInboundFailureNoticeTimeout)
}

func testTextMessageEvent(eventID, messageID, text string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    eventID,
				EventType:  "im.message.receive_v1",
				CreateTime: "1710000000000",
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef(messageID),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"` + text + `"}`),
				CreateTime:  stringRef("1710000001000"),
			},
		},
	}
}

func testImageMessageEvent(eventID, messageID, imageKey string) *larkim.P2MessageReceiveV1 {
	return &larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    eventID,
				EventType:  "im.message.receive_v1",
				CreateTime: "1710000000000",
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef(messageID),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("image"),
				Content:     stringRef(`{"image_key":"` + imageKey + `"}`),
				CreateTime:  stringRef("1710000001000"),
			},
		},
	}
}

func testReactionCreatedEvent(eventID, targetMessageID string) *larkim.P2MessageReactionCreatedV1 {
	return &larkim.P2MessageReactionCreatedV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    eventID,
				EventType:  "im.message.reaction.created_v1",
				CreateTime: "1710000002000",
			},
		},
		Event: &larkim.P2MessageReactionCreatedV1Data{
			MessageId:    stringRef(targetMessageID),
			ReactionType: &larkim.Emoji{EmojiType: stringRef("ThumbsUp")},
			UserId:       &larkim.UserId{OpenId: stringRef("ou_user")},
		},
	}
}
