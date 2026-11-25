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
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type cancelWindowHostReducer struct {
	owner   Owner
	entered chan struct{}
	release chan struct{}
	calls   int
}

func (r *cancelWindowHostReducer) ReduceHostReceipt(a *App, _ Effect, _ json.RawMessage) ([]eventcontract.Event, error) {
	r.calls++
	action := privateAction(r.owner, control.ActionVerboseCommand)
	action.Text = "/verbose quiet"
	events, err := a.Action(action)
	close(r.entered)
	<-r.release
	return events, err
}

func TestHostProjectionCancellationIsAtomic(t *testing.T) {
	ctx := context.Background()
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	action := privateAction(owner, control.ActionShowCommandMenu)
	action.Text = "/menu"
	seq := admittedAction(t, j, owner, "menu", "control", action)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil || len(effects) == 0 {
		t.Fatalf("initial effects: %v", err)
	}
	id := effects[0].Effect.ID
	if err = j.PreparePublication(ctx, owner, 1, id, effects[0].Effect.Body, []json.RawMessage{json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, owner, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(ctx, owner, 1, id, 0); err != nil {
		t.Fatal(err)
	}
	if err = j.CompletePublicationOperation(ctx, owner, 1, id, 0, "confirmed", json.RawMessage(`{"message_id":"actual"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.FinishPublication(ctx, owner, 1, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	before, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := &cancelWindowHostReducer{owner: owner, entered: make(chan struct{}), release: make(chan struct{})}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = p.ApplyHostReceipt(canceled, owner, 1, id, r); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled: %v", err)
	}
	if r.calls != 0 {
		t.Fatal("reducer executed for pre-canceled call")
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil || core.Verbosity != before.Core.Verbosity {
		t.Fatalf("pre-canceled core changed: %+v %v", core, err)
	}
	after, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || after.Revision != before.Revision || after.Core.Verbosity != before.Core.Verbosity {
		t.Fatalf("pre-canceled checkpoint changed: %+v %v", after, err)
	}
	var applied int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_effect_applied WHERE effect_id=?`, id).Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("pre-canceled receipt applied: %d %v", applied, err)
	}
	active, cancelActive := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, e := p.ApplyHostReceipt(active, owner, 1, id, r); result <- e }()
	select {
	case <-r.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reducer did not enter")
	}
	cancelActive()
	close(r.release)
	select {
	case err = <-result:
		if err != nil {
			t.Fatalf("started projection did not commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("projection did not complete")
	}
	after, err = j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || after.Revision != before.Revision+1 || after.Core.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("mutated checkpoint missing: %+v %v", after, err)
	}
	var revision int64
	if err = j.db.QueryRow(`SELECT revision FROM private_effect_applied WHERE effect_id=?`, id).Scan(&revision); err != nil || revision != after.Revision {
		t.Fatalf("receipt not atomically applied: %d %v", revision, err)
	}
	next := privateAction(owner, control.ActionShowCommandMenu)
	next.Text = "/menu"
	nextSeq := admittedAction(t, j, owner, "next", "control", next)
	if _, err = p.ApplyIngress(ctx, owner, 1, nextSeq); err != nil {
		t.Fatalf("subsequent ingress failed: %v", err)
	}
	if _, err = p.ApplyHostReceipt(ctx, owner, 1, id, r); err != nil || r.calls != 1 {
		t.Fatalf("receipt replay: calls=%d err=%v", r.calls, err)
	}
}

type cancelWindowIngressReducer struct {
	entered, release chan struct{}
	calls            int
}

func (r *cancelWindowIngressReducer) Reduce(a *App, action control.Action) ([]eventcontract.Event, error) {
	r.calls++
	events, err := a.Action(action)
	close(r.entered)
	<-r.release
	return events, err
}

func TestIngressProjectionCancellationIsAtomic(t *testing.T) {
	ctx := context.Background()
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	menu := privateAction(owner, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	seed := admittedAction(t, j, owner, "seed", "control", menu)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, owner, 1, seed); err != nil {
		t.Fatal(err)
	}
	before, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	action := privateAction(owner, control.ActionVerboseCommand)
	action.Text = "/verbose quiet"
	seq := admittedAction(t, j, owner, "verbose", "control", action)
	reducer := &cancelWindowIngressReducer{entered: make(chan struct{}), release: make(chan struct{})}
	p.reducer = reducer
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = p.ApplyIngress(canceled, owner, 1, seq); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled: %v", err)
	}
	if reducer.calls != 0 {
		t.Fatal("pre-canceled reducer executed")
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil || core.Verbosity != before.Core.Verbosity {
		t.Fatalf("core changed: %+v %v", core, err)
	}
	var count int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=?`, seq).Scan(&count); err != nil || count != 0 {
		t.Fatalf("premature applied=%d %v", count, err)
	}
	active, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, e := p.ApplyIngress(active, owner, 1, seq); result <- e }()
	select {
	case <-reducer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reducer not entered")
	}
	stop()
	close(reducer.release)
	select {
	case err = <-result:
		if err != nil {
			t.Fatalf("projection commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("projection stuck")
	}
	after, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || after.Revision != before.Revision+1 || after.Core.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("checkpoint: %+v %v", after, err)
	}
	var rev int64
	if err = j.db.QueryRow(`SELECT revision FROM private_applied WHERE seq=?`, seq).Scan(&rev); err != nil || rev != after.Revision {
		t.Fatalf("applied revision=%d %v", rev, err)
	}
	p.reducer = CoreReducer{}
	next := admittedAction(t, j, owner, "next", "control", menu)
	if _, err = p.ApplyIngress(ctx, owner, 1, next); err != nil {
		t.Fatalf("next: %v", err)
	}
}

func TestOwnerLifecycleProjectionCancellationIsAtomic(t *testing.T) {
	ctx := context.Background()
	owner := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	menu := privateAction(owner, control.ActionShowCommandMenu)
	menu.Text = "/menu"
	seq := admittedAction(t, j, owner, "seed", "control", menu)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, owner, 1, seq); err != nil {
		t.Fatal(err)
	}
	before, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	at := time.Now().UTC()
	reduce := func(a *App, o Owner) []eventcontract.Event {
		calls++
		action := privateAction(o, control.ActionVerboseCommand)
		action.Text = "/verbose quiet"
		_, e := a.Action(action)
		if e != nil {
			panic(e)
		}
		close(entered)
		<-release
		return []eventcontract.Event{{Kind: eventcontract.KindNotice, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "stopping"}}}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = p.ApplyOwnerHostLifecycle(canceled, owner, 1, "shutdown", at, reduce); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled: %v", err)
	}
	if calls != 0 {
		t.Fatal("pre-canceled reducer executed")
	}
	core, err := p.app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil || core.Verbosity != before.Core.Verbosity {
		t.Fatalf("core changed: %+v %v", core, err)
	}
	var count int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_tick_applied WHERE tick_id=?`, "host-lifecycle:shutdown").Scan(&count); err != nil || count != 0 {
		t.Fatalf("premature lifecycle=%d %v", count, err)
	}
	active, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, e := p.ApplyOwnerHostLifecycle(active, owner, 1, "shutdown", at, reduce); result <- e }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reducer not entered")
	}
	stop()
	close(release)
	select {
	case err = <-result:
		if err != nil {
			t.Fatalf("projection commit: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("projection stuck")
	}
	after, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || after.Revision != before.Revision+1 || after.Core.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("checkpoint: %+v %v", after, err)
	}
	var rev int64
	if err = j.db.QueryRow(`SELECT revision FROM private_tick_applied WHERE tick_id=?`, "host-lifecycle:shutdown").Scan(&rev); err != nil || rev != after.Revision {
		t.Fatalf("lifecycle revision=%d %v", rev, err)
	}
	next := admittedAction(t, j, owner, "next", "control", menu)
	if _, err = p.ApplyIngress(ctx, owner, 1, next); err != nil {
		t.Fatalf("next: %v", err)
	}
	if _, err = p.ApplyOwnerHostLifecycle(ctx, owner, 1, "shutdown", at, reduce); err != nil || calls != 1 {
		t.Fatalf("replay calls=%d err=%v", calls, err)
	}
}
