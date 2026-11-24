package privatechat

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

// This reproduces the post-shutdown application state: durable selected owner
// checkpoint, a new processor and no connected old native carrier.
func TestPrivateCleanRestartWithoutRetiredProofDoesNotGuess(t *testing.T) {
	j, _, _, o, _ := effectFixture(t)
	var seq int
	var name, path string
	if err := j.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	action := privateAction(o, control.ActionTextMessage)
	action.MessageID = "after-restart"
	action.Text = "continue"
	raw, _ := json.Marshal(action)
	input, _, err := j.Admit(t.Context(), Ingress{Owner: o, Generation: 1, EventID: "after-restart-event", Lane: "input", Payload: raw, Sources: []string{action.MessageID}})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(t.Context(), o, 1, input)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range effects {
		v, err := DecodeEffectEvent(e.Effect.Body)
		if err != nil {
			t.Fatal(err)
		}
		if v.DaemonCommand != nil && v.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			t.Fatal("unproven recovery launched")
		}
		if v.Notice != nil && v.Notice.Code == "private_reconnect_input_unavailable" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing recovery notice")
	}
}
