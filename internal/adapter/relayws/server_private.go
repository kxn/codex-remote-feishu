package relayws

import (
	"errors"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// SendCommandToConnection fails if the authenticated connection has since been
// replaced. The connection map remains read locked through the socket write.
func (s *Server) SendCommandToConnection(instance string, connection uint64, command agentproto.Command) error {
	raw, err := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeCommand, Command: &command})
	if err != nil {
		return err
	}
	return s.sendToConnection(instance, connection, raw, agentproto.EnvelopeCommand, command.CommandID)
}

func (s *Server) SendEventReceiptToConnection(instance string, connection uint64, receipt agentproto.EventReceipt) error {
	if receipt.InstanceID == "" {
		receipt.InstanceID = instance
	}
	if receipt.InstanceID != instance {
		return errors.New("receipt instance mismatch")
	}
	raw, err := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeEventReceipt, EventReceipt: &receipt})
	if err != nil {
		return err
	}
	return s.sendToConnection(instance, connection, raw, agentproto.EnvelopeEventReceipt, "")
}

func (s *Server) sendToConnection(instance string, connection uint64, raw []byte, kind agentproto.EnvelopeType, command string) error {
	if connection == 0 {
		return errors.New("connection identity required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current := s.conns[instance]
	if current == nil || !current.authenticated || current.id != connection {
		return errors.New("authenticated connection replaced or offline")
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	current.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	s.logRaw("out", raw, kind, instance, command)
	return current.conn.WriteMessage(websocket.TextMessage, raw)
}

// WithCurrentConnection serializes a local durable callback with replacement of
// the socket. The callback must not perform network I/O or call back into Server.
func (s *Server) WithCurrentConnection(instance string, connection uint64, commit func() error) error {
	if connection == 0 || commit == nil {
		return errors.New("connection identity and callback required")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	current := s.conns[instance]
	if current == nil || !current.authenticated || current.id != connection {
		return errors.New("authenticated connection replaced or offline")
	}
	return commit()
}
