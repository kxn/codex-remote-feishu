package privatechat

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestRecoveryNoticeIsSoftPublicationPredecessor(t *testing.T) {
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
	cp, err := app.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	hint := eventcontract.Event{Kind: eventcontract.KindTimelineText, SurfaceSessionID: o.SurfaceID, SourceMessageID: "msg", TimelineText: &control.TimelineText{Type: control.TimelineTextRecoveryInputWaiting, Text: "waiting", ReplyToMessageID: "msg"}}
	final := eventcontract.Event{Kind: eventcontract.KindBlockCommitted, SurfaceSessionID: o.SurfaceID, SourceMessageID: "msg", Block: &render.Block{Final: true, Text: "answer"}}
	var effects []Effect
	for i, event := range []eventcontract.Event{hint, final} {
		body, e := EncodeEffectEvent(event)
		if e != nil {
			t.Fatal(e)
		}
		id := []string{"hint", "final"}[i]
		effects = append(effects, Effect{ID: id, Lane: "interactive", Body: body, OrderKeys: publicationOrderKeys(o, 1, orchestrator.FrozenPrivatePublicationOrderScopes(event))})
	}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: cp}, 0, effects); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"prepared", "sending"} {
		if _, err = j.db.Exec(`UPDATE private_effect SET state=? WHERE id='hint'`, status); err != nil {
			t.Fatal(err)
		}
		if err = j.PublicationOrderReady(t.Context(), o, 1, "final"); !errors.Is(err, ErrDisplayHeld) {
			t.Fatalf("%s did not hold concurrent final: %v", status, err)
		}
	}
	if _, err = j.db.Exec(`UPDATE private_effect SET state='unknown' WHERE id='hint'`); err != nil {
		t.Fatal(err)
	}
	if err = j.PublicationOrderReady(t.Context(), o, 1, "final"); err != nil {
		t.Fatalf("unknown explanatory message blocked answer: %v", err)
	}
	foreign := o
	foreign.UserID = "other"
	if err = j.PublicationOrderReady(t.Context(), foreign, 1, "final"); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("foreign owner: %v", err)
	}
}
