package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type testCardReducer struct{ CoreReducer }

func (testCardReducer) PlanCard(_ *App, _ control.Action, events []eventcontract.Event) (*CardResponse, []eventcontract.Event, error) {
	return &CardResponse{Version: 1, TargetMessageID: "card", Response: json.RawMessage(`{"card":{"type":"raw","data":{}}}`)}, events, nil
}

func TestCallbackResponseAndCheckpointCommitTogether(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	j, err := OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	a := privateAction(o, control.ActionShowCommandMenu)
	a.Text = "/menu"
	seq := admittedAction(t, j, o, "callback", "control", a)
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, testCardReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec(`CREATE TRIGGER fail_callback BEFORE INSERT ON private_callback_response BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, seq); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("commit did not fail: %v", err)
	}
	for _, table := range []string{"private_applied", "private_checkpoint", "private_effect", "private_callback_response"} {
		var count int
		if err := j.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s not rolled back: %d %v", table, count, err)
		}
	}
	if _, err = j.db.Exec(`DROP TRIGGER fail_callback`); err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, testCardReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatal(err)
	}
	card, err := p.ReadCommittedCardResponse(ctx, o, 1, seq)
	if err != nil || card.TargetMessageID != "card" {
		t.Fatalf("response: %#v %v", card, err)
	}
}
