package privatechat

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestSuppressedPublicationKeepsUnsentOperationsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "chat", "user", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []Effect{{ID: "view", Lane: "interactive", Body: body}}); err != nil {
		t.Fatal(err)
	}
	if err = j.PreparePublication(t.Context(), o, 1, "view", body, []json.RawMessage{json.RawMessage(`{"op":"card"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`DROP TABLE private_applied; CREATE TABLE private_applied(seq INTEGER PRIMARY KEY REFERENCES ingress(seq),revision INTEGER NOT NULL); INSERT INTO ingress(gateway,tenant,app_id,surface,chat,user_id,generation,event_id,lane,payload,sources) VALUES('g','tenant','app','s','chat','user',1,'old','input','{}','[]'); INSERT INTO private_applied(seq,revision) VALUES(1,1); PRAGMA user_version=7`); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	var migrated int
	if err = j.db.QueryRow(`SELECT count(*) FROM private_applied WHERE seq=1 AND revision=1 AND canceled_by IS NULL`).Scan(&migrated); err != nil || migrated != 1 {
		t.Fatalf("legacy applied row: %d %v", migrated, err)
	}
	if err = j.SuppressPreparedPublication(t.Context(), o, 1, "view", "stale_model_view", time.Now()); err != nil {
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
	var version int
	if err = j.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 8 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	pub, err := j.LoadPublication(t.Context(), o, 1, "view")
	if err != nil || pub.EffectState != "confirmed" || len(pub.Operations) != 1 || pub.Operations[0].State != "prepared" {
		t.Fatalf("pub=%+v err=%v", pub, err)
	}
	effect, err := j.LoadEffect(t.Context(), o, 1, "view")
	if err != nil {
		t.Fatal(err)
	}
	var receipt PublicationReceipt
	if json.Unmarshal(effect.Receipt, &receipt) != nil || receipt.Version != 2 || receipt.Kind != "suppressed" || len(receipt.Results) != 0 {
		t.Fatalf("receipt=%+v", receipt)
	}
	if err = j.SuppressPreparedPublication(t.Context(), o, 1, "view", "stale_model_view", time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestDisplayPredecessorSkipsSuppressedWithoutInventingReceipt(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "s", "chat", "user", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	effects := []Effect{{ID: "first", Lane: "interactive", Body: body, DisplayKey: "chain"}, {ID: "stale", Lane: "interactive", Body: body, DisplayKey: "chain"}, {ID: "next", Lane: "interactive", Body: body, DisplayKey: "chain"}}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, effects); err != nil {
		t.Fatal(err)
	}
	if err = j.PreparePublication(t.Context(), o, 1, "first", body, []json.RawMessage{json.RawMessage(`{"op":"first"}`)}); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(t.Context(), o, 1, "first"); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(t.Context(), o, 1, "first", 0); err != nil {
		t.Fatal(err)
	}
	if err = j.CompletePublicationOperation(t.Context(), o, 1, "first", 0, "confirmed", json.RawMessage(`{"message":"platform"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.FinishPublication(t.Context(), o, 1, "first", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`INSERT INTO private_effect_applied(effect_id,revision,receipt_digest) VALUES('first',1,'digest')`); err != nil {
		t.Fatal(err)
	}
	if err = j.PreparePublication(t.Context(), o, 1, "stale", body, []json.RawMessage{json.RawMessage(`{"op":"stale"}`)}); err != nil {
		t.Fatal(err)
	}
	if err = j.SuppressPreparedPublication(t.Context(), o, 1, "stale", "stale_model_view", time.Now()); err != nil {
		t.Fatal(err)
	}
	predecessor, err := j.ResolveDisplayPredecessor(t.Context(), o, 1, "next")
	if err != nil || predecessor == nil || predecessor.EffectID != "first" {
		t.Fatalf("predecessor=%+v err=%v", predecessor, err)
	}
	pub, err := j.LoadPublication(t.Context(), o, 1, "stale")
	if err != nil || pub.Operations[0].State != "prepared" {
		t.Fatalf("suppressed operation=%+v err=%v", pub, err)
	}
}
