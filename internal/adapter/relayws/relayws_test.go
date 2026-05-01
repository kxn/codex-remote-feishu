package relayws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"

	"github.com/gorilla/websocket"
)

func TestClientServerCommandAndEventFlow(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 1)
	commandCh := make(chan agentproto.Command, 1)

	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
		},
	})
	defer server.Close()

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:  "inst-1",
			DisplayName: "droid",
		},
	}, ClientCallbacks{
		OnCommand: func(_ context.Context, command agentproto.Command) error {
			commandCh <- command
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = client.Run(ctx)
	}()
	defer client.Close()

	select {
	case hello := <-helloCh:
		if hello.Instance.InstanceID != "inst-1" {
			t.Fatalf("unexpected hello: %#v", hello)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello")
	}

	if err := server.SendCommand("inst-1", agentproto.Command{
		CommandID: "cmd-1",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send command: %v", err)
	}

	select {
	case command := <-commandCh:
		if command.Kind != agentproto.CommandThreadsRefresh {
			t.Fatalf("unexpected command: %#v", command)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for command")
	}

	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-1"}}); err != nil {
		t.Fatalf("send events: %v", err)
	}
	select {
	case events := <-eventsCh:
		if len(events) != 1 || events[0].Kind != agentproto.EventThreadFocused {
			t.Fatalf("unexpected events: %#v", events)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for events")
	}
}

func TestClientWaitForOutboundIdleDrainsQueuedEvents(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 1)
	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID: "inst-idle",
		},
	}, ClientCallbacks{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = client.Run(ctx)
	}()
	defer client.Close()

	select {
	case <-helloCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello")
	}

	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed"}}); err != nil {
		t.Fatalf("SendEvents: %v", err)
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer drainCancel()
	if err := client.WaitForOutboundIdle(drainCtx); err != nil {
		t.Fatalf("WaitForOutboundIdle: %v", err)
	}

	select {
	case events := <-eventsCh:
		if len(events) != 1 || events[0].Kind != agentproto.EventTurnCompleted {
			t.Fatalf("unexpected drained events: %#v", events)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for drained events")
	}
}

func TestNewClientNormalizesRelayURL(t *testing.T) {
	client := NewClient("ws://relay.test?token=abc", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{})
	if client.url != "ws://relay.test/ws/agent?token=abc" {
		t.Fatalf("client url = %q", client.url)
	}
}

func TestClientNextOutboundPrioritizesControlQueue(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{}, 4, 4)

	data := queuedEnvelope{
		epoch: 1,
		envelope: agentproto.Envelope{
			Type: agentproto.EnvelopeEventBatch,
			EventBatch: &agentproto.EventBatch{
				InstanceID: "inst-1",
				Events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "item-1", Delta: "data"}},
			},
		},
	}
	control := queuedEnvelope{
		epoch: 1,
		envelope: agentproto.Envelope{
			Type: agentproto.EnvelopeCommandAck,
			CommandAck: &agentproto.CommandAck{
				InstanceID: "inst-1",
				CommandID:  "cmd-1",
				Accepted:   true,
			},
		},
	}
	if err := client.enqueue(&client.dataOutbox, client.dataMax, data, errors.New("test data queue full")); err != nil {
		t.Fatalf("enqueue data: %v", err)
	}
	if err := client.enqueue(&client.controlOutbox, client.controlMax, control, errors.New("test control queue full")); err != nil {
		t.Fatalf("enqueue control: %v", err)
	}

	item, pendingData, err := client.nextOutbound(context.Background(), nil)
	if err != nil {
		t.Fatalf("nextOutbound: %v", err)
	}
	if pendingData != nil {
		t.Fatalf("expected no buffered data when control already queued, got %#v", pendingData)
	}
	if item.envelope.Type != agentproto.EnvelopeCommandAck {
		t.Fatalf("expected control item first, got %#v", item)
	}
	next, pendingData, err := client.nextOutbound(context.Background(), nil)
	if err != nil {
		t.Fatalf("nextOutbound second: %v", err)
	}
	if pendingData != nil {
		t.Fatalf("expected no buffered data after consuming queue, got %#v", pendingData)
	}
	if next.envelope.Type != agentproto.EnvelopeEventBatch {
		t.Fatalf("expected queued data after control, got %#v", next)
	}
}

