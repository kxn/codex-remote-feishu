package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/shutdownctx"
)

// Exercise the real App.Shutdown entry with the SDK owner binding and private
// publisher, while a worker is deliberately unable to drain in time.
func TestPrivateShutdownNotifiesPersistedUnloadedOwnerBeforeDrainTimeout(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "first_delivery_fails"}[failFirst], func(t *testing.T) { testPrivateShutdownUnloadedOwners(t, failFirst) })
	}
}

func testPrivateShutdownUnloadedOwners(t *testing.T, failFirst bool) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	app.daemonStartedAt = time.Now().UTC()
	app.daemonLifecycleID = "shutdown-fixture"
	for _, user := range []string{"user-one", "user-two"} {
		raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "event-" + user, MessageID: "message-" + user, ChatID: "chat-" + user, ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: user}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
		if err := r.hook.Receive(t.Context(), raw); err != nil {
			t.Fatal(err)
		}
		o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", user), ChatID: "chat-" + user, UserID: user, TenantID: "tenant", AppID: "app"}
		binding, err := r.ingress.privatePublicationBindingFor(t.Context(), o, 1)
		if err != nil {
			t.Fatal(err)
		}
		seed := privatechat.New(nil, orchestrator.Config{})
		if err = seed.RegisterOwner(o); err != nil {
			t.Fatal(err)
		}
		core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
		if err != nil {
			t.Fatal(err)
		}
		if err = r.journal.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: binding.Owner.Epoch, Revision: 1, Core: core}, 0, nil); err != nil {
			t.Fatal(err)
		}
	}

	if failFirst {
		app.gateway = shutdownSelectiveGateway{gateway}
	}
	r.done = make(chan struct{})
	defer close(r.done)
	ctx, setMode := shutdownctx.WithHolder(context.Background())
	setMode(shutdownctx.ModeConsoleClose)
	_ = app.Shutdown(ctx)
	operations := gateway.snapshotOperations()
	found := 0
	chats := map[string]bool{}
	for _, op := range operations {
		if op.Kind == feishu.OperationSendCard && op.CardBody == daemonShutdownNoticeText {
			found++
			chats[op.ChatID] = true
		}
	}
	want := 2
	if failFirst {
		want = 1
	}
	if found != want || !chats["chat-user-two"] || (!failFirst && !chats["chat-user-one"]) {
		t.Fatalf("shutdown notice count incorrect; platform operations=%d", len(operations))
	}
}

type shutdownBlockingGateway struct{ *messageIDAssigningGateway }

func (g shutdownBlockingGateway) Apply(ctx context.Context, _ []feishu.Operation) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestPrivateShutdownNoticeHonorsCallerDeadline(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	app.daemonStartedAt = time.Now().UTC()
	app.daemonLifecycleID = "deadline-fixture"
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "event", MessageID: "message", ChatID: "chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	seed := privatechat.New(nil, orchestrator.Config{})
	if err := seed.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.journal.CommitCheckpoint(t.Context(), privatechat.Checkpoint{Owner: o, Generation: 1, Revision: 1, Core: core}, 0, nil); err != nil {
		t.Fatal(err)
	}
	app.gateway = shutdownBlockingGateway{gateway}
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = app.shutdownApplications(parent)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("caller deadline ignored: %s", elapsed)
	}
}

type shutdownSelectiveGateway struct{ *messageIDAssigningGateway }

func (g shutdownSelectiveGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	for _, op := range ops {
		if op.ChatID == "chat-user-one" {
			return errors.New("controlled failure")
		}
	}
	return g.messageIDAssigningGateway.Apply(ctx, ops)
}
