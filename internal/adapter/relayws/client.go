package relayws

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/relayurl"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"

	"github.com/gorilla/websocket"
)

type ClientCallbacks struct {
	OnCommand func(context.Context, agentproto.Command) error
	OnWelcome func(context.Context, agentproto.Welcome) error
	OnConnect func(context.Context) error
	OnError   func(context.Context, agentproto.ErrorInfo) error
}

type Client struct {
	url       string
	hello     agentproto.Hello
	callbacks ClientCallbacks

	mu      sync.RWMutex
	conn    *websocket.Conn
	epoch   uint64
	closed  chan struct{}
	closeMu sync.Once

	outboxMu      sync.Mutex
	controlMax    int
	dataMax       int
	controlOutbox []queuedEnvelope
	dataOutbox    []queuedEnvelope
	outboundReady chan struct{}

	rawLogger *debuglog.RawLogger
}

type queuedEnvelope struct {
	envelope agentproto.Envelope
	epoch    uint64
}

const (
	defaultControlOutboxCapacity = 512
	defaultDataOutboxCapacity    = 16 * 1024
)

func NewClient(url string, hello agentproto.Hello, callbacks ClientCallbacks) *Client {
	return newClientWithQueueSizes(url, hello, callbacks, defaultControlOutboxCapacity, defaultDataOutboxCapacity)
}

func newClientWithQueueSizes(url string, hello agentproto.Hello, callbacks ClientCallbacks, controlCapacity, dataCapacity int) *Client {
	if hello.Protocol == "" {
		hello.Protocol = agentproto.WireProtocol
	}
	if controlCapacity <= 0 {
		controlCapacity = defaultControlOutboxCapacity
	}
	if dataCapacity <= 0 {
		dataCapacity = defaultDataOutboxCapacity
	}
	return &Client{
		url:           normalizeRelayURL(url),
		hello:         hello,
		callbacks:     callbacks,
		closed:        make(chan struct{}),
		controlMax:    controlCapacity,
		dataMax:       dataCapacity,
		outboundReady: make(chan struct{}, 1),
	}
}

