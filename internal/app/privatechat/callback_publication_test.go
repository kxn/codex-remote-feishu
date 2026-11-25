package privatechat

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestCallbackPublicationEligibilityRequiresExactUnappliedLatestCard(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{"g", "surface", "chat", "user", "tenant", "app"}
	app := New(nil, orchestrator.Config{})
	if err = app.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []Effect{{ID: "card", Lane: "interactive", Body: body}, {ID: "later", Lane: "interactive", Body: body}}); err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		got, e := j.CallbackPublicationEligible(ctx, o, 1, "card", 0, "message")
		if e != nil || got != want {
			t.Fatalf("eligible=%v want=%v err=%v", got, want, e)
		}
	}
	check(true)
	if _, err = j.db.Exec(`INSERT INTO private_publications(effect_id,plan,plan_hash) VALUES('later','{}','hash')`); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`INSERT INTO private_publication_ops(effect_id,ordinal,body,state) VALUES('later',0,?,'sending')`, `{"Operation":{"Kind":"update_card","MessageID":"message"}}`); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err = j.db.Exec(`UPDATE private_publication_ops SET state='prepared' WHERE effect_id='later'`); err != nil {
		t.Fatal(err)
	}
	check(true)
	other := o
	other.UserID = "foreign"
	got, err := j.CallbackPublicationEligible(ctx, other, 1, "card", 0, "message")
	if err != nil || got {
		t.Fatalf("foreign eligible=%v err=%v", got, err)
	}
	action := privateAction(o, control.ActionStatus)
	seq := admittedAction(t, j, o, "later", "control", action)
	if _, err = j.db.Exec(`INSERT INTO private_callback_response(seq,revision,body) VALUES(?,2,?)`, seq, `{"version":1,"target_message_id":"message","view_token":"later","response":{}}`); err != nil {
		t.Fatal(err)
	}
	check(false)
	if _, err = j.db.Exec(`UPDATE private_callback_response SET revision=1 WHERE seq=?`, seq); err != nil {
		t.Fatal(err)
	}
	check(true)
	if _, err = j.db.Exec(`INSERT INTO private_effect_applied(effect_id,revision,receipt_digest) VALUES('card',1,'done')`); err != nil {
		t.Fatal(err)
	}
	check(false)
}
