package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type emptyHostReceiptReducer struct{}

func (emptyHostReceiptReducer) ReduceHostReceipt(*App, Effect, json.RawMessage) ([]eventcontract.Event, error) {
	return nil, nil
}

func TestDisplayChainWaitsForAppliedReceiptButIndependentMenuProceeds(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app := New(nil, orchestrator.Config{})
	if err := app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	progress := eventcontract.Event{SurfaceSessionID: "s", ExecCommandProgress: &control.ExecCommandProgress{ThreadID: "thread", TurnID: "turn"}}
	events := []eventcontract.Event{progress, progress, {SurfaceSessionID: "s", Notice: &control.Notice{Text: "menu"}}}
	effects := []Effect{{ID: "progress-1", Lane: "interactive"}, {ID: "progress-2", Lane: "interactive"}, {ID: "menu", Lane: "interactive"}}
	for i := range effects {
		effects[i].Body, err = EncodeEffectEvent(events[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := bindNativeDisplayChains(o, "carrier", events, effects); err != nil {
		t.Fatal(err)
	}
	if err := j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	if err := j.PreparePublication(ctx, o, 1, "progress-1", effects[0].Body, []json.RawMessage{json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkEffectSending(ctx, o, 1, "progress-1"); err != nil {
		t.Fatal(err)
	}
	if err := j.BeginPublicationOperation(ctx, o, 1, "progress-1", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveDisplayPredecessor(ctx, o, 1, "progress-2"); !errors.Is(err, ErrDisplayHeld) {
		t.Fatalf("progress did not wait: %v", err)
	}
	if err := j.MarkEffectSending(ctx, o, 1, "menu"); err != nil {
		t.Fatalf("independent menu blocked: %v", err)
	}
	if err := j.CompletePublicationOperation(ctx, o, 1, "progress-1", 0, "unknown", json.RawMessage(`{"unknown":true}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := j.ResolveEffect(ctx, o, 1, "progress-1", "unknown", json.RawMessage(`{"unknown":true}`), nil); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkEffectSending(ctx, o, 1, "progress-2"); err == nil {
		t.Fatal("unknown predecessor bypassed")
	}
	if _, err := j.ResolveDisplayPredecessor(ctx, o, 1, "progress-2"); !errors.Is(err, ErrDisplayHeld) {
		t.Fatalf("unknown predecessor: %v", err)
	}
	if err := j.CompletePublicationOperation(ctx, o, 1, "progress-1", 0, "confirmed", json.RawMessage(`{"message":"actual"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.FinishPublication(ctx, o, 1, "progress-1", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := j.ResolveDisplayPredecessor(ctx, o, 1, "progress-2"); !errors.Is(err, ErrDisplayHeld) {
		t.Fatalf("unapplied receipt allowed: %v", err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyHostReceipt(ctx, o, 1, "progress-1", emptyHostReceiptReducer{}); err != nil {
		t.Fatal(err)
	}
	previous, err := j.ResolveDisplayPredecessor(ctx, o, 1, "progress-2")
	if err != nil || previous == nil || previous.EffectID != "progress-1" {
		t.Fatalf("confirmed predecessor: %#v %v", previous, err)
	}
	history, err := j.ListDisplayHistory(ctx, o, 1, "progress-2", 0, 0, 64)
	if err != nil || len(history) != 1 || history[0].Receipt.EffectID != "progress-1" {
		t.Fatalf("display history: %#v %v", history, err)
	}
	last := history[0]
	history, err = j.ListDisplayHistory(ctx, o, 1, "progress-2", last.Revision, last.Ordinal, 64)
	if err != nil || len(history) != 0 {
		t.Fatalf("display history cursor: %#v %v", history, err)
	}
	if err := j.MarkEffectSending(ctx, o, 1, "progress-2"); err != nil {
		t.Fatal(err)
	}
}

func TestPendingReactionChainUsesEachActualSourceMessage(t *testing.T) {
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	a := eventcontract.Event{SurfaceSessionID: "s", PendingInput: &control.PendingInputState{SourceMessageID: "image", QueueOn: true}}
	b := eventcontract.Event{SurfaceSessionID: "s", PendingInput: &control.PendingInputState{SourceMessageID: "image", QueueOff: true, TypingOn: true}}
	c := eventcontract.Event{SurfaceSessionID: "s", PendingInput: &control.PendingInputState{SourceMessageID: "text", QueueOn: true}}
	if explicitCardDisplayKey(o, a) == "" || explicitCardDisplayKey(o, a) != explicitCardDisplayKey(o, b) || explicitCardDisplayKey(o, a) == explicitCardDisplayKey(o, c) {
		t.Fatal("reaction chains do not follow exact source")
	}
}