func TestClientNextOutboundPrefersControlOverBufferedData(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{}, 4, 4)

	pendingData := &queuedEnvelope{
		epoch: 1,
		envelope: agentproto.Envelope{
			Type: agentproto.EnvelopeEventBatch,
			EventBatch: &agentproto.EventBatch{
				InstanceID: "inst-1",
				Events:     []agentproto.Event{{Kind: agentproto.EventItemDelta, ItemID: "item-1", Delta: "data"}},
			},
		},
	}
	if err := client.enqueue(&client.controlOutbox, client.controlMax, queuedEnvelope{
		epoch: 1,
		envelope: agentproto.Envelope{
			Type:       agentproto.EnvelopeCommandAck,
			CommandAck: &agentproto.CommandAck{InstanceID: "inst-1", CommandID: "cmd-1", Accepted: true},
		},
	}, errors.New("test control queue full")); err != nil {
		t.Fatalf("enqueue control: %v", err)
	}

	item, nextPending, err := client.nextOutbound(context.Background(), pendingData)
	if err != nil {
		t.Fatalf("nextOutbound: %v", err)
	}
	if item.envelope.Type != agentproto.EnvelopeCommandAck {
		t.Fatalf("expected control item to preempt buffered data, got %#v", item)
	}
	if nextPending == nil || nextPending.envelope.Type != agentproto.EnvelopeEventBatch {
		t.Fatalf("expected buffered data to stay pending, got %#v", nextPending)
	}

	item, nextPending, err = client.nextOutbound(context.Background(), nextPending)
	if err != nil {
		t.Fatalf("nextOutbound buffered data: %v", err)
	}
	if nextPending != nil {
		t.Fatalf("expected buffered data to drain, got %#v", nextPending)
	}
	if item.envelope.Type != agentproto.EnvelopeEventBatch {
		t.Fatalf("expected buffered data after control, got %#v", item)
	}
}

func TestClientSendersTagCurrentEpochAndAllowUnboundWork(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{}, 4, 4)

	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-1"}}); err != nil {
		t.Fatalf("SendEvents pre-connect: %v", err)
	}
	first, pending, err := client.nextOutbound(context.Background(), nil)
	if err != nil {
		t.Fatalf("nextOutbound pre-connect data: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected no pending data after first dequeue, got %#v", pending)
	}
	if first.epoch != 0 {
		t.Fatalf("expected pre-connect event batch to stay unbound, got %#v", first)
	}

	atomic.StoreUint64(&client.epoch, 3)
	if err := client.SendCommandAck(agentproto.CommandAck{CommandID: "cmd-1", Accepted: true}); err != nil {
		t.Fatalf("SendCommandAck: %v", err)
	}
	ack, pending, err := client.nextOutbound(context.Background(), nil)
	if err != nil {
		t.Fatalf("nextOutbound command ack: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected no pending data after ack dequeue, got %#v", pending)
	}
	if ack.epoch != 0 {
		t.Fatalf("expected command ack to stay unbound across reconnects, got %#v", ack)
	}
}

func TestClientCommandAckSurvivesEpochAdvance(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{}, 4, 4)

	atomic.StoreUint64(&client.epoch, 3)
	if err := client.SendCommandAck(agentproto.CommandAck{CommandID: "cmd-1", Accepted: true}); err != nil {
		t.Fatalf("SendCommandAck: %v", err)
	}

	item, pending, err := client.nextOutbound(context.Background(), nil)
	if err != nil {
		t.Fatalf("nextOutbound: %v", err)
	}
	if pending != nil {
		t.Fatalf("expected no pending data after ack dequeue, got %#v", pending)
	}
	if item.envelope.Type != agentproto.EnvelopeCommandAck {
		t.Fatalf("expected command ack, got %#v", item)
	}
	if item.epoch != 0 {
		t.Fatalf("expected unbound command ack, got %#v", item)
	}
	if !matchesConnectionEpoch(item.epoch, 4) {
		t.Fatalf("expected command ack to remain sendable after reconnect, got %#v", item)
	}
}

