package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Reuses a real prepared team launch, SQLite owner registry and coordinator;
// unlike the full gateway assembly this fixture does not start the SDK gateway.
func exerciseTeamOwnerWebSocket(t *testing.T, app *App, adapter *teamExecutionOwnerAdapter, worker work.WorkerSession, carrier workstore.CarrierRegistration, hello agentproto.Hello, credential string, command agentproto.Command, event agentproto.Event) {
	t.Helper()
	web := httptest.NewServer(app.relay)
	defer web.Close()
	url := "ws" + strings.TrimPrefix(web.URL, "http")
	dial := func(secret string) *websocket.Conn {
		t.Helper()
		header := http.Header{}
		if secret != "" {
			header.Set(relayws.PrivateCredentialHeader, secret)
		}
		c, _, err := websocket.DefaultDialer.Dial(url, header)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeHello, Hello: &hello})
		if err = c.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		return c
	}
	bad := dial("wrong")
	defer bad.Close()
	_ = bad.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := bad.ReadMessage(); err == nil {
		t.Fatal("wrong team credential accepted")
	}
	current := dial(credential)
	defer current.Close()
	if _, _, err := current.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	waitConnection := func(after uint64) uint64 {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			adapter.connectionMu.Lock()
			c := adapter.connections[worker.InstanceID]
			adapter.connectionMu.Unlock()
			if c.id != 0 && c.id != after {
				return c.id
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("authenticated team hello not committed")
		return 0
	}
	firstID := waitConnection(0)
	var err error
	adapter.outbound, err = executionowner.NewOutbound(adapter.registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.SendRegistered(t.Context(), carrier.Incarnation, worker.InstanceID, command, func() error { t.Error("authenticated command used legacy sender"); return nil }); err != nil {
		t.Fatal(err)
	}
	_, wire, err := current.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	e, err := agentproto.UnmarshalEnvelope(wire)
	if err != nil || e.Command == nil || e.Command.CommandID != command.CommandID {
		t.Fatalf("team command wire: %+v %v", e, err)
	}
	raw, _ := json.Marshal(command)
	hash := sha256.Sum256(raw)
	ack := agentproto.CommandAck{InstanceID: worker.InstanceID, CommandID: command.CommandID, Accepted: true, RuntimeReceipt: &agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: carrier.Incarnation, CommandID: command.CommandID, PayloadHash: hex.EncodeToString(hash[:]), Stage: "written", PreparedFrames: 1, WrittenFrames: 1}}
	wire, _ = agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeCommandAck, CommandAck: &ack})
	if err = current.WriteMessage(websocket.TextMessage, wire); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		var saved bool
		err = adapter.store.Within(t.Context(), func(tx *workstore.Tx) error {
			c, e := tx.GetNativeCommand(command.CommandID)
			if e == nil {
				saved = c.Ack != nil
			}
			return e
		})
		if err == nil && saved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("team ack not persisted: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	batch := agentproto.EventBatch{InstanceID: worker.InstanceID, Events: []agentproto.Event{event}}
	wire, _ = agentproto.MarshalEnvelope(agentproto.Envelope{Type: agentproto.EnvelopeEventBatch, EventBatch: &batch})
	if err = current.WriteMessage(websocket.TextMessage, wire); err != nil {
		t.Fatal(err)
	}
	_ = current.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, receiptRaw, err := current.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := agentproto.UnmarshalEnvelope(receiptRaw)
	if err != nil || receipt.EventReceipt == nil || receipt.EventReceipt.PayloadHash != event.Journal.PayloadHash {
		t.Fatalf("durable team event receipt: %+v %v", receipt, err)
	}
	replacement := dial(credential)
	defer replacement.Close()
	if _, _, err = replacement.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	secondID := waitConnection(firstID)
	if err = app.relay.SendEventReceiptToConnection(worker.InstanceID, firstID, *receipt.EventReceipt); err == nil {
		t.Fatal("old event receipt sent to new connection")
	}
	if err = replacement.WriteMessage(websocket.TextMessage, wire); err != nil {
		t.Fatal(err)
	}
	_ = replacement.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, replayed, err := replacement.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	again, err := agentproto.UnmarshalEnvelope(replayed)
	if err != nil || again.EventReceipt == nil || *again.EventReceipt != *receipt.EventReceipt {
		t.Fatalf("team replay receipt: %+v %v (connection %d)", again, err, secondID)
	}
}
