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

func TestPublicationPartialSuccessSurvivesRestartWithoutBatchReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	o := Owner{"g", "s", "chat", "user", "tenant", "app"}
	a := New(nil, orchestrator.Config{})
	if err = a.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := a.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := EncodeEffectEvent(eventcontract.Event{Kind: eventcontract.KindNotice, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Text: "two operations"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = j.CommitCheckpoint(t.Context(), Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, []Effect{{ID: "publish", Lane: "interactive", Body: body}}); err != nil {
		t.Fatal(err)
	}
	if err = j.PreparePublication(t.Context(), o, 1, "publish", body, []json.RawMessage{json.RawMessage(`{"op":"first"}`), json.RawMessage(`{"op":"second"}`)}); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(t.Context(), o, 1, "publish"); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(t.Context(), o, 1, "publish", 1); err == nil {
		t.Fatal("second operation passed first without receipt")
	}
	if err = j.BeginPublicationOperation(t.Context(), o, 1, "publish", 0); err != nil {
		t.Fatal(err)
	}
	if err = j.CompletePublicationOperation(t.Context(), o, 1, "publish", 0, "confirmed", json.RawMessage(`{"message":"actual-first"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(t.Context(), o, 1, "publish", 1); err != nil {
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
	p, err := j.LoadPublication(t.Context(), o, 1, "publish")
	if err != nil {
		t.Fatal(err)
	}
	if p.EffectState != "unknown" || p.Operations[0].State != "confirmed" || p.Operations[1].State != "unknown" {
		t.Fatalf("partial results lost: %#v", p)
	}
	if err = j.BeginPublicationOperation(t.Context(), o, 1, "publish", 0); err == nil {
		t.Fatal("first successful send replayed")
	}
	if _, err = j.FinishPublication(t.Context(), o, 1, "publish", time.Now()); err == nil {
		t.Fatal("partial batch marked complete")
	}
	if err = j.CompletePublicationOperation(t.Context(), o, 1, "publish", 1, "confirmed", json.RawMessage(`{"message":"invented"}`), nil); err == nil {
		t.Fatal("caller JSON became a platform receipt")
	}
	if err = j.CompletePublicationOperation(t.Context(), o, 1, "publish", 1, "confirmed", json.RawMessage(`{"message":"actual-second"}`), testEffectVerifier{}); err != nil {
		t.Fatal(err)
	}
	receipt, err := j.FinishPublication(t.Context(), o, 1, "publish", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Results) != 2 || string(receipt.Plan) != string(body) {
		t.Fatalf("lost immutable plan/results: %#v", receipt)
	}
	other := o
	other.UserID = "other"
	if _, err = j.LoadPublication(t.Context(), other, 1, "publish"); err == nil {
		t.Fatal("another owner obtained publication")
	}
}