func TestClientSendersRejectWhenDynamicQueueLimitReached(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{InstanceID: "inst-1"},
	}, ClientCallbacks{}, 1, 1)

	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-1"}}); err != nil {
		t.Fatalf("SendEvents first: %v", err)
	}
	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-2"}}); err == nil || err.Error() != "relay client outbox full" {
		t.Fatalf("expected data queue full error, got %v", err)
	}
	if err := client.SendCommandAck(agentproto.CommandAck{CommandID: "cmd-1", Accepted: true}); err != nil {
		t.Fatalf("SendCommandAck first: %v", err)
	}
	if err := client.SendCommandAck(agentproto.CommandAck{CommandID: "cmd-2", Accepted: true}); err == nil || err.Error() != "relay client control outbox full" {
		t.Fatalf("expected control queue full error, got %v", err)
	}
}

func TestMatchesConnectionEpoch(t *testing.T) {
	cases := []struct {
		name            string
		enqueuedEpoch   uint64
		connectionEpoch uint64
		want            bool
	}{
		{name: "unbound work allowed", enqueuedEpoch: 0, connectionEpoch: 2, want: true},
		{name: "current epoch allowed", enqueuedEpoch: 2, connectionEpoch: 2, want: true},
		{name: "stale epoch rejected", enqueuedEpoch: 1, connectionEpoch: 2, want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesConnectionEpoch(tt.enqueuedEpoch, tt.connectionEpoch); got != tt.want {
				t.Fatalf("matchesConnectionEpoch(%d, %d) = %t, want %t", tt.enqueuedEpoch, tt.connectionEpoch, got, tt.want)
			}
		})
	}
}

func TestClientNormalizesDefaultRelayPath(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
	})
	defer server.Close()

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:  "inst-normalized",
			DisplayName: "droid",
		},
	}, ClientCallbacks{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = client.Run(ctx)
	}()
	defer client.Close()

	select {
	case hello := <-helloCh:
		if hello.Instance.InstanceID != "inst-normalized" {
			t.Fatalf("unexpected hello: %#v", hello)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello over normalized path")
	}
}

func TestClientReceivesWelcomeServerIdentity(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	welcomeCh := make(chan agentproto.Welcome, 1)
	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
	})
	server.SetServerIdentity(agentproto.ServerIdentity{
		BinaryIdentity: agentproto.BinaryIdentity{
			Product:          "codex-remote",
			Version:          "1.0.0",
			BuildFingerprint: "fp-1",
			BinaryPath:       "/tmp/codex-remote",
		},
		PID: 12345,
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID: "inst-welcome",
		},
	}, ClientCallbacks{
		OnWelcome: func(_ context.Context, welcome agentproto.Welcome) error {
			welcomeCh <- welcome
			return context.Canceled
		},
	})

	if err := client.Run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("client run: %v", err)
	}

	select {
	case hello := <-helloCh:
		if hello.Instance.InstanceID != "inst-welcome" {
			t.Fatalf("unexpected hello: %#v", hello)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello")
	}

	select {
	case welcome := <-welcomeCh:
		if welcome.Server == nil || welcome.Server.BuildFingerprint != "fp-1" || welcome.Server.PID != 12345 {
			t.Fatalf("unexpected welcome server identity: %#v", welcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for welcome")
	}
}

func TestClientReportsErrorEnvelope(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			t.Fatalf("read hello: %v", err)
		}
		welcome, err := agentproto.MarshalEnvelope(agentproto.Envelope{
			Type: agentproto.EnvelopeWelcome,
			Welcome: &agentproto.Welcome{
				Protocol:   agentproto.WireProtocol,
				ServerTime: time.Now(),
			},
		})
		if err != nil {
			t.Fatalf("marshal welcome: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, welcome); err != nil {
			t.Fatalf("write welcome: %v", err)
		}
		problem := agentproto.ErrorInfo{
			Code:      "bad_envelope",
			Layer:     "relayws_server",
			Stage:     "decode_envelope",
			Message:   "relay 收到无法解析的 websocket envelope。",
			Details:   "unexpected token",
			Retryable: true,
		}
		payload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
			Type: agentproto.EnvelopeError,
			Error: &agentproto.ErrorEnvelope{
				Code:    problem.Code,
				Message: problem.Message,
				Problem: &problem,
			},
		})
		if err != nil {
			t.Fatalf("marshal error envelope: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			t.Fatalf("write error envelope: %v", err)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	problemCh := make(chan agentproto.ErrorInfo, 1)
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID: "inst-error",
		},
	}, ClientCallbacks{
		OnError: func(_ context.Context, problem agentproto.ErrorInfo) error {
			problemCh <- problem
			return context.Canceled
		},
	})

	if err := client.Run(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("client run: %v", err)
	}
	select {
	case problem := <-problemCh:
		if problem.Layer != "relayws_server" || problem.Stage != "decode_envelope" || problem.Details != "unexpected token" {
			t.Fatalf("unexpected reported problem: %#v", problem)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for error envelope callback")
	}
}