func (c *Client) Run(ctx context.Context) error {
	backoff := 200 * time.Millisecond
	for {
		err := c.RunOnce(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		var fatalErr FatalError
		if errors.As(err, &fatalErr) {
			return err
		}
		log.Printf("relay client connect failed: url=%s err=%v", c.url, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

type FatalError struct {
	Err error
}

func (e FatalError) Error() string {
	if e.Err == nil {
		return "fatal relay error"
	}
	return e.Err.Error()
}

func (e FatalError) Unwrap() error {
	return e.Err
}

func normalizeRelayURL(raw string) string {
	return relayurl.NormalizeAgentURL(raw)
}

func (c *Client) Close() {
	c.closeMu.Do(func() {
		close(c.closed)
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
			c.conn = nil
		}
		c.mu.Unlock()
	})
}

func (c *Client) SetRawLogger(logger *debuglog.RawLogger) {
	c.rawLogger = logger
}

func (c *Client) SendEvents(events []agentproto.Event) error {
	if len(events) == 0 {
		return nil
	}
	return c.enqueue(&c.dataOutbox, c.dataMax, queuedEnvelope{
		epoch: atomic.LoadUint64(&c.epoch),
		envelope: agentproto.Envelope{
			Type: agentproto.EnvelopeEventBatch,
			EventBatch: &agentproto.EventBatch{
				InstanceID: c.hello.Instance.InstanceID,
				Events:     events,
			},
		},
	}, errors.New("relay client outbox full"))
}

func (c *Client) SendCommandAck(ack agentproto.CommandAck) error {
	if ack.InstanceID == "" {
		ack.InstanceID = c.hello.Instance.InstanceID
	}
	return c.enqueue(&c.controlOutbox, c.controlMax, queuedEnvelope{
		epoch: atomic.LoadUint64(&c.epoch),
		envelope: agentproto.Envelope{
			Type:       agentproto.EnvelopeCommandAck,
			CommandAck: &ack,
		},
	}, errors.New("relay client control outbox full"))
}

func (c *Client) enqueue(outbox *[]queuedEnvelope, max int, item queuedEnvelope, fullErr error) error {
	c.outboxMu.Lock()
	defer c.outboxMu.Unlock()

	select {
	case <-c.closed:
		return context.Canceled
	default:
	}
	if max > 0 && len(*outbox) >= max {
		return fullErr
	}
	*outbox = append(*outbox, item)
	select {
	case c.outboundReady <- struct{}{}:
	default:
	}
	return nil
}

func (c *Client) RunOnce(ctx context.Context) error {
	connectionEpoch := atomic.AddUint64(&c.epoch, 1)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.url, http.Header{})
	if err != nil {
		return err
	}
	defer conn.Close()

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
	}()

	helloBytes, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type:  agentproto.EnvelopeHello,
		Hello: &c.hello,
	})
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, helloBytes); err != nil {
		return err
	}
	c.logRaw("out", helloBytes, agentproto.EnvelopeHello, c.hello.Instance.InstanceID, "")

	writeErr := make(chan error, 1)
	welcomed := false
	go func() {
		var pendingData *queuedEnvelope
		for {
			item, nextPendingData, err := c.nextOutbound(ctx, pendingData)
			pendingData = nextPendingData
			if err != nil {
				writeErr <- err
				return
			}
			if !matchesConnectionEpoch(item.epoch, connectionEpoch) {
				continue
			}
			payload, err := agentproto.MarshalEnvelope(item.envelope)
			if err != nil {
				writeErr <- err
				return
			}
			c.logRaw("out", payload, item.envelope.Type, envelopeInstanceID(item.envelope, c.hello.Instance.InstanceID), envelopeCommandID(item.envelope))
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				writeErr <- err
				return
			}
		}
	}()

	for {
		select {
		case err := <-writeErr:
			return err
		default:
		}

		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		envelope, err := agentproto.UnmarshalEnvelope(raw)
		envelopeType := envelope.Type
		instanceID := envelopeInstanceID(envelope, c.hello.Instance.InstanceID)
		commandID := envelopeCommandID(envelope)
		if err != nil {
			envelopeType = ""
			instanceID = c.hello.Instance.InstanceID
			commandID = ""
		}
		c.logRaw("in", raw, envelopeType, instanceID, commandID)
		if err != nil {
			log.Printf("relay client decode failed: %v", err)
			if c.callbacks.OnError != nil {
				_ = c.callbacks.OnError(ctx, agentproto.ErrorInfo{
					Code:      "relay_client_bad_envelope",
					Layer:     "relayws_client",
					Stage:     "decode_envelope",
					Operation: "relay.read",
					Message:   "relay 返回了无法解析的 envelope。",
					Details:   err.Error(),
					Retryable: true,
				}.Normalize())
			}
			continue
		}
		switch envelope.Type {
		case agentproto.EnvelopeWelcome:
			if envelope.Welcome == nil {
				continue
			}
			welcomed = true
			if c.callbacks.OnWelcome != nil {
				if err := c.callbacks.OnWelcome(ctx, *envelope.Welcome); err != nil {
					return err
				}
			}
			if c.callbacks.OnConnect != nil {
				if err := c.callbacks.OnConnect(ctx); err != nil {
					return err
				}
			}
		case agentproto.EnvelopeCommand:
			if envelope.Command == nil {
				continue
			}
			if c.callbacks.OnCommand != nil {
				if err := c.callbacks.OnCommand(ctx, *envelope.Command); err != nil {
					problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{
						Code:             "command_rejected",
						Layer:            "wrapper",
						Stage:            "handle_command",
						Operation:        string(envelope.Command.Kind),
						Message:          "wrapper 拒绝执行 relay 命令。",
						SurfaceSessionID: envelope.Command.Origin.Surface,
						CommandID:        envelope.Command.CommandID,
						ThreadID:         envelope.Command.Target.ThreadID,
						TurnID:           envelope.Command.Target.TurnID,
					})
					_ = c.SendCommandAck(agentproto.CommandAck{
						CommandID: envelope.Command.CommandID,
						Accepted:  false,
						Error:     problem.Message,
						Problem:   &problem,
					})
					continue
				}
			}
			_ = c.SendCommandAck(agentproto.CommandAck{
				CommandID: envelope.Command.CommandID,
				Accepted:  true,
			})
		case agentproto.EnvelopeError:
			problem := relayEnvelopeProblem(envelope.Error)
			if c.callbacks.OnError != nil {
				if err := c.callbacks.OnError(ctx, problem); err != nil {
					return err
				}
			}
			if !welcomed {
				return FatalError{Err: problem}
			}
		}
	}
}

