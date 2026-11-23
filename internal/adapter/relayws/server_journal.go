package relayws

import (
	"errors"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// SendEventReceipt must be called only after the daemon commits the exact
// incarnation/sequence/hash receipt to its durable store.
func (s *Server) SendEventReceipt(instanceID string, receipt agentproto.EventReceipt) error {
	s.mu.RLock()
	current := s.conns[instanceID]
	s.mu.RUnlock()
	if current == nil {
		return errors.New("instance offline")
	}
	if current.authenticated {
		return errors.New("authenticated carrier requires connection identity")
	}
	if receipt.InstanceID == "" {
		receipt.InstanceID = instanceID
	}
	if receipt.InstanceID != instanceID {
		return errors.New("receipt instance mismatch")
	}
	raw, err := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeEventReceipt, EventReceipt: &receipt})
	if err != nil {
		return err
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	current.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	s.logRaw("out", raw, agentproto.EnvelopeEventReceipt, instanceID, "")
	return current.conn.WriteMessage(websocket.TextMessage, raw)
}
