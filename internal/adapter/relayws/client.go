package relayws

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
	outbox  chan agentproto.Envelope
	closed  chan struct{}
	closeMu sync.Once

	rawLogger *debuglog.RawLogger
}

func NewClient(url string, hello agentproto.Hello, callbacks ClientCallbacks) *Client {
	if hello.Protocol == "" {
		hello.Protocol = agentproto.WireProtocol
	}
	return &Client{
		url:       normalizeRelayURL(url),
		hello:     hello,
		callbacks: callbacks,
		outbox:    make(chan agentproto.Envelope, 512),
		closed:    make(chan struct{}),
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
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/ws/agent"
	}
	return parsed.String()
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
	return c.enqueue(agentproto.Envelope{
		Type: agentproto.EnvelopeEventBatch,
		EventBatch: &agentproto.EventBatch{
			InstanceID: c.hello.Instance.InstanceID,
			Events:     events,
		},
	})
}

func (c *Client) SendCommandAck(ack agentproto.CommandAck) error {
	if ack.InstanceID == "" {
		ack.InstanceID = c.hello.Instance.InstanceID
	}
	return c.enqueue(agentproto.Envelope{
		Type:       agentproto.EnvelopeCommandAck,
		CommandAck: &ack,
	})
}

func (c *Client) enqueue(envelope agentproto.Envelope) error {
	select {
	case <-c.closed:
		return context.Canceled
	default:
	}
	select {
	case c.outbox <- envelope:
		return nil
	default:
		return errors.New("relay client outbox full")
	}
}

func (c *Client) RunOnce(ctx context.Context) error {
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
		for {
			select {
			case <-ctx.Done():
				writeErr <- ctx.Err()
				return
			case <-c.closed:
				writeErr <- context.Canceled
				return
			case envelope := <-c.outbox:
				payload, err := agentproto.MarshalEnvelope(envelope)
				if err != nil {
					writeErr <- err
					return
				}
				c.logRaw("out", payload, envelope.Type, envelopeInstanceID(envelope, c.hello.Instance.InstanceID), envelopeCommandID(envelope))
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					writeErr <- err
					return
				}
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
