package privatechat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateReconnectRejectsInputWithoutRetargeting(t *testing.T) {
	j, p, _, o, _ := effectFixture(t)
	p.poisoned = true
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "waiting-message"
	text.Text = "old target only"
	seq, effects := applyEffectAction(t, j, p, o, "waiting-event", "input", []string{text.MessageID}, text)
	found := false
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Notice != nil && event.Notice.Code == "private_reconnect_input_unavailable" {
			found = true
		}
		if event.Command != nil {
			t.Fatal("unready input dispatched")
		}
	}
	if !found {
		t.Fatal("missing actionable rejection")
	}
	entry, applied, _, err := j.processorIngress(t.Context(), o, 1, seq)
	if err != nil || !applied || entry.Ingress.EventID != "waiting-event" {
		t.Fatal("original input not retained as rejected fact")
	}
	detach := privateAction(o, control.ActionDetach)
	applyEffectAction(t, j, p, o, "detach-event", "control", nil, detach)
	cp, err := j.LoadCheckpoint(t.Context(), o, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range cp.Core.QueueItems {
		if item.SourceMessageID == text.MessageID {
			t.Fatal("rejected input entered a dispatch queue")
		}
	}
}
