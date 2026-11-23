package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type knownUnsentTestReducer struct{}
type knownUnsentStopVerifier struct{}

func (knownUnsentStopVerifier) VerifyStop(context.Context, executionowner.Registration, string) error {
	return errors.New("not used")
}

func (knownUnsentTestReducer) ReduceHostReceipt(app *App, e Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	var receipt KnownUnsentReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	event, _, _, err := DecodeStoredEffectTarget(e.Body)
	if err != nil {
		return nil, err
	}
	return app.Service().HandleKnownUnsent(event.SurfaceSessionID, receipt.CommandID, nil), nil
}

func TestKnownUnsentAtomicallyRecordsNegativeDispatchAndReceipt(t *testing.T) {
	j, p, l, o, b := effectFixture(t)
	action := privateAction(o, control.ActionTextMessage)
	action.MessageID = "message"
	action.Text = "hello"
	_, effects := applyEffectAction(t, j, p, o, "message", "input", []string{"message"}, action)
	effect := commandEffect(t, effects, agentproto.CommandPromptSend)
	registry, err := executionowner.Open(filepath.Join(t.TempDir(), "registry.db"), knownUnsentStopVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	registration := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "instance", Incarnation: "inc", Backend: "codex", Namespace: "history", Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Private, ID: "owner", Epoch: 1}}
	if err = registry.Register(t.Context(), registration); err != nil {
		t.Fatal(err)
	}
	outbound, err := executionowner.NewOutbound(registry)
	if err != nil {
		t.Fatal(err)
	}
	var dispatch Dispatch
	sent := false
	err = outbound.SendLeased(t.Context(), registration, func(*executionowner.Lease) error {
		var e error
		dispatch, _, e = l.PrepareEffectDispatch(t.Context(), effect.Effect.ID, b)
		if e != nil {
			return e
		}
		if e = l.MarkSending(t.Context(), dispatch.ID); e != nil {
			return e
		}
		return errors.New("local commit failure")
	}, func() error { sent = true; return nil })
	var before *executionowner.BeforeSendError
	if !errors.As(err, &before) || sent {
		t.Fatalf("transport was not proved unsent: %v %v", err, sent)
	}
	if err = l.RecordKnownUnsent(t.Context(), effect.Effect.ID, dispatch.ID, "transport_not_invoked"); err != nil {
		t.Fatal(err)
	}
	dispatchState, err := j.DispatchState(t.Context(), dispatch.ID)
	if err != nil || dispatchState != "failed" {
		t.Fatalf("not-sent dispatch: %s %v", dispatchState, err)
	}
	saved, err := j.LoadEffect(t.Context(), o, 1, effect.Effect.ID)
	if err != nil || saved.State != "confirmed" {
		t.Fatalf("missing local receipt: %+v %v", saved, err)
	}
	if err = l.RecordKnownUnsent(t.Context(), effect.Effect.ID, dispatch.ID, "transport_not_invoked"); err == nil {
		t.Fatal("duplicate proof was accepted as a fresh send")
	}
	if _, err = p.ApplyLocalHostReceipt(t.Context(), o, 1, effect.Effect.ID, knownUnsentTestReducer{}); err != nil {
		t.Fatal(err)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface.ActiveQueueItemID != "" || len(surface.QueuedQueueItemIDs) != 1 || surface.QueueItems[surface.QueuedQueueItemIDs[0]].Status != state.QueueItemQueued {
		t.Fatalf("unsent input not restored: %+v", surface)
	}
}
