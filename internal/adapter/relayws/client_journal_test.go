package relayws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func testTransportJournal(t *testing.T) *runtimejournal.Store {
	t.Helper()
	root, cwd := t.TempDir(), t.TempDir()
	store, err := runtimejournal.Open(context.Background(), filepath.Join(root, "journal.db"), root, cwd, "carrier-old")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestJournalTransportDisconnectReplaysUntilExactReceipt(t *testing.T) {
	journal := testTransportJournal(t)
	events, err := journal.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnCompleted, CommandID: "command-original", Execution: &agentproto.ExecutionBinding{Version: 1, ScopeID: "original-scope"}}})
	if err != nil {
		t.Fatal(err)
	}
	original := events[0]
	var firstConnection atomic.Uint64
	replayed := make(chan agentproto.Event, 8)
	receipts := make(chan agentproto.EventReceipt, 8)
	var server *Server
	server = NewServer(ServerCallbacks{
		OnHello: func(_ context.Context, meta ConnectionMeta, _ agentproto.Hello) {
			firstConnection.CompareAndSwap(0, meta.ConnectionID)
		},
		OnEvents: func(_ context.Context, meta ConnectionMeta, instanceID string, events []agentproto.Event) {
			if meta.ConnectionID == firstConnection.Load() {
				server.CloseConnection(instanceID, meta.ConnectionID)
				return
			}
			for _, event := range events {
				replayed <- event
				if event.Journal != nil {
					_ = server.SendEventReceipt(instanceID, agentproto.EventReceipt{CarrierIncarnation: event.Journal.CarrierIncarnation, Sequence: event.Journal.Sequence, PayloadHash: event.Journal.PayloadHash})
				}
			}
		},
	})
	t.Cleanup(func() { server.Close() })
	httpServer := httptest.NewServer(http.HandlerFunc(server.ServeHTTP))
	t.Cleanup(httpServer.Close)
	client := NewClient("ws"+strings.TrimPrefix(httpServer.URL, "http"), agentproto.Hello{Protocol: agentproto.WireProtocol, Instance: agentproto.InstanceHello{InstanceID: "inst-1"}, JournalProtocolVersion: 1, CarrierIncarnation: "carrier-new"}, ClientCallbacks{OnEventReceipt: func(_ context.Context, receipt agentproto.EventReceipt) error { receipts <- receipt; return nil }})
	client.SetEventJournal(journal)
	t.Cleanup(client.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	select {
	case event := <-replayed:
		if event.Journal == nil || *event.Journal != *original.Journal || event.Execution == nil || event.Execution.ScopeID != "original-scope" {
			t.Fatal("reconnect replaced original incarnation or execution")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unacknowledged critical event was not replayed after reconnect")
	}
	select {
	case receipt := <-receipts:
		if receipt.Sequence != original.Journal.Sequence {
			t.Fatal("wrong receipt")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("receipt not delivered")
	}
	pending, _, err := journal.Replay(0, 128)
	if err != nil || len(pending) != 0 {
		t.Fatal("durable receipt did not clear pending replay")
	}
	cancel()
	client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("client did not close")
	}
}

func TestJournalPersistPrecedesOutboxAndRejectsBadReceipt(t *testing.T) {
	journal := testTransportJournal(t)
	client := newClientWithQueueSizes("ws://unused", agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "inst"}}, ClientCallbacks{}, 1, 1)
	client.SetEventJournal(journal)
	defer client.Close()
	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventTurnStarted}}); err != nil {
		t.Fatal(err)
	}
	if err := client.SendEvents([]agentproto.Event{{Kind: agentproto.EventTurnCompleted}}); err == nil {
		t.Fatal("test did not fill transport outbox")
	}
	pending, _, err := journal.Replay(0, 128)
	if err != nil || len(pending) != 2 {
		t.Fatal("outbox failure lost durable event")
	}
	identity := pending[0].Journal
	if err := client.handleEventReceipt(context.Background(), agentproto.EventReceipt{InstanceID: "other", CarrierIncarnation: identity.CarrierIncarnation, Sequence: identity.Sequence, PayloadHash: identity.PayloadHash}); err == nil {
		t.Fatal("accepted cross-instance receipt")
	}
	if err := client.handleEventReceipt(context.Background(), agentproto.EventReceipt{InstanceID: "inst", CarrierIncarnation: identity.CarrierIncarnation, Sequence: identity.Sequence, PayloadHash: "wrong"}); err == nil {
		t.Fatal("accepted wrong event hash")
	}
	pending, _, err = journal.Replay(0, 128)
	if err != nil || len(pending) != 2 {
		t.Fatal("bad receipt deleted durable events")
	}
}

func TestRuntimeCommandAckReturnsDurableStage(t *testing.T) {
	client := NewClient("ws://unused", agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: "inst"}}, ClientCallbacks{
		OnCommand: func(context.Context, agentproto.Command) error { return nil },
		RuntimeCommandReceipt: func(id string) (*agentproto.RuntimeCommandReceipt, error) {
			return &agentproto.RuntimeCommandReceipt{Version: 1, CommandID: id, Stage: runtimejournal.StagePrepared}, nil
		},
	})
	defer client.Close()
	client.handleCommand(context.Background(), agentproto.Command{CommandID: "prepared-only"})
	if len(client.controlOutbox) != 1 {
		t.Fatal("missing command ACK")
	}
	ack := client.controlOutbox[0].envelope.CommandAck
	if !ack.Accepted || ack.RuntimeReceipt == nil || ack.RuntimeReceipt.Stage != runtimejournal.StagePrepared || ack.RuntimeReceipt.WrittenFrames != 0 {
		t.Fatal("Accepted transport ACK fabricated written receipt")
	}
}
