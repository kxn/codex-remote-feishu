package daemon

import (
	"context"
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

type privateHostReceiptTestVerifier struct{}

func (privateHostReceiptTestVerifier) VerifyPublicationResult(context.Context, privatechat.Owner, int64, string, int, json.RawMessage, json.RawMessage) error {
	return nil
}

func TestPrivateHostReceiptPersistsActualCardMessage(t *testing.T) {
	ctx := context.Background()
	gateway := &messageIDAssigningGateway{}
	host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil || len(effects) == 0 {
		t.Fatalf("menu: %v", err)
	}
	effect := effects[0].Effect
	event, err := privatechat.DecodeEffectEvent(effect.Body)
	if err != nil {
		t.Fatal(err)
	}
	event.DaemonLifecycleID = host.daemonLifecycleID
	plan, err := privatechat.EncodeEffectEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	operations := host.projector.ProjectEvent("c", event)
	if len(operations) != 1 {
		t.Fatalf("projection: %#v", operations)
	}
	operations[0].GatewayID, operations[0].SurfaceSessionID, operations[0].ChatID = "g", "s", "c"
	frozen, err := feishu.FreezePrivateOperation(operations[0], "test-transport")
	if err != nil {
		t.Fatal(err)
	}
	if err = j.PreparePublication(ctx, o, 1, effect.ID, plan, []json.RawMessage{frozen}); err != nil {
		t.Fatal(err)
	}
	if err = j.MarkEffectSending(ctx, o, 1, effect.ID); err != nil {
		t.Fatal(err)
	}
	if err = j.BeginPublicationOperation(ctx, o, 1, effect.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err = gateway.Apply(ctx, operations); err != nil {
		t.Fatal(err)
	}
	opReceipt, err := feishu.PrivateReceiptForOperation(operations[0], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	receipt, _ := json.Marshal(opReceipt)
	if err = j.CompletePublicationOperation(ctx, o, 1, effect.ID, 0, "confirmed", receipt, privateHostReceiptTestVerifier{}); err != nil {
		t.Fatal(err)
	}
	if _, err = j.FinishPublication(ctx, o, 1, effect.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyHostReceipt(ctx, o, 1, effect.ID, privateHostReceiptReducer{host}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Core.SurfaceMessages[operations[0].MessageID] == nil {
		t.Fatal("actual card message not persisted")
	}
	if _, err = p.ApplyHostReceipt(ctx, o, 1, effect.ID, privateHostReceiptReducer{host}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err = j.LoadCheckpoint(ctx, o, 1)
	if err != nil || checkpoint.Core.SurfaceMessageSeq != 1 {
		t.Fatalf("duplicate recorded: %v", err)
	}
}
