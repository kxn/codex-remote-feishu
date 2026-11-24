package privatechat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"testing"
)

func TestPrivateOfflineStopJournalReconnectDoesNotGuessTurn(t *testing.T) {
	j, p, lease, o, _ := effectFixture(t)
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "old-input"
	text.Text = "old input"
	applyEffectAction(t, j, p, o, "old", "input", []string{"old-input"}, text)
	if _, e := p.ApplyNativeDisconnect(t.Context(), lease, "gone"); e != nil {
		t.Fatal(e)
	}
	stop := privateAction(o, control.ActionStop)
	stop.MessageID = "stop"
	_, effects := applyEffectAction(t, j, p, o, "stop", "control", nil, stop)
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil {
			t.Fatal("offline stop emitted native command")
		}
	}
	cp, e := j.LoadCheckpoint(t.Context(), o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.Core.Version != 2 || cp.Core.OfflineStop == nil || cp.Core.OfflineStop.InstanceID != "instance" || cp.Core.OfflineStop.CommandID == "" {
		t.Fatalf("stop intent missing: %+v", cp.Core.OfflineStop)
	}
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	effects, e = p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "reconnect")
	if e != nil {
		t.Fatal(e)
	}
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandTurnInterrupt {
			t.Fatal("reconnect guessed an unconfirmed turn")
		}
	}
	cp, e = j.LoadCheckpoint(t.Context(), o, 1)
	if e != nil || cp.Core.OfflineStop == nil {
		t.Fatal("unconfirmed stop intent lost")
	}
}