func TestClientSendsStructuredRejectedAckAndKeepsConnection(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	ackCh := make(chan agentproto.CommandAck, 2)
	commandCh := make(chan string, 2)

	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnCommandAck: func(_ context.Context, _ ConnectionMeta, _ string, ack agentproto.CommandAck) {
			ackCh <- ack
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID: "inst-ack",
		},
	}, ClientCallbacks{
		OnCommand: func(_ context.Context, command agentproto.Command) error {
			commandCh <- command.CommandID
			if command.CommandID == "cmd-bad" {
				return agentproto.ErrorInfo{
					Code:      "translate_command_failed",
					Layer:     "wrapper",
					Stage:     "translate_command",
					Message:   "wrapper 无法把 relay 命令转换成 Codex 请求。",
					Details:   "unknown model",
					CommandID: command.CommandID,
				}
			}
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = client.Run(ctx)
	}()
	defer client.Close()

	select {
	case <-helloCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello")
	}

	if err := server.SendCommand("inst-ack", agentproto.Command{CommandID: "cmd-bad", Kind: agentproto.CommandPromptSend}); err != nil {
		t.Fatalf("send bad command: %v", err)
	}
	if err := server.SendCommand("inst-ack", agentproto.Command{CommandID: "cmd-good", Kind: agentproto.CommandThreadsRefresh}); err != nil {
		t.Fatalf("send good command: %v", err)
	}

	for _, wantCommand := range []string{"cmd-bad", "cmd-good"} {
		select {
		case got := <-commandCh:
			if got != wantCommand {
				t.Fatalf("command callback order mismatch: got %s want %s", got, wantCommand)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for command callback %s", wantCommand)
		}
	}

	var badAck, goodAck agentproto.CommandAck
	for i := 0; i < 2; i++ {
		select {
		case ack := <-ackCh:
			switch ack.CommandID {
			case "cmd-bad":
				badAck = ack
			case "cmd-good":
				goodAck = ack
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for command ack")
		}
	}
	if badAck.Accepted || badAck.Problem == nil || badAck.Problem.Stage != "translate_command" {
		t.Fatalf("expected structured rejected ack, got %#v", badAck)
	}
	if !goodAck.Accepted {
		t.Fatalf("expected follow-up command to still be accepted, got %#v", goodAck)
	}
}

func TestClientCommandAckRetriesAfterWriteFailureOnReconnect(t *testing.T) {
	client := newClientWithQueueSizes("ws://relay.test/ws/agent", agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID: "inst-retry-ack",
		},
	}, ClientCallbacks{}, 4, 4)

	firstConn := newStubRelayConn(t)
	firstConn.readErr = errors.New("first connection closed")
	firstConn.writeErrAtCall = 2 // hello succeeds; ack write fails
	secondConn := newStubRelayConn(t)
	secondConn.readErr = context.Canceled

	dialCalls := make(chan int, 4)
	var dialCount atomic.Int32
	client.dialRelayConn = func(ctx context.Context, _ string, _ http.Header) (relayConn, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		call := int(dialCount.Add(1))
		dialCalls <- call
		switch call {
		case 1:
			return firstConn, nil
		case 2:
			return secondConn, nil
		default:
			return nil, context.Canceled
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- client.Run(ctx)
	}()
	defer client.Close()

	select {
	case call := <-dialCalls:
		if call != 1 {
			t.Fatalf("expected first dial call, got %d", call)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for first dial")
	}

	if err := client.SendCommandAck(agentproto.CommandAck{CommandID: "cmd-retry-ack", Accepted: true}); err != nil {
		t.Fatalf("SendCommandAck: %v", err)
	}

	select {
	case <-dialCalls:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reconnect dial")
	}

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer drainCancel()
	if err := client.WaitForOutboundIdle(drainCtx); err != nil {
		t.Fatalf("WaitForOutboundIdle: %v", err)
	}

	cancel()
	select {
	case err := <-runErrCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client run returned %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for client shutdown")
	}

	firstConn.mu.Lock()
	firstWrites := append([][]byte(nil), firstConn.writes...)
	firstConn.mu.Unlock()
	secondConn.mu.Lock()
	secondWrites := append([][]byte(nil), secondConn.writes...)
	secondConn.mu.Unlock()

	if len(firstWrites) != 1 {
		t.Fatalf("expected first connection to persist only hello before failing ack write, got %d writes", len(firstWrites))
	}
	if envelopeTypeFromPayload(t, firstWrites[0]) != agentproto.EnvelopeHello {
		t.Fatalf("expected first connection first write to be hello, got %s", envelopeTypeFromPayload(t, firstWrites[0]))
	}
	if len(secondWrites) != 2 {
		t.Fatalf("expected second connection to write hello and retried ack, got %d writes", len(secondWrites))
	}
	if envelopeTypeFromPayload(t, secondWrites[0]) != agentproto.EnvelopeHello {
		t.Fatalf("expected second connection first write to be hello, got %s", envelopeTypeFromPayload(t, secondWrites[0]))
	}
	if envelopeTypeFromPayload(t, secondWrites[1]) != agentproto.EnvelopeCommandAck {
		t.Fatalf("expected second connection second write to be command ack, got %s", envelopeTypeFromPayload(t, secondWrites[1]))
	}
	ack := mustCommandAckFromPayload(t, secondWrites[1])
	if ack.CommandID != "cmd-retry-ack" || !ack.Accepted {
		t.Fatalf("unexpected retried command ack: %#v", ack)
	}
}

func TestServerSendsWelcomeBeforeOnHelloCommand(t *testing.T) {
	commandErrCh := make(chan error, 1)
	var server *Server
	server = NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			commandErrCh <- server.SendCommand(hello.Instance.InstanceID, agentproto.Command{
				CommandID: "cmd-1",
				Kind:      agentproto.CommandThreadsRefresh,
			})
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	helloPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeHello,
		Hello: &agentproto.Hello{
			Protocol: agentproto.WireProtocol,
			Instance: agentproto.InstanceHello{InstanceID: "inst-order"},
		},
	})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	for i, want := range []agentproto.EnvelopeType{agentproto.EnvelopeWelcome, agentproto.EnvelopeCommand} {
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read frame %d: %v", i, err)
		}
		envelope, err := agentproto.UnmarshalEnvelope(raw)
		if err != nil {
			t.Fatalf("unmarshal frame %d: %v", i, err)
		}
		if envelope.Type != want {
			t.Fatalf("frame %d type = %s, want %s", i, envelope.Type, want)
		}
	}

	select {
	case err := <-commandErrCh:
		if err != nil {
			t.Fatalf("send command after welcome: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for command callback")
	}
}

