package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
)

var errPrivateFixtureApplicationChanged = errors.New("configured application changed")

type privatePublicationGatewaySwap struct {
	feishu.Gateway
	appID       string
	afterSelect func()
}

func (g *privatePublicationGatewaySwap) PrivatePublicationGateway(gatewayID, appID string) (feishu.Gateway, error) {
	if gatewayID != "gateway" || appID != g.appID {
		return nil, errPrivateFixtureApplicationChanged
	}
	selected := g.Gateway
	if g.afterSelect != nil {
		g.afterSelect()
	}
	return selected, nil
}

func TestPrivateFrozenPublicationRespectsCurrentAuthority(t *testing.T) {
	app, original := newApplicationRuntimeTestApp(t)
	r := app.applications
	ctx := t.Context()
	user := sdkJourneyUser{t: t, ctx: ctx, gateway: original}
	user.send(app, "freeze-menu", "/menu")
	pending, err := r.journal.PendingIngressWork(ctx, 1)
	if err != nil || len(pending) != 1 {
		t.Fatalf("SDK ingress did not prepare menu: %v %v", pending, err)
	}
	item := pending[0]
	effects, err := r.processor.ApplyIngress(ctx, item.Owner, item.Generation, item.Sequence)
	if err != nil || len(effects) != 1 {
		t.Fatalf("apply menu ingress: %v %v", effects, err)
	}
	id := effects[0].Effect.ID
	if err = r.publisher.prepare(ctx, item.Owner, item.Generation, id); err != nil {
		t.Fatal(err)
	}
	replacement := &messageIDAssigningGateway{}
	selector := &privatePublicationGatewaySwap{Gateway: replacement, appID: "replacement-app"}
	app.gateway = selector
	assertUnstarted := func() {
		t.Helper()
		publication, err := r.journal.LoadPublication(ctx, item.Owner, item.Generation, id)
		if err != nil || publication.EffectState != "prepared" || len(publication.Operations) != 1 || publication.Operations[0].State != "prepared" {
			t.Fatalf("unsent publication must remain prepared: %v %v", publication, err)
		}
		if got := len(original.snapshotOperations()) + len(replacement.snapshotOperations()); got != 0 {
			t.Fatalf("invalid publication reached a gateway: writes=%d", got)
		}
	}
	if _, err = r.publisher.Publish(ctx, item.Owner, item.Generation, id); !errors.Is(err, errPrivateFixtureApplicationChanged) {
		t.Fatalf("replacement application was not rejected: %v", err)
	}
	assertUnstarted()

	// Restore the gateway but advance the exact owner epoch. The adapter's
	// physical application identity alone does not authorize the frozen card.
	selector.Gateway, selector.appID = original, item.Owner.AppID
	binding := r.publisher.ownership.(*privatePublicationBinding)
	current := binding.bindingFor
	binding.bindingFor = func(ctx context.Context, owner privatechat.Owner, generation int64) (ingressowner.PrivateBinding, error) {
		bound, err := current(ctx, owner, generation)
		bound.Owner.Epoch++
		return bound, err
	}
	if _, err = r.publisher.Publish(ctx, item.Owner, item.Generation, id); !errors.Is(err, ingressowner.ErrConflict) {
		t.Fatalf("new owner epoch was not rejected: %v", err)
	}
	assertUnstarted()

	binding.bindingFor = current
	// Replacement after selection must not substitute the captured transport.
	selector.afterSelect = func() {
		selector.Gateway, selector.appID = replacement, "replacement-app"
	}
	if _, err = r.publisher.Publish(ctx, item.Owner, item.Generation, id); err != nil {
		t.Fatal(err)
	}
	ops := original.snapshotOperations()
	if len(ops) != 1 || ops[0].Kind != feishu.OperationSendCard || ops[0].MessageID == "" || len(replacement.snapshotOperations()) != 0 {
		t.Fatalf("publication did not use only the original selected gateway: original=%d replacement=%d", len(ops), len(replacement.snapshotOperations()))
	}
	effect, err := r.journal.LoadEffect(ctx, item.Owner, item.Generation, id)
	if err != nil || effect.State != "confirmed" {
		t.Fatalf("original valid publication was not confirmed: %s %v", effect.State, err)
	}
}
