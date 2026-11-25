package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"path/filepath"
	"testing"
)

func TestPublicationCausalitySurvivesRestartAndIsolatesIndependentOperations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(owner.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: owner.SurfaceID, Notice: &control.Notice{Text: "state"}})
	if err != nil {
		t.Fatal(err)
	}
	var effects []Effect
	for _, v := range []struct{ id, scope string }{{"pending", "operation"}, {"ready", "operation"}, {"other", "independent"}} {
		effects = append(effects, Effect{ID: v.id, Lane: "interactive", Body: body, OrderKeys: publicationOrderKeys(owner, 1, []string{v.scope})})
	}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: owner, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "pending"); err != nil {
		t.Fatal(err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "ready"); err != ErrDisplayHeld {
		t.Fatalf("missing predecessor barrier: %v", err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "other"); err != nil {
		t.Fatalf("unrelated operation blocked: %v", err)
	}
	if _, err = j.db.Exec(`UPDATE private_effect SET state='unknown' WHERE id='pending'`); err != nil {
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
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "ready"); err != ErrDisplayHeld {
		t.Fatalf("restart lost unknown predecessor: %v", err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "other"); err != nil {
		t.Fatal(err)
	}
	foreign := owner
	foreign.UserID = "another"
	if err = j.PublicationOrderReady(t.Context(), foreign, 1, "ready"); err != ErrJournalConflict {
		t.Fatalf("foreign owner: %v", err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 2, "ready"); err != ErrJournalConflict {
		t.Fatalf("foreign generation: %v", err)
	}
	// Model the existing applied marker's completed transaction, not a platform
	// success. The barrier must require application, not merely a receipt state.
	if _, err = j.db.Exec(`UPDATE private_effect SET state='confirmed' WHERE id='pending'`); err != nil {
		t.Fatal(err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "ready"); err != ErrDisplayHeld {
		t.Fatal("confirmed but unapplied predecessor passed")
	}
	if _, err = j.db.Exec(`INSERT INTO private_effect_applied(effect_id,revision,receipt_digest) VALUES('pending',1,'test')`); err != nil {
		t.Fatal(err)
	}
	if err = j.PublicationOrderReady(t.Context(), owner, 1, "ready"); err != nil {
		t.Fatal(err)
	}
}
