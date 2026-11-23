package relayws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateAuthenticatedConnectionCannotBeTakenOver(t *testing.T) {
	hellos := make(chan ConnectionMeta, 3)
	server := NewServer(ServerCallbacks{AuthorizeHello: func(h agentproto.Hello, secret string) error {
		if h.Instance.InstanceID == "private" && (secret != "secret" || h.PrivateNativeProtocolVersion != 1) {
			return context.Canceled
		}
		return nil
	}, OnHello: func(_ context.Context, m ConnectionMeta, _ agentproto.Hello) { hellos <- m }})
	defer server.Close()
	web := httptest.NewServer(server)
	defer web.Close()
	url := "ws" + strings.TrimPrefix(web.URL, "http")
	dial := func(secret string, private bool) *websocket.Conn {
		t.Helper()
		header := http.Header{}
		if secret != "" {
			header.Set(PrivateCredentialHeader, secret)
		}
		c, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		hello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "private"}}
		if private {
			hello.PrivateNativeProtocolVersion = 1
		}
		raw, _ := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeHello, Hello: &hello})
		if err = c.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		return c
	}
	first := dial("secret", true)
	defer first.Close()
	if _, _, err := first.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	m1 := <-hellos
	if !m1.PrivateAuthenticated {
		t.Fatal("missing authentication")
	}
	attacker := dial("", false)
	defer attacker.Close()
	attacker.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := attacker.ReadMessage(); err == nil {
		t.Fatal("takeover accepted")
	}
	if err := server.SendCommandToConnection("private", m1.ConnectionID, agentproto.Command{CommandID: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := first.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	second := dial("secret", true)
	defer second.Close()
	if _, _, err := second.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	m2 := <-hellos
	if m2.ConnectionID == m1.ConnectionID {
		t.Fatal("connection identity reused")
	}
	if err := server.SendCommandToConnection("private", m1.ConnectionID, agentproto.Command{}); err == nil {
		t.Fatal("old socket accepted")
	}
	if err := server.SendEventReceiptToConnection("private", m1.ConnectionID, agentproto.EventReceipt{}); err == nil {
		t.Fatal("old receipt accepted")
	}
	if err := server.SendCommandToConnection("private", m2.ConnectionID, agentproto.Command{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := second.ReadMessage(); err != nil {
		t.Fatal(err)
	}
}
func TestPrivateHelloRejectedWithoutAuthorizer(t *testing.T) {
	s := NewServer(ServerCallbacks{})
	defer s.Close()
	web := httptest.NewServer(s)
	defer web.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(web.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, _ := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeHello, Hello: &agentproto.Hello{PrivateNativeProtocolVersion: 1, Instance: agentproto.InstanceHello{InstanceID: "i"}}})
	if err = c.WriteMessage(websocket.TextMessage, raw); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = c.ReadMessage(); err == nil {
		t.Fatal("unauthenticated private accepted")
	}
}
