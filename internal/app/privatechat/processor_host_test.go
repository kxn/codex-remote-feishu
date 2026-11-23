package privatechat

import (
	"context"
	"database/sql"
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

type testEffectVerifier struct{}

func (testEffectVerifier) VerifyPublicationResult(context.Context, Owner, int64, string, int, json.RawMessage, json.RawMessage) error {
	return nil
}

type testHostReducer struct {
	owner Owner
	calls *int
}

func (r testHostReducer) ReduceHostReceipt(a *App, effect Effect, receipt json.RawMessage) ([]eventcontract.Event, error) {
	*r.calls++
	action := privateAction(r.owner, control.ActionVerboseCommand)
	action.Text = "/verbose quiet"
	return a.Action(action)
}

func TestHostReceiptRecoveryAndAtomicReplay(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := privateAction(o, control.ActionShowCommandMenu)
	a.Text = "/menu"
	seq := admittedAction(t, j, o, "menu", "control", a)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil || len(effects) == 0 {
		t.Fatalf("effects: %v", err)
	}
	id := effects[0].Effect.ID
	if err := p.InspectEffect(ctx, o, 1, id, func(app *App, effect EffectRecord) error {
		if app.Service() == nil || effect.Effect.ID != id {
			t.Fatal("incorrect planning owner")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	reducer := testHostReducer{owner: o, calls: &calls}
	if _, err := p.ApplyHostReceipt(ctx, o, 1, id, reducer); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unconfirmed receipt applied: %v", err)
	}
	if err := j.PreparePublication(ctx, o, 1, id, effects[0].Effect.Body, []json.RawMessage{json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err := j.BeginPublicationOperation(ctx, o, 1, id, 0); err != nil {
		t.Fatal(err)
	}
	if err := j.CompletePublicationOperation(ctx, o, 1, id, 0, "confirmed", json.RawMessage(`{"message_id":"actual"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	if _, err := j.FinishPublication(ctx, o, 1, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec(`CREATE TRIGGER fail_host BEFORE INSERT ON private_effect_applied BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyHostReceipt(ctx, o, 1, id, reducer); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("failed transaction: %v", err)
	}
	checkpoint, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || checkpoint.Core.Verbosity == state.SurfaceVerbosityQuiet {
		t.Fatalf("failed mutation committed: %#v %v", checkpoint, err)
	}
	if _, err := j.db.Exec(`DROP TRIGGER fail_host`); err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyHostReceipt(ctx, o, 1, id, reducer); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyHostReceipt(ctx, o, 1, id, reducer); err != nil || calls != 2 {
		t.Fatalf("duplicate reduced: calls=%d err=%v", calls, err)
	}
	checkpoint, err = j.LoadCheckpoint(ctx, o, 1)
	if err != nil || checkpoint.Core.Verbosity != state.SurfaceVerbosityQuiet {
		t.Fatalf("receipt not committed: %#v %v", checkpoint, err)
	}
}
