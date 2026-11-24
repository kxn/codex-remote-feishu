package privatechat

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"testing"
	"time"
)

func TestPrivateDisconnectedDetachFreezesOldInputAndPreventsDispatch(t *testing.T) {
	j, p, lease, o, binding := effectFixture(t)
	var dbIndex int
	var dbName, dbPath string
	if e := j.db.QueryRow(`PRAGMA database_list`).Scan(&dbIndex, &dbName, &dbPath); e != nil {
		t.Fatal(e)
	}
	var before int
	if e := j.db.QueryRow(`PRAGMA user_version`).Scan(&before); e != nil || before != 7 {
		t.Fatalf("initial schema=%d err=%v", before, e)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "old-message"
	text.Text = "old input"
	_, effects := applyEffectAction(t, j, p, o, "old-ingress", "input", []string{"old-message"}, text)
	old := commandEffect(t, effects, agentproto.CommandPromptSend)
	dispatch, _, e := lease.PrepareEffectDispatch(t.Context(), old.Effect.ID, binding)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := p.ApplyNativeDisconnect(t.Context(), lease, "disconnected"); e != nil {
		t.Fatal(e)
	}
	detach := privateAction(o, control.ActionDetach)
	_, effects = applyEffectAction(t, j, p, o, "explicit-detach", "control", nil, detach)
	found := false
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "instance" {
			found = true
		}
	}
	if !found {
		t.Fatal("old exact stop not persisted")
	}
	if _, e := p.ToolSurface(t.Context(), o, 1, "instance"); e == nil {
		t.Fatal("detached old carrier retained private tool access")
	}

	if _, _, e := lease.PrepareEffectDispatch(t.Context(), old.Effect.ID, binding); !errors.Is(e, ErrRecoveryHeld) {
		t.Fatalf("old prepared input could dispatch: %v", e)
	}
	if e = lease.MarkSending(t.Context(), dispatch.ID); !errors.Is(e, ErrRecoveryHeld) {
		t.Fatalf("pre-registered old input could send: %v", e)
	}

	// A late authenticated hello may continue reporting old receipts but cannot
	// reattach the detached surface or run its native tick.
	helloEffects, e := p.ApplyAuthenticatedNativeHello(t.Context(), lease, nativeHello(lease.native), "late-hello")
	if e != nil || len(helloEffects) != 0 {
		t.Fatalf("late hello projected effects=%d err=%v", len(helloEffects), e)
	}
	if due, e := p.PrivateTickDue(t.Context(), lease, time.Now()); e != nil || due {
		t.Fatalf("detached tick due=%v err=%v", due, e)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	_, rejected := applyEffectAction(t, j, p, o, "stale-attach", "control", nil, attach)
	explicit := false
	for _, v := range rejected {
		ev, e := DecodeEffectEvent(v.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if ev.Notice != nil && ev.Notice.Code == "private_old_execution_unknown" {
			explicit = true
		}
	}
	if !explicit || p.app.Service().Surface(o.SurfaceID).AttachedInstanceID != "" {
		t.Fatal("old carrier was reattached")
	}

	// A late durable native fact remains correlated to the old execution and
	// cannot generate display or retarget the replacement conversation.
	p.app.Service().Surface(o.SurfaceID).SelectedThreadID = "new-thread"
	late := persistNative(t, t.Context(), lease, binding, 1, agentproto.Event{Kind: agentproto.EventThreadsSnapshot})
	lateEffects, e := p.ApplyNative(t.Context(), lease, late, CoreNativeReducer{})
	if e != nil || len(lateEffects) != 0 {
		t.Fatalf("late event projected into current surface: effects=%d err=%v", len(lateEffects), e)
	}
	if p.app.Service().Surface(o.SurfaceID).SelectedThreadID != "new-thread" {
		t.Fatal("old event retargeted new surface")
	}

	cp, e := j.LoadCheckpoint(t.Context(), o, 1)
	if e != nil {
		t.Fatal(e)
	}
	if cp.Core.Version != 2 {
		t.Fatal("detached evidence must fence version-1 readers")
	}
	if len(cp.Core.DetachedExecutions) != 1 || cp.Core.DetachedExecutions[0].InstanceID != "instance" || cp.Core.ActiveQueueItemID != "" || len(cp.Core.DetachedExecutions[0].Events) != 1 || cp.Core.DetachedExecutions[0].Events[0].PayloadHash != late.PayloadHash {
		t.Fatal("old unknown ownership not isolated")
	}
	p.poisoned = true
	p, e = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.ensureOwner(t.Context(), o, 1); e != nil {
		t.Fatal(e)
	}
	if !p.app.Service().PrivateDetachedInstance(o.SurfaceID, "instance") {
		t.Fatal("detached proof lost on restart")
	}
	if !p.app.Service().PrivateRecoveryActionAllowed(privateAction(o, control.ActionTextMessage)) {
		t.Fatal("old execution holds new owner input")
	}
	other := Owner{"g", "other-surface", "other-chat", "other-user", "tenant", "app"}
	if _, e = p.ensureOwner(t.Context(), other, 1); e != nil {
		t.Fatal(e)
	}
	if !p.app.Service().PrivateRecoveryActionAllowed(privateAction(other, control.ActionTextMessage)) {
		t.Fatal("detached execution blocked a different owner")
	}
	var version int
	if e := j.db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil || version != 7 {
		t.Fatalf("detached schema=%d err=%v", version, e)
	}
	// The version-5 reader's startup gate explicitly rejects every version >5.
	if version <= 5 {
		t.Fatal("older background writer could open detached journal")
	}
	if e := j.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := OpenJournal(dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.db.QueryRow(`PRAGMA user_version`).Scan(&version); e != nil || version != 7 {
		t.Fatalf("reopen downgraded schema=%d err=%v", version, e)
	}

}

func TestPrivateDetachPreservesAlreadySendingAsUnknown(t *testing.T) {
	j, p, lease, o, binding := effectFixture(t)
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "sent-message"
	text.Text = "already sent"
	_, effects := applyEffectAction(t, j, p, o, "sent-ingress", "input", []string{"sent-message"}, text)
	old := commandEffect(t, effects, agentproto.CommandPromptSend)
	dispatch, _, e := lease.PrepareEffectDispatch(t.Context(), old.Effect.ID, binding)
	if e != nil {
		t.Fatal(e)
	}
	if e = lease.MarkSending(t.Context(), dispatch.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = p.ApplyNativeDisconnect(t.Context(), lease, "lost"); e != nil {
		t.Fatal(e)
	}
	detach := privateAction(o, control.ActionDetach)
	applyEffectAction(t, j, p, o, "detach-after-send", "control", nil, detach)
	if e = lease.MarkDispatchUnknown(t.Context(), dispatch.ID, "socket outcome unavailable"); e != nil {
		t.Fatal(e)
	}
	var state string
	if e = j.db.QueryRow(`SELECT state FROM dispatch WHERE id=?`, dispatch.ID).Scan(&state); e != nil || state != "unknown" {
		t.Fatalf("sent state=%q err=%v", state, e)
	}
}