type stubRelayConn struct {
	t              *testing.T
	mu             sync.Mutex
	writes         [][]byte
	writeCalls     int
	writeErrAtCall int
	writeErr       error
	readErr        error
	readBlocks     bool
	readReady      chan struct{}
	closed         chan struct{}
}

func newStubRelayConn(t *testing.T) *stubRelayConn {
	t.Helper()
	return &stubRelayConn{
		t:       t,
		readErr: context.Canceled,
		closed:  make(chan struct{}),
	}
}

func (c *stubRelayConn) WriteMessage(_ int, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeCalls++
	if c.writeErrAtCall > 0 && c.writeCalls == c.writeErrAtCall {
		if c.writeErr != nil {
			return c.writeErr
		}
		return errors.New("stub write failed")
	}
	c.writes = append(c.writes, append([]byte(nil), payload...))
	return nil
}

func (c *stubRelayConn) ReadMessage() (int, []byte, error) {
	if c.readBlocks {
		select {
		case <-c.closed:
			return 0, nil, context.Canceled
		case <-c.readReady:
		}
	}
	if c.readErr == nil {
		return 0, nil, context.Canceled
	}
	return 0, nil, c.readErr
}

func (c *stubRelayConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func envelopeTypeFromPayload(t *testing.T, payload []byte) agentproto.EnvelopeType {
	t.Helper()
	envelope, err := agentproto.UnmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return envelope.Type
}

func mustCommandAckFromPayload(t *testing.T, payload []byte) agentproto.CommandAck {
	t.Helper()
	envelope, err := agentproto.UnmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unmarshal command ack envelope: %v", err)
	}
	if envelope.CommandAck == nil {
		t.Fatalf("expected command ack payload, got %#v", envelope)
	}
	return *envelope.CommandAck
}

