package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivatePublicationRestartContinuesOnlyUnstartedTail(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "journal.db")
	gateway := &privatePublishGateway{}
	o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user"}
	open := func() (*privatechat.Journal, *privatePublisher) {
		j, err := privatechat.OpenJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
		app := privatechat.New(nil, orchestrator.Config{})
		host.service = app.Service()
		processor, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
		if err != nil {
			t.Fatal(err)
		}
		return j, &privatePublisher{host: host, processor: processor, journal: j}
	}
	j, p := open()
	action := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.processor.ApplyIngress(ctx, o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatalf("effects=%v err=%v", effects, err)
	}
	id := effects[0].Effect.ID
	if err = p.prepare(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	publication, err := j.LoadPublication(ctx, o, 1, id)
	if err != nil || len(publication.Operations) != 1 {
		t.Fatalf("plan=%+v err=%v", publication, err)
	}
	// Crash after the durable effect started, before its first platform call.
	if err = j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, p = open()
	if _, err = p.Publish(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 1 {
		t.Fatalf("calls=%d", gateway.calls)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, p = open()
	defer j.Close()
	if _, err = p.Publish(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 1 {
		t.Fatal("confirmed call replayed")
	}
}

func TestPrivatePublicationRestartAfterReceiptBeforeEffectConfirmation(t *testing.T) {
	ctx := t.Context()
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	gateway := &privatePublishGateway{}
	host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{TenantID: "tenant", AppID: "bot", GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u"}
	a := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil || len(effects) != 1 {
		t.Fatal(err)
	}
	pub := &privatePublisher{host: host, processor: p, journal: j}
	id := effects[0].Effect.ID
	if err = pub.prepare(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	plan, err := j.LoadPublication(ctx, o, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(ctx, o, 1, id, 0); err != nil {
		t.Fatal(err)
	}
	op, err := feishu.ThawPrivateOperation(plan.Operations[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	batch := []feishu.Operation{op}
	if err = gateway.Apply(ctx, batch); err != nil {
		t.Fatal(err)
	}
	receipt, err := feishu.PrivateReceiptForOperation(batch[0], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	evidence, _ := json.Marshal(receipt)
	v := privatePublicationResult{o: o, generation: 1, id: id, ordinal: 0, body: plan.Operations[0].Body, receipt: evidence}
	if err = j.CompletePublicationOperation(ctx, o, 1, id, 0, "confirmed", evidence, v); err != nil {
		t.Fatal(err)
	}
	if _, err = pub.Publish(ctx, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 1 {
		t.Fatal("receipt-confirmed operation repeated")
	}
}
