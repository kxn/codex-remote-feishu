package privatechat

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPublicationSchema8BackfillKeepsOriginalUnknownAndScopes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	event := eventcontract.Event{SurfaceSessionID: o.SurfaceID, RequestView: &control.FeishuRequestView{RequestID: "original-request"}}
	body, err := EncodeEffectEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	effects := []Effect{{ID: "old", Lane: "interactive", Body: body}, {ID: "next", Lane: "interactive", Body: body}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`UPDATE private_effect SET state='unknown' WHERE id='old'; DROP TABLE private_publication_order; PRAGMA user_version=8;`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err = j.PublicationOrderReady(ctx, o, 1, "next"); err != ErrDisplayHeld {
		t.Fatalf("old unknown was bypassed: %v", err)
	}
	stored, err := j.LoadEffect(ctx, o, 1, "old")
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != "unknown" || string(stored.Effect.Body) != string(body) {
		t.Fatal("migration rewrote old evidence")
	}
}

func TestUnrenderedCardWriterUsesOnlyOriginalConfirmedDisplayReceipt(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	effects := []Effect{{ID: "sent", Lane: "interactive", Body: body, DisplayKey: "original"}, {ID: "unrendered", Lane: "interactive", Body: body, DisplayKey: "original"}, {ID: "back", Lane: "interactive", Body: body}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	// Controlled historical receipt: no current surface or rendered operation is
	// supplied for the pending writer, which was frozen before it knew the ID.
	receipt, _ := json.Marshal(PublicationReceipt{Version: 2, Results: []PublicationResult{{Receipt: json.RawMessage(`{"Kind":"send_card","MessageID":"actual-card"}`)}}})
	if _, err = j.db.Exec(`UPDATE private_effect SET state='confirmed',receipt=? WHERE id='sent'`, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`INSERT INTO private_effect_applied VALUES('sent',1,'fixture')`); err != nil {
		t.Fatal(err)
	}
	held, err := j.HasUnsettledCardWrite(ctx, o, 1, "actual-card")
	if err != nil || !held {
		t.Fatalf("unrendered writer missed: %v %v", held, err)
	}
	if err = j.CardPublicationOrderReady(ctx, o, 1, "back", "actual-card"); err != ErrDisplayHeld {
		t.Fatalf("back overtook old writer: %v", err)
	}
	if err = j.CardPublicationOrderReady(ctx, o, 1, "unrendered", "actual-card"); err != nil {
		t.Fatalf("writer blocked itself: %v", err)
	}
	held, err = j.HasUnsettledCardWrite(ctx, o, 1, "independent-card")
	if err != nil || held {
		t.Fatalf("independent card held: %v %v", held, err)
	}
	foreign := o
	foreign.UserID = "other"
	held, err = j.HasUnsettledCardWrite(ctx, foreign, 1, "actual-card")
	if err != nil || held {
		t.Fatalf("foreign owner leaked: %v %v", held, err)
	}
}

func TestFirstCardReceiptHoldsNavigationUntilOriginalEffectApplied(t *testing.T) {
	ctx := context.Background()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "first card"}})
	if err != nil {
		t.Fatal(err)
	}
	effects := []Effect{{ID: "first", Lane: "interactive", Body: body}, {ID: "navigation", Lane: "interactive", Body: body}}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	op := json.RawMessage(`{"Operation":{"Kind":"send_card","MessageID":""}}`)
	if err = j.PreparePublication(ctx, o, 1, "first", body, []json.RawMessage{op}); err != nil {
		t.Fatal(err)
	}
	// The real operation receipt, rather than a guessed message target or an
	// already-completed whole effect, is now known to the publisher.
	if _, err = j.db.Exec(`UPDATE private_publication_ops SET state='confirmed',receipt='{"Kind":"send_card","MessageID":"new-card"}' WHERE effect_id='first'`); err != nil {
		t.Fatal(err)
	}
	held, err := j.HasUnsettledCardWrite(ctx, o, 1, "new-card")
	if err != nil || !held {
		t.Fatalf("first card write missed: %v %v", held, err)
	}
	if err = j.CardPublicationOrderReady(ctx, o, 1, "navigation", "new-card"); err != ErrDisplayHeld {
		t.Fatalf("navigation overtook first card: %v", err)
	}
	if _, err = j.db.Exec(`INSERT INTO private_effect_applied VALUES('first',1,'fixture')`); err != nil {
		t.Fatal(err)
	}
	held, err = j.HasUnsettledCardWrite(ctx, o, 1, "new-card")
	if err != nil || held {
		t.Fatalf("completed first card still held: %v %v", held, err)
	}
}