func TestRelayWSRawLoggerCapturesIncomingAndOutgoingEnvelopes(t *testing.T) {
	rawPath := filepath.Join(t.TempDir(), "relay-raw.ndjson")
	rawLogger, err := debuglog.OpenRaw(rawPath, "daemon", "", 1)
	if err != nil {
		t.Fatalf("OpenRaw: %v", err)
	}
	defer rawLogger.Close()

	helloCh := make(chan agentproto.Hello, 1)
	eventsCh := make(chan []agentproto.Event, 1)
	commandCh := make(chan agentproto.Command, 1)

	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
		OnEvents: func(_ context.Context, _ ConnectionMeta, _ string, events []agentproto.Event) {
			eventsCh <- events
		},
	})
	server.SetRawLogger(rawLogger)
	defer server.Close()

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	client := NewClient(wsURL, agentproto.Hello{
		Protocol: agentproto.WireProtocol,
		Instance: agentproto.InstanceHello{
			InstanceID:  "inst-raw",
			DisplayName: "raw",
		},
	}, ClientCallbacks{
		OnCommand: func(_ context.Context, command agentproto.Command) error {
			commandCh <- command
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = client.Run(ctx)
	}()
	defer client.Close()

	select {
	case <-helloCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello")
	}

	if err := server.SendCommand("inst-raw", agentproto.Command{
		CommandID: "cmd-raw",
		Kind:      agentproto.CommandThreadsRefresh,
	}); err != nil {
		t.Fatalf("send command: %v", err)
	}

	select {
	case <-commandCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for command")
	}

	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-1"}}); err != nil {
		t.Fatalf("send events: %v", err)
	}
	select {
	case <-eventsCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected multiple raw log entries, got %d: %s", len(lines), raw)
	}
	var sawHello, sawWelcome, sawCommand, sawEventBatch bool
	for _, line := range lines {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("unmarshal raw line: %v\nline=%s", err, line)
		}
		if payload["channel"] != "relay.ws" {
			t.Fatalf("unexpected channel: %#v", payload)
		}
		switch payload["envelopeType"] {
		case string(agentproto.EnvelopeHello):
			sawHello = true
		case string(agentproto.EnvelopeWelcome):
			sawWelcome = true
		case string(agentproto.EnvelopeCommand):
			sawCommand = true
		case string(agentproto.EnvelopeEventBatch):
			sawEventBatch = true
		}
	}
	if !sawHello || !sawWelcome || !sawCommand || !sawEventBatch {
		t.Fatalf("missing expected envelope types in raw log: %s", raw)
	}
}

