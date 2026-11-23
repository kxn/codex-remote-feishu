package privatechat

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateTickIsDurableAndIdempotent(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	n := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, e := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	l, e := j.OpenNativeLease(ctx, testNativeAuthority{}, n, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	p, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if e = p.RegisterNativeHello(ctx, l, nativeHello(n)); e != nil {
		t.Fatal(e)
	}
	second := n
	second.CarrierIncarnation = "other-inc"
	second.InstanceID = "other-instance"
	l2, e := j.OpenNativeLease(ctx, testNativeAuthority{}, second, o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.RegisterNativeHello(ctx, l2, nativeHello(second)); e != nil {
		t.Fatal(e)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = n.InstanceID
	if _, e = p.app.Action(attach); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, e = p.ApplyPrivateTick(ctx, l2, "wrong-carrier", now); !errors.Is(e, ErrJournalConflict) {
		t.Fatalf("wrong attached carrier: %v", e)
	}
	if _, e = p.ApplyPrivateTick(ctx, l, "tick-1", now); e != nil {
		t.Fatal(e)
	}
	first := p.owners[o].revision
	if _, e = p.ApplyPrivateTick(ctx, l, "tick-1", now); e != nil {
		t.Fatal(e)
	}
	if p.owners[o].revision != first {
		t.Fatal("replay committed a second revision")
	}
	if _, e = p.ApplyPrivateTick(ctx, l, "tick-1", now.Add(time.Second)); !errors.Is(e, ErrJournalConflict) {
		t.Fatalf("changed observation time: %v", e)
	}
	// A replacement processor reads the committed tick, including its original
	// revision, without executing the timer a second time.
	p.poisoned = true // simulate loss of the previous processor owner
	p2, e := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p2.ApplyPrivateTick(ctx, l, "tick-1", now); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_tick_applied WHERE tick_id='tick-1'`).Scan(&count); e != nil || count != 1 {
		t.Fatalf("durable tick: %d %v", count, e)
	}
}
