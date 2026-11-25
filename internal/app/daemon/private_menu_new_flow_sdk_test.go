package daemon

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

type firstMenuUnknownGateway struct {
	*messageIDAssigningGateway
	mu     sync.Mutex
	failed bool
}

func (g *firstMenuUnknownGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	g.mu.Lock()
	fail := !g.failed && len(ops) > 0 && ops[0].Kind == feishu.OperationSendCard
	if fail {
		g.failed = true
	}
	g.mu.Unlock()
	if fail {
		return errors.New("controlled unknown platform result")
	}
	return g.messageIDAssigningGateway.Apply(ctx, ops)
}

func TestPrivateSDKNewMenuNotBlockedByPriorUnknownMenu(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	app.gateway = &firstMenuUnknownGateway{messageIDAssigningGateway: gateway}
	r := app.applications
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errs := make(chan error, 8)
	if err := r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
	}()
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	send := func(id string) {
		t.Helper()
		raw.EventID = id
		raw.MessageID = id
		raw.ReceivedAt = time.Now()
		if err := r.hook.Receive(ctx, raw); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(app.headlessRuntime.Paths.StateDir, "private-chat.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	send("first-menu")
	for {
		var n int
		err = db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect WHERE state='unknown' AND json_extract(body,'$.event.PageView.CommandID')='menu'`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("first menu did not record unknown")
		case <-time.After(20 * time.Millisecond):
		}
	}
	send("second-menu")
	for {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
				return
			}
		}
		select {
		case e := <-errs:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal("new menu blocked by prior unknown")
		case <-time.After(20 * time.Millisecond):
		}
	}
}