func TestServerProbeHelloReturnsWelcomeWithoutRegisteringInstance(t *testing.T) {
	helloCh := make(chan agentproto.Hello, 1)
	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, _ ConnectionMeta, hello agentproto.Hello) {
			helloCh <- hello
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	helloPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeHello,
		Hello: &agentproto.Hello{
			Protocol: agentproto.WireProtocol,
			Probe:    true,
			Instance: agentproto.InstanceHello{InstanceID: "probe"},
		},
	})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	envelope, err := agentproto.UnmarshalEnvelope(raw)
	if err != nil {
		t.Fatalf("unmarshal welcome: %v", err)
	}
	if envelope.Type != agentproto.EnvelopeWelcome {
		t.Fatalf("first frame type = %s, want welcome", envelope.Type)
	}

	select {
	case hello := <-helloCh:
		t.Fatalf("probe hello should not invoke OnHello, got %#v", hello)
	case <-time.After(200 * time.Millisecond):
	}

	if err := server.SendCommand("probe", agentproto.Command{CommandID: "cmd-probe", Kind: agentproto.CommandThreadsRefresh}); err == nil {
		t.Fatal("expected probe instance to be offline")
	}
}

func TestServerConnectionMetaAndTargetedClose(t *testing.T) {
	helloMetaCh := make(chan ConnectionMeta, 1)
	disconnectMetaCh := make(chan ConnectionMeta, 1)
	server := NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, meta ConnectionMeta, _ agentproto.Hello) {
			helloMetaCh <- meta
		},
		OnDisconnect: func(_ context.Context, meta ConnectionMeta, _ string) {
			disconnectMetaCh <- meta
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	helloPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeHello,
		Hello: &agentproto.Hello{
			Protocol: agentproto.WireProtocol,
			Instance: agentproto.InstanceHello{InstanceID: "inst-close"},
		},
	})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	var meta ConnectionMeta
	select {
	case meta = <-helloMetaCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello callback")
	}
	if meta.ConnectionID == 0 {
		t.Fatalf("expected non-zero connection id, got %#v", meta)
	}
	if !server.CloseConnection("inst-close", meta.ConnectionID) {
		t.Fatal("expected targeted close to succeed")
	}
	select {
	case got := <-disconnectMetaCh:
		if got.ConnectionID != meta.ConnectionID {
			t.Fatalf("disconnect connection id = %d, want %d", got.ConnectionID, meta.ConnectionID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for disconnect callback")
	}
}

func TestServerCallbackContextSurvivesAfterUpgradeHandlerReturns(t *testing.T) {
	callbackErrCh := make(chan error, 2)
	server := NewServer(ServerCallbacks{
		OnHello: func(ctx context.Context, _ ConnectionMeta, _ agentproto.Hello) {
			callbackErrCh <- ctx.Err()
		},
		OnEvents: func(ctx context.Context, _ ConnectionMeta, _ string, _ []agentproto.Event) {
			callbackErrCh <- ctx.Err()
		},
	})
	defer server.Close()

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	helloPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeHello,
		Hello: &agentproto.Hello{
			Protocol: agentproto.WireProtocol,
			Instance: agentproto.InstanceHello{InstanceID: "inst-ctx"},
		},
	})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, helloPayload); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read welcome: %v", err)
	}

	select {
	case err := <-callbackErrCh:
		if err != nil {
			t.Fatalf("expected hello callback context to be alive, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for hello callback")
	}

	eventPayload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeEventBatch,
		EventBatch: &agentproto.EventBatch{
			InstanceID: "inst-ctx",
			Events:     []agentproto.Event{{Kind: agentproto.EventThreadFocused, ThreadID: "thread-1"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, eventPayload); err != nil {
		t.Fatalf("write events: %v", err)
	}

	select {
	case err := <-callbackErrCh:
		if err != nil {
			t.Fatalf("expected event callback context to be alive, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event callback")
	}
}
