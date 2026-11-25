package privatechat

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPreparedReadSurvivesRegistryRollbackWithoutDuplicateSend(t *testing.T) {
	j, p, l, o, b := effectFixture(t)
	a := privateAction(o, control.ActionGoalCommand)
	a.Text = "/goal"
	_, effects := applyEffectAction(t, j, p, o, "goal", "control", nil, a)
	effect := commandEffect(t, effects, agentproto.CommandThreadGoalGet)
	registry, err := executionowner.Open(filepath.Join(t.TempDir(), "owner.db"), knownUnsentStopVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	reg := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "instance", Incarnation: "inc", Backend: "codex", Namespace: "history", Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Private, ID: "owner", Epoch: 1}}
	if err = registry.Register(t.Context(), reg); err != nil {
		t.Fatal(err)
	}
	out, err := executionowner.NewOutbound(registry)
	if err != nil {
		t.Fatal(err)
	}
	var dispatch Dispatch
	sent := 0
	err = out.SendLeased(t.Context(), reg, func(token *executionowner.Lease) error {
		var e error
		dispatch, _, e = l.PrepareEffectDispatch(t.Context(), effect.Effect.ID, b)
		if e != nil {
			return e
		}
		deadline, e := j.NativeReadDeadline(t.Context(), o, 1, dispatch.ID)
		if e != nil {
			return e
		}
		if e = token.BeginSessionRead(reg, "thread", dispatch.ID, deadline); e != nil {
			return e
		}
		return errors.New("registry rollback before mark sending")
	}, func() error { sent++; return nil })
	var before *executionowner.BeforeSendError
	if !errors.As(err, &before) || sent != 0 {
		t.Fatalf("initial operation unexpectedly sent: %v %d", err, sent)
	}
	deadline, err := j.NativeReadDeadline(t.Context(), o, 1, dispatch.ID)
	if err != nil || deadline.IsZero() {
		t.Fatalf("frozen deadline lost: %v", err)
	}
	if state, e := j.DispatchState(t.Context(), dispatch.ID); e != nil || state != "prepared" {
		t.Fatalf("state=%s error=%v", state, e)
	}
	err = out.SendLeased(t.Context(), reg, func(token *executionowner.Lease) error {
		d, _, e := l.PrepareEffectDispatch(context.Background(), effect.Effect.ID, b)
		if e != nil {
			return e
		}
		again, e := j.NativeReadDeadline(t.Context(), o, 1, d.ID)
		if e != nil || !again.Equal(deadline) {
			t.Fatalf("deadline changed: %v", e)
		}
		if e = token.BeginSessionRead(reg, "thread", d.ID, again); e != nil {
			return e
		}
		return l.MarkSending(t.Context(), d.ID)
	}, func() error { sent++; return nil })
	if err != nil || sent != 1 {
		t.Fatalf("prepared recovery failed: sent=%d error=%v", sent, err)
	}
	if err = l.MarkSending(t.Context(), dispatch.ID); err == nil {
		t.Fatal("second send admission succeeded")
	}
}
