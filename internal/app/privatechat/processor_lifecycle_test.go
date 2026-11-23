package privatechat

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestHostLifecyclePersistsOnlyLoadedOwnersAndReplaysWithoutReduction(t *testing.T) {
	j, o, _ := memoryJournalFixture(t)
	defer j.Close()
	menu := privateAction(o, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	seq := admittedAction(t, j, o, "menu", "control", menu)
	makeProcessor := func() *Processor {
		p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := makeProcessor()
	calls := 0
	reduce := func(_ *App, owner Owner) []eventcontract.Event {
		calls++
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: owner.GatewayID, SurfaceSessionID: owner.SurfaceID, Notice: &control.Notice{Text: "服务正在重启"}}}
	}
	at := time.Now().UTC()
	if effects, err := p.ApplyHostLifecycle(t.Context(), "shutdown", at, reduce); err != nil || len(effects) != 0 || calls != 0 {
		t.Fatalf("unloaded owner received notice: %+v %v", effects, err)
	}
	if _, err := p.ApplyIngress(t.Context(), o, 1, seq); err != nil {
		t.Fatal(err)
	}
	first, err := p.ApplyHostLifecycle(t.Context(), "shutdown", at, reduce)
	if err != nil || len(first) != 1 || calls != 1 {
		t.Fatalf("shutdown effects: %+v %v calls=%d", first, err, calls)
	}
	p.poisoned = true // Simulate loss of the prior processor.
	p = makeProcessor()
	if _, err = p.ensureOwner(t.Context(), o, 1); err != nil {
		t.Fatal(err)
	}
	replay, err := p.ApplyHostLifecycle(t.Context(), "shutdown", at, reduce)
	if err != nil || len(replay) != 1 || calls != 1 || replay[0].Effect.Effect.ID != first[0].Effect.Effect.ID {
		t.Fatalf("replayed reduction: %+v %v calls=%d", replay, err, calls)
	}
	if _, err = p.ApplyHostLifecycle(t.Context(), "shutdown", at.Add(time.Second), reduce); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("changed observation accepted: %v", err)
	}
	if _, err = p.ApplyHostLifecycle(t.Context(), "bad", at, func(*App, Owner) []eventcontract.Event {
		return []eventcontract.Event{{Kind: eventcontract.KindDaemonCommand, SurfaceSessionID: o.SurfaceID}}
	}); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("lifecycle could start execution: %v", err)
	}
	if _, err = p.ApplyHostLifecycle(t.Context(), "after-bad", at, reduce); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("invalid reducer state reused: %v", err)
	}
}