func (c *Client) nextOutbound(ctx context.Context, pendingData *queuedEnvelope) (queuedEnvelope, *queuedEnvelope, error) {
	for {
		if pendingData != nil {
			if item, ok := c.dequeueControl(); ok {
				return item, pendingData, nil
			}
			return *pendingData, nil, nil
		}
		if item, ok := c.dequeueControl(); ok {
			return item, nil, nil
		}
		if data, ok := c.dequeueData(); ok {
			if item, ok := c.dequeueControl(); ok {
				return item, &data, nil
			}
			return data, nil, nil
		}
		select {
		case <-ctx.Done():
			return queuedEnvelope{}, nil, ctx.Err()
		case <-c.closed:
			return queuedEnvelope{}, nil, context.Canceled
		case <-c.outboundReady:
		}
	}
}

func (c *Client) dequeueControl() (queuedEnvelope, bool) {
	c.outboxMu.Lock()
	defer c.outboxMu.Unlock()
	return dequeueQueuedEnvelopeLocked(&c.controlOutbox)
}

func (c *Client) dequeueData() (queuedEnvelope, bool) {
	c.outboxMu.Lock()
	defer c.outboxMu.Unlock()
	return dequeueQueuedEnvelopeLocked(&c.dataOutbox)
}

func dequeueQueuedEnvelopeLocked(queue *[]queuedEnvelope) (queuedEnvelope, bool) {
	if len(*queue) == 0 {
		return queuedEnvelope{}, false
	}
	item := (*queue)[0]
	(*queue)[0] = queuedEnvelope{}
	*queue = (*queue)[1:]
	if len(*queue) == 0 {
		*queue = nil
	}
	return item, true
}

func matchesConnectionEpoch(enqueuedEpoch, connectionEpoch uint64) bool {
	return enqueuedEpoch == 0 || enqueuedEpoch == connectionEpoch
}

func relayEnvelopeProblem(envelope *agentproto.ErrorEnvelope) agentproto.ErrorInfo {
	defaults := agentproto.ErrorInfo{
		Code:      "relay_error",
		Layer:     "relayws_server",
		Stage:     "error_envelope",
		Operation: "relay.read",
		Message:   "relay 返回了错误 envelope。",
		Retryable: true,
	}
	if envelope == nil {
		return defaults.Normalize()
	}
	defaults.Code = strings.TrimSpace(firstNonEmptyRelayString(envelope.Code, defaults.Code))
	defaults.Message = strings.TrimSpace(firstNonEmptyRelayString(envelope.Message, defaults.Message))
	if envelope.Problem == nil {
		return defaults.Normalize()
	}
	return envelope.Problem.WithDefaults(defaults)
}

func firstNonEmptyRelayString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Client) logRaw(direction string, payload []byte, envelopeType agentproto.EnvelopeType, instanceID, commandID string) {
	if c.rawLogger == nil {
		return
	}
	c.rawLogger.Log(debuglog.RawEntry{
		InstanceID:   instanceID,
		Channel:      "relay.ws",
		Direction:    direction,
		EnvelopeType: string(envelopeType),
		CommandID:    commandID,
		Frame:        payload,
	})
}

func envelopeInstanceID(envelope agentproto.Envelope, fallback string) string {
	switch envelope.Type {
	case agentproto.EnvelopeHello:
		if envelope.Hello != nil {
			return envelope.Hello.Instance.InstanceID
		}
	case agentproto.EnvelopeEventBatch:
		if envelope.EventBatch != nil {
			return envelope.EventBatch.InstanceID
		}
	case agentproto.EnvelopeCommandAck:
		if envelope.CommandAck != nil {
			return envelope.CommandAck.InstanceID
		}
	case agentproto.EnvelopeCommand:
		return fallback
	case agentproto.EnvelopeWelcome:
		return fallback
	}
	return fallback
}

func envelopeCommandID(envelope agentproto.Envelope) string {
	switch envelope.Type {
	case agentproto.EnvelopeCommand:
		if envelope.Command != nil {
			return envelope.Command.CommandID
		}
	case agentproto.EnvelopeCommandAck:
		if envelope.CommandAck != nil {
			return envelope.CommandAck.CommandID
		}
	}
	return ""
}
