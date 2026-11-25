package privatechat

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPendingRequestVisibilityDoesNotDuplicateUnconfirmedOutbox(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	v := p.app.Service().Surface(o.SurfaceID)
	v.PendingRequests = map[string]*state.RequestPromptRecord{"request": {RequestID: "request", RequestType: "approval", InstanceID: "instance", ThreadID: "thread", TurnID: "turn", Title: "确认操作", VisibilityState: "delivery_degraded", LifecycleState: "awaiting_visibility", NeedsRedelivery: true}}
	v.PendingRequestOrder = []string{"request"}
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	seq := admittedAction(t, j, o, "menu-first", "control", menu)
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var requestEffect string
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.RequestView != nil && event.RequestView.RequestID == "request" {
			if requestEffect != "" {
				t.Fatal("duplicate request")
			}
			requestEffect = effect.Effect.ID
		}
	}
	if requestEffect == "" {
		t.Fatal("degraded request was not replayed")
	}
	if got := v.PendingRequests["request"].DeliveryAttemptCount; got != 1 {
		t.Fatalf("one replay must count once, got %d", got)
	}
	if err = j.MarkEffectSending(ctx, o, 1, requestEffect); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []control.ActionKind{control.ActionShowCommandMenu, control.ActionStatus} {
		a := privateAction(o, kind)
		a.Text = "/menu"
		seq = admittedAction(t, j, o, "inflight-"+string(kind), "control", a)
		effects, err = p.ApplyIngress(ctx, o, 1, seq)
		if err != nil {
			t.Fatal(err)
		}
		for _, effect := range effects {
			event, e := DecodeEffectEvent(effect.Effect.Body)
			if e != nil {
				t.Fatal(e)
			}
			if event.RequestView != nil && event.RequestView.RequestID == "request" {
				t.Fatalf("sending request was re-sent after %s", kind)
			}
		}
	}
	if got := v.PendingRequests["request"].DeliveryAttemptCount; got != 1 {
		t.Fatalf("held send advanced attempt counter: %d", got)
	}
	// A crash changes the unresolved send to unknown. It must remain the only
	// candidate after restart, regardless of further menu or status input.
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	var effectState string
	if err = j.db.QueryRow(`SELECT state FROM private_effect WHERE id=?`, requestEffect).Scan(&effectState); err != nil || effectState != "unknown" {
		t.Fatalf("uncertain outbox not retained: %q %v", effectState, err)
	}
	for _, kind := range []control.ActionKind{control.ActionShowCommandMenu, control.ActionStatus} {
		a := privateAction(o, kind)
		a.Text = "/menu"
		seq = admittedAction(t, j, o, "after-"+string(kind), "control", a)
		effects, err = p.ApplyIngress(ctx, o, 1, seq)
		if err != nil {
			t.Fatal(err)
		}
		for _, effect := range effects {
			event, e := DecodeEffectEvent(effect.Effect.Body)
			if e != nil {
				t.Fatal(e)
			}
			if event.RequestView != nil && event.RequestView.RequestID == "request" {
				t.Fatalf("unknown request was re-sent after %s", kind)
			}
		}
	}
}

func TestDegradedRequestReplaysAfterOrdinaryTextAndMenu(t *testing.T) {
	for _, kind := range []control.ActionKind{control.ActionTextMessage, control.ActionShowCommandMenu, control.ActionStatus} {
		t.Run(string(kind), func(t *testing.T) {
			ctx := context.Background()
			o := Owner{"g", "s", "c", "u", "tenant", "app"}
			j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.app.RegisterOwner(o); err != nil {
				t.Fatal(err)
			}
			v := p.app.Service().Surface(o.SurfaceID)
			v.PendingRequests = map[string]*state.RequestPromptRecord{"request": {RequestID: "request", RequestType: "approval", InstanceID: "instance", ThreadID: "thread", TurnID: "turn", Title: "确认操作", VisibilityState: "delivery_degraded", LifecycleState: "awaiting_visibility", NeedsRedelivery: true}}
			v.PendingRequestOrder = []string{"request"}
			a := privateAction(o, kind)
			a.MessageID = "source"
			a.Text = "hello"
			lane := "input"
			if kind != control.ActionTextMessage {
				lane = "control"
			}
			seq := admittedAction(t, j, o, "action", lane, a)
			effects, err := p.ApplyIngress(ctx, o, 1, seq)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, effect := range effects {
				event, e := DecodeEffectEvent(effect.Effect.Body)
				if e != nil {
					t.Fatal(e)
				}
				if event.RequestView != nil && event.RequestView.RequestID == "request" {
					count++
				}
			}
			if count != 1 || v.PendingRequests["request"].DeliveryAttemptCount != 1 {
				t.Fatalf("request effects=%d attempts=%d", count, v.PendingRequests["request"].DeliveryAttemptCount)
			}
		})
	}
}
