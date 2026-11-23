package relayws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestAuthenticatedTeamConnectionUsesExactIdentity(t *testing.T) {
	hellos := make(chan ConnectionMeta, 4)
	events := make(chan ConnectionMeta, 4)
	acks := make(chan ConnectionMeta, 4)
	s := NewServer(ServerCallbacks{AuthorizeHello: func(h agentproto.Hello, secret string) error {
		if h.Instance.InstanceID == "team" && (h.WorkerProtocolVersion != 1 || secret != "team-secret") {
			return errors.New("team authentication failed")
		}
		return nil
	}, OnHello: func(_ context.Context, m ConnectionMeta, _ agentproto.Hello) { hellos <- m }, OnEvents: func(_ context.Context, m ConnectionMeta, _ string, _ []agentproto.Event) { events <- m }, OnCommandAck: func(_ context.Context, m ConnectionMeta, _ string, _ agentproto.CommandAck) { acks <- m }})
	defer s.Close()
	web := httptest.NewServer(s)
	defer web.Close()
	url := "ws" + strings.TrimPrefix(web.URL, "http")
	dial := func(instance, secret string, team bool) *websocket.Conn {
		t.Helper()
		header := http.Header{}
		if secret != "" {
			header.Set(PrivateCredentialHeader, secret)
		}
		c, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		h := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: instance}}
		if team {
			h.WorkerProtocolVersion = 1
		}
		raw, _ := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeHello, Hello: &h})
		if err = c.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		return c
	}
	bad := dial("team", "", true)
	defer bad.Close()
	_ = bad.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := bad.ReadMessage(); err == nil {
		t.Fatal("credential-free team admitted")
	}
	first := dial("team", "team-secret", true)
	defer first.Close()
	if _, _, err := first.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	m1 := <-hellos
	if !m1.CarrierAuthenticated || m1.PrivateAuthenticated {
		t.Fatalf("team mislabeled: %+v", m1)
	}
	if err := s.WithCurrentConnection("team", m1.ConnectionID, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.SendCommand("team", agentproto.Command{}); err == nil {
		t.Fatal("authenticated carrier allowed instance-only send")
	}
	if err := s.SendCommandToConnection("team", m1.ConnectionID, agentproto.Command{CommandID: "cmd"}); err != nil {
		t.Fatal(err)
	}
	_, raw, err := first.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := agentproto.UnmarshalEnvelope(raw)
	if err != nil || envelope.Command == nil || envelope.Command.CommandID != "cmd" {
		t.Fatalf("command: %+v %v", envelope, err)
	}
	ack := agentproto.CommandAck{InstanceID: "team", CommandID: "cmd", Accepted: true}
	raw, _ = agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeCommandAck, CommandAck: &ack})
	if err = first.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	if (<-acks).ConnectionID != m1.ConnectionID {
		t.Fatal("ack lost connection identity")
	}
	batch := agentproto.EventBatch{InstanceID: "team", Events: []agentproto.Event{{Kind: agentproto.EventModelCatalogUpdated}}}
	raw, _ = agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeEventBatch, EventBatch: &batch})
	if err = first.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	if (<-events).ConnectionID != m1.ConnectionID {
		t.Fatal("event lost connection identity")
	}
	second := dial("team", "team-secret", true)
	defer second.Close()
	if _, _, err = second.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	m2 := <-hellos
	if err = s.SendEventReceiptToConnection("team", m1.ConnectionID, agentproto.EventReceipt{Sequence: 1}); err == nil {
		t.Fatal("old receipt reached replacement")
	}
	if err = s.SendEventReceiptToConnection("team", m2.ConnectionID, agentproto.EventReceipt{Sequence: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = second.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	legacy := dial("legacy", "", false)
	defer legacy.Close()
	if _, _, err = legacy.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	lm := <-hellos
	if lm.CarrierAuthenticated || lm.PrivateAuthenticated {
		t.Fatal("legacy connection mislabeled authenticated")
	}
	if err = s.SendCommand("legacy", agentproto.Command{CommandID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = legacy.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err = s.WithCurrentConnection("legacy", lm.ConnectionID, func() error { return nil }); err == nil {
		t.Fatal("legacy admitted to authenticated path")
	}
}
