package relayws

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/debuglog"

	"github.com/gorilla/websocket"
)

type ServerCallbacks struct {
	OnHello      func(context.Context, ConnectionMeta, agentproto.Hello)
	OnEvents     func(context.Context, ConnectionMeta, string, []agentproto.Event)
	OnCommandAck func(context.Context, ConnectionMeta, string, agentproto.CommandAck)
	OnDisconnect func(context.Context, ConnectionMeta, string)
}

type ConnectionMeta struct {
	ConnectionID uint64
}

type Server struct {
	upgrader  websocket.Upgrader
	callbacks ServerCallbacks
	identity  agentproto.ServerIdentity

	mu       sync.RWMutex
	conns    map[string]*serverConn
	shutdown chan struct{}
	nextConn uint64

	rawLogger *debuglog.RawLogger
}

type serverConn struct {
	id   uint64
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewServer(callbacks ServerCallbacks) *Server {
	return &Server{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		callbacks: callbacks,
		conns:     map[string]*serverConn{},
		shutdown:  make(chan struct{}),
	}
}

func (s *Server) SetServerIdentity(identity agentproto.ServerIdentity) {
	s.identity = identity
}

func (s *Server) SetRawLogger(logger *debuglog.RawLogger) {
	s.rawLogger = logger
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.serveConn(ctx, cancel, conn)
}

func (s *Server) Close() error {
	close(s.shutdown)
	s.mu.Lock()
	defer s.mu.Unlock()
	for instanceID, current := range s.conns {
		_ = current.conn.Close()
		delete(s.conns, instanceID)
	}
	return nil
}

func (s *Server) SendCommand(instanceID string, command agentproto.Command) error {
	s.mu.RLock()
	current := s.conns[instanceID]
	s.mu.RUnlock()
	if current == nil {
		return errors.New("instance offline")
	}
	payload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type:    agentproto.EnvelopeCommand,
		Command: &command,
	})
	if err != nil {
		return err
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	current.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	s.logRaw("out", payload, agentproto.EnvelopeCommand, instanceID, command.CommandID)
	return current.conn.WriteMessage(websocket.TextMessage, payload)
}

func (s *Server) CloseConnection(instanceID string, connectionID uint64) bool {
	s.mu.RLock()
	current := s.conns[instanceID]
	s.mu.RUnlock()
	if current == nil || current.id != connectionID {
		return false
	}
	_ = current.conn.Close()
	return true
}

func (s *Server) serveConn(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn) {
	defer conn.Close()
	defer cancel()
	meta := ConnectionMeta{ConnectionID: atomic.AddUint64(&s.nextConn, 1)}
	var instanceID string
	defer func() {
		if instanceID == "" {
			return
		}
		s.mu.Lock()
		if current := s.conns[instanceID]; current != nil && current.conn == conn {
			delete(s.conns, instanceID)
		}
		s.mu.Unlock()
		if s.callbacks.OnDisconnect != nil {
			s.callbacks.OnDisconnect(ctx, meta, instanceID)
		}
	}()

	for {
		messageType, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		envelope, err := agentproto.UnmarshalEnvelope(raw)
		envelopeType := envelope.Type
		currentInstanceID := instanceID
		commandID := envelopeCommandID(envelope)
		if envelope.Type == agentproto.EnvelopeHello && envelope.Hello != nil {
			currentInstanceID = envelope.Hello.Instance.InstanceID
		} else if derived := envelopeInstanceID(envelope, currentInstanceID); derived != "" {
			currentInstanceID = derived
		}
		if err != nil {
			envelopeType = ""
			commandID = ""
		}
		s.logRaw("in", raw, envelopeType, currentInstanceID, commandID)
		if err != nil {
			_ = writeError(conn, agentproto.ErrorInfo{
				Code:      "bad_envelope",
				Layer:     "relayws_server",
				Stage:     "decode_envelope",
				Operation: "relay.read",
				Message:   "relay 收到无法解析的 websocket envelope。",
				Details:   err.Error(),
				Retryable: true,
			})
			continue
		}
		switch envelope.Type {
		case agentproto.EnvelopeHello:
			if envelope.Hello == nil {
				_ = writeError(conn, agentproto.ErrorInfo{
					Code:      "bad_hello",
					Layer:     "relayws_server",
					Stage:     "hello",
					Operation: "relay.handshake",
					Message:   "relay hello 缺少负载。",
					Retryable: false,
				})
				return
			}
			hello := *envelope.Hello
			probeOnly := hello.Probe
			var current *serverConn
			if !probeOnly {
				instanceID = hello.Instance.InstanceID
				current = &serverConn{id: meta.ConnectionID, conn: conn}
				s.mu.Lock()
				if previous := s.conns[instanceID]; previous != nil && previous.conn != conn {
					_ = previous.conn.Close()
				}
				s.conns[instanceID] = current
				s.mu.Unlock()
			}
			serverIdentity := s.identity
			var serverPtr *agentproto.ServerIdentity
			if serverIdentity.Product != "" || serverIdentity.Version != "" || serverIdentity.Branch != "" || serverIdentity.BuildFingerprint != "" || serverIdentity.PID != 0 {
				serverPtr = &serverIdentity
			}
			payload, _ := agentproto.MarshalEnvelope(agentproto.Envelope{
				Type: agentproto.EnvelopeWelcome,
				Welcome: &agentproto.Welcome{
					Protocol:   agentproto.WireProtocol,
					ServerTime: time.Now(),
					Server:     serverPtr,
				},
			})
			s.logRaw("out", payload, agentproto.EnvelopeWelcome, hello.Instance.InstanceID, "")
			err = conn.WriteMessage(websocket.TextMessage, payload)
			if err != nil {
				return
			}
			if probeOnly {
				continue
			}
			if s.callbacks.OnHello != nil {
				s.callbacks.OnHello(ctx, meta, hello)
			}
		case agentproto.EnvelopeEventBatch:
			if envelope.EventBatch == nil {
				continue
			}
			if s.callbacks.OnEvents != nil {
				s.callbacks.OnEvents(ctx, meta, envelope.EventBatch.InstanceID, envelope.EventBatch.Events)
			}
		case agentproto.EnvelopeCommandAck:
			if envelope.CommandAck == nil {
				continue
			}
			if instanceID == "" {
				instanceID = envelope.CommandAck.InstanceID
			}
			if s.callbacks.OnCommandAck != nil {
				s.callbacks.OnCommandAck(ctx, meta, instanceID, *envelope.CommandAck)
			}
		}
	}
}

func (s *Server) logRaw(direction string, payload []byte, envelopeType agentproto.EnvelopeType, instanceID, commandID string) {
	if s.rawLogger == nil {
		return
	}
	s.rawLogger.Log(debuglog.RawEntry{
		InstanceID:   instanceID,
		Channel:      "relay.ws",
		Direction:    direction,
		EnvelopeType: string(envelopeType),
		CommandID:    commandID,
		Frame:        payload,
	})
}

func writeError(conn *websocket.Conn, problem agentproto.ErrorInfo) error {
	problem = problem.Normalize()
	payload, err := agentproto.MarshalEnvelope(agentproto.Envelope{
		Type: agentproto.EnvelopeError,
		Error: &agentproto.ErrorEnvelope{
			Code:    problem.Code,
			Message: problem.Message,
			Problem: &problem,
		},
	})
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}
