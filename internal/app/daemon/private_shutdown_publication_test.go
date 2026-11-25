package daemon

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type shutdownReceiptGateway struct {
	*messageIDAssigningGateway
	visible chan struct{}
	release chan struct{}
}

func (g *shutdownReceiptGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	if err := g.messageIDAssigningGateway.Apply(ctx, ops); err != nil {
		return err
	}
	select {
	case g.visible <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// A platform write already in flight must reach its real receipt and atomic
// core checkpoint even when shutdown stops scheduling new work.
func TestPrivateShutdownDrainsPublicationReceiptBeforeStopping(t *testing.T) {
	a, cards := newApplicationRuntimeTestApp(t)
	r := a.applications
	g := &shutdownReceiptGateway{messageIDAssigningGateway: cards, visible: make(chan struct{}, 1), release: make(chan struct{})}
	a.gateway = g
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	errs := make(chan error, 8)
	if err := r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	closed := false
	defer func() {
		if !closed {
			close(g.release)
		}
	}()
	in := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", ReceivedAt: time.Now(), EventID: "shutdown-publish", MessageID: "shutdown-publish", RawContent: `{"text":"hi"}`}
	if err := r.hook.Receive(ctx, in); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.visible:
	case <-ctx.Done():
		t.Fatal("publication did not reach platform")
	}
	work, err := r.journal.PendingHostEffectsPage(ctx, 128, 0)
	if err != nil || len(work) == 0 {
		t.Fatalf("missing in-flight effect: %v", err)
	}
	var effect = work[0]
	// Trigger the same stop signal that stopWorkers uses before releasing the
	// platform call, so cancellation cannot race the test's assertions.
	r.lifecycleMu.Lock()
	stopAdmission := r.cancel
	r.lifecycleMu.Unlock()
	stopAdmission()
	done := make(chan error, 1)
	go func() { done <- r.stopWorkers(ctx) }()
	select {
	case err := <-done:
		t.Fatalf("stop finished before receipt: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(g.release)
	closed = true
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	stored, err := r.journal.LoadEffect(ctx, effect.Owner, effect.Generation, effect.Effect.Effect.ID)
	if err != nil || stored.State != "confirmed" {
		t.Fatalf("publication not confirmed: %v %v", stored.State, err)
	}
	var applied int
	db, err := sql.Open("sqlite", "file:"+filepath.Join(a.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect_applied WHERE effect_id=?`, effect.Effect.Effect.ID).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("receipt not applied: %d %v", applied, err)
	}
	// An effect queued after the stop signal must remain unclaimed for the
	// next runtime rather than starting a new platform write during drain.
	in.EventID, in.MessageID, in.RawContent = "after-stop", "after-stop", `{"text":"/status"}`
	if err = r.hook.Receive(ctx, in); err != nil {
		t.Fatal(err)
	}
	pending, err := r.journal.PendingIngressWorkPage(ctx, 128, 0)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending ingress: %d %v", len(pending), err)
	}
	item := pending[0]
	created, err := r.processor.ApplyIngress(ctx, item.Owner, item.Generation, item.Sequence)
	if err != nil || len(created) == 0 {
		t.Fatalf("prepare later effect: %v", err)
	}
	later := privatechat.PendingEffectWork{Owner: item.Owner, Generation: item.Generation, Effect: created[0]}
	stopped, cancelStopped := context.WithCancel(ctx)
	cancelStopped()
	r.runner.scheduleEffectWithStop(ctx, stopped, later)
	r.runner.effectWG.Wait()
	record, err := r.journal.LoadEffect(ctx, item.Owner, item.Generation, later.Effect.Effect.ID)
	if err != nil || record.State != "prepared" {
		t.Fatalf("post-stop effect was claimed: %s %v", record.State, err)
	}
}
