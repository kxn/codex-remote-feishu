package workpublish

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type yieldingResolver struct {
	sender *snapshotSender
	hook   func()
}

func (r *yieldingResolver) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	if r.hook != nil {
		f := r.hook
		r.hook = nil
		f()
	}
	return r.sender, nil
}

func TestGroupProgressOnlyChangedAndNoLateSameAttemptAfterFinal(t *testing.T) {
	store, _, _, now := publisherFixture(t)
	sender := &snapshotSender{}
	p, _ := New(store, sender, func() time.Time { return *now })
	queueSnapshot(t, store, "a", "role", "snapshot", workservice.PublicationProgress, *now)
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(31 * time.Second)
	queueSnapshot(t, store, "b", "role", "snapshot", workservice.PublicationProgress, *now, "a")
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || sender.patches != 0 {
		t.Fatal("identical progress generated API write")
	}
	*now = now.Add(time.Second)
	queueSnapshot(t, store, "c", "role", "result", workservice.PublicationFinal, *now, "a")
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(31 * time.Second)
	queueSnapshot(t, store, "d", "role", "late stale progress", workservice.PublicationProgress, *now, "a")
	if _, err := p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 2 || sender.patches != 0 {
		t.Fatal("same attempt progress resurrected after final")
	}
}

func TestFinalArrivingDuringGatewayResolutionFencesProgress(t *testing.T) {
	store, _, _, now := publisherFixture(t)
	sender := &snapshotSender{}
	resolver := &yieldingResolver{sender: sender}
	p, _ := New(store, resolver, func() time.Time { return *now })
	queueSnapshot(t, store, "a", "role", "pending progress", workservice.PublicationProgress, *now)
	resolver.hook = func() {
		*now = now.Add(time.Second)
		queueSnapshot(t, store, "b", "role", "final while gateway resolves", workservice.PublicationFinal, *now, "a")
	}
	if _, err := p.RunOnce(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 0 || sender.patches != 0 {
		t.Fatal("progress escaped after final was queued")
	}
	if _, err := p.RunOnce(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || sender.bodies[0] != "final while gateway resolves" {
		t.Fatalf("final not sent: %+v", sender)
	}
}
