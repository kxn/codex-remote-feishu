package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestShouldAcknowledgeGatewayActionImmediately(t *testing.T) {
	testCases := []struct {
		name   string
		action control.Action
		want   bool
	}{
		{name: "list instances", action: control.Action{Kind: control.ActionListInstances}, want: true},
		{name: "debug command", action: control.Action{Kind: control.ActionDebugCommand}, want: true},
		{name: "use thread button", action: control.Action{Kind: control.ActionUseThread}, want: true},
		{name: "text message", action: control.Action{Kind: control.ActionTextMessage}, want: false},
		{name: "image message", action: control.Action{Kind: control.ActionImageMessage}, want: false},
		{name: "file message", action: control.Action{Kind: control.ActionFileMessage}, want: false},
		{name: "reaction created", action: control.Action{Kind: control.ActionReactionCreated}, want: false},
		{name: "message recalled", action: control.Action{Kind: control.ActionMessageRecalled}, want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAcknowledgeGatewayActionImmediately(tc.action); got != tc.want {
				t.Fatalf("shouldAcknowledgeGatewayActionImmediately(%s) = %t, want %t", tc.action.Kind, got, tc.want)
			}
		})
	}
}

func TestHandleGatewayEventActionReturnsImmediatelyForMenuAction(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	handler := func(context.Context, control.Action) *ActionResult {
		close(started)
		<-release
		close(done)
		return nil
	}

	begin := time.Now()
	if err := handleGatewayEventAction(context.Background(), control.Action{Kind: control.ActionListInstances}, handler); err != nil {
		t.Fatalf("handleGatewayEventAction returned error: %v", err)
	}
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Fatalf("expected immediate ack for menu action, took %s", elapsed)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to start")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected background handler to finish")
	}
}

func TestLiveGatewayCommandMessageAcksBeforeHandlerCompletes(t *testing.T) {
	holdConn := make(chan struct{})
	defer close(holdConn)

	server, responseCh := newGatewayAckTestServer(t, messageEventPayload(t, "/admin web"), holdConn)
	defer server.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	gateway := NewLiveGateway(LiveGatewayConfig{
		GatewayID: "app-1",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.Start(ctx, func(context.Context, control.Action) *ActionResult {
			close(started)
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	var response gatewayAckResponse
	select {
	case response = <-responseCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway ack")
	}
	if response.err != nil {
		t.Fatalf("gateway response failed: %v", response.err)
	}
	if response.elapsed > 200*time.Millisecond {
		t.Fatalf("expected immediate command ack, took %s", response.elapsed)
	}
	if got := response.headers.GetString(larkws.HeaderBizRt); strings.TrimSpace(got) == "" {
		t.Fatalf("expected %s header in response, got headers=%#v", larkws.HeaderBizRt, response.headers)
	}

	close(release)
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("gateway.Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway shutdown")
	}
}

func TestLiveGatewayPlainTextAcksBeforeQueuedHandlerCompletes(t *testing.T) {
	holdConn := make(chan struct{})
	defer close(holdConn)

	server, responseCh := newGatewayAckTestServer(t, messageEventPayload(t, "你好"), holdConn)
	defer server.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	gateway := NewLiveGateway(LiveGatewayConfig{
		GatewayID: "app-1",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
		Domain:    server.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.Start(ctx, func(context.Context, control.Action) *ActionResult {
			close(started)
			<-release
			return nil
		})
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for handler to start")
	}

	select {
	case response := <-responseCh:
		if response.err != nil {
			t.Fatalf("gateway response failed: %v", response.err)
		}
		if response.elapsed > 200*time.Millisecond {
			t.Fatalf("expected immediate plain text ack, took %s", response.elapsed)
		}
		if got := response.headers.GetString(larkws.HeaderBizRt); strings.TrimSpace(got) == "" {
			t.Fatalf("expected %s header in response, got headers=%#v", larkws.HeaderBizRt, response.headers)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway ack before queued handler completed")
	}

	close(release)

	select {
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("gateway.Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for gateway shutdown")
	}
}

type gatewayAckResponse struct {
	headers larkws.Headers
	elapsed time.Duration
	err     error
}

func newGatewayAckTestServer(t *testing.T, payload []byte, holdConn <-chan struct{}) (*httptest.Server, <-chan gatewayAckResponse) {
	t.Helper()

	responseCh := make(chan gatewayAckResponse, 1)
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(mux)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?service_id=1&device_id=test"

	mux.HandleFunc(larkws.GenEndpointUri, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(larkws.EndpointResp{
			Code: larkws.OK,
			Data: &larkws.Endpoint{Url: wsURL},
		})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		frame := larkws.Frame{
			Method:  int32(larkws.FrameTypeData),
			Service: 1,
			Headers: larkws.Headers{
				{Key: larkws.HeaderType, Value: string(larkws.MessageTypeEvent)},
				{Key: larkws.HeaderMessageID, Value: "ws-msg-1"},
				{Key: larkws.HeaderSeq, Value: "0"},
				{Key: larkws.HeaderSum, Value: "1"},
				{Key: larkws.HeaderTraceID, Value: "trace-1"},
			},
			Payload: payload,
		}
		wire, err := frame.Marshal()
		if err != nil {
			responseCh <- gatewayAckResponse{err: err}
			return
		}
		start := time.Now()
		if err := conn.WriteMessage(websocket.BinaryMessage, wire); err != nil {
			responseCh <- gatewayAckResponse{err: err}
			return
		}
		_, responseWire, err := conn.ReadMessage()
		if err != nil {
			responseCh <- gatewayAckResponse{err: err}
			return
		}
		var responseFrame larkws.Frame
		if err := responseFrame.Unmarshal(responseWire); err != nil {
			responseCh <- gatewayAckResponse{err: err}
			return
		}
		responseCh <- gatewayAckResponse{
			headers: larkws.Headers(responseFrame.Headers),
			elapsed: time.Since(start),
		}
		<-holdConn
	})

	return server, responseCh
}

func messageEventPayload(t *testing.T, text string) []byte {
	t.Helper()

	payload, err := json.Marshal(&larkim.P2MessageReceiveV1{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{
				EventID:    "evt-msg-1",
				EventType:  "im.message.receive_v1",
				CreateTime: "1710000000000",
			},
		},
		Event: &larkim.P2MessageReceiveV1Data{
			Sender: &larkim.EventSender{
				SenderId: &larkim.UserId{OpenId: stringRef("ou_user")},
			},
			Message: &larkim.EventMessage{
				MessageId:   stringRef("om-msg-1"),
				ChatId:      stringRef("oc_chat"),
				ChatType:    stringRef("p2p"),
				MessageType: stringRef("text"),
				Content:     stringRef(`{"text":"` + text + `"}`),
				CreateTime:  stringRef("1710000001000"),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal message event payload: %v", err)
	}
	return payload
}
