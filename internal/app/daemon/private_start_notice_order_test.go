package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type incidentBlockedGateway struct {
	blocked chan struct{}
	release chan struct{}
	done    chan feishu.OperationKind
}

func (g *incidentBlockedGateway) Start(context.Context, feishu.ActionHandler) error { return nil }
func (g *incidentBlockedGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	for i := range ops {
		if ops[i].Kind == feishu.OperationSendCard {
			close(g.blocked)
			select {
			case <-g.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if ops[i].MessageID == "" {
			ops[i].MessageID = "notice"
		}
		g.done <- ops[i].Kind
	}
	return nil
}

// Publications sharing an operation must complete in causal order.
// Source events are synthetic; gateway Apply is the controlled platform port.
func TestPrivateStartingNoticeCannotArriveAfterReadyCard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	gateway := &incidentBlockedGateway{make(chan struct{}), make(chan struct{}), make(chan feishu.OperationKind, 4)}
	host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	o := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	seed := privatechat.New(nil, orchestrator.Config{})
	if err := seed.RegisterOwner(o); err != nil {
		t.Fatal(err)
	}
	core, err := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
	if err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	notice := eventcontract.Event{Kind: eventcontract.KindNotice, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, Notice: &control.Notice{Code: "headless_starting", Text: "启动中"}}
	ready := eventcontract.Event{Kind: eventcontract.KindTargetPicker, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, TargetPickerView: &control.FeishuTargetPickerView{PickerID: "picker", MessageID: "card", Stage: control.FeishuTargetPickerStageSucceeded, StatusTitle: "已进入新会话待命"}}
	for i, event := range []eventcontract.Event{notice, ready} {
		raw, e := privatechat.EncodeEffectEvent(event)
		if e != nil {
			t.Fatal(e)
		}
		if e = j.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: 1, Revision: int64(i + 1), Core: core}, int64(i), []privatechat.Effect{{ID: fmt.Sprintf("effect-%d", i), Lane: "interactive", Body: raw, OrderKeys: []string{"workspace-operation"}}}); e != nil {
			t.Fatal(e)
		}
	}
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	publisher := &privatePublisher{host: host, processor: p, journal: j}
	result := make(chan error, 2)
	go func() { _, e := publisher.Publish(ctx, o, 1, "effect-0"); result <- e }()
	select {
	case <-gateway.blocked:
	case <-ctx.Done():
		t.Fatal("notice not blocked")
	}
	if _, err := publisher.Publish(ctx, o, 1, "effect-1"); err != privatechat.ErrDisplayHeld {
		t.Fatalf("success publication bypassed earlier pending operation: %v", err)
	}
	select {
	case kind := <-gateway.done:
		t.Fatalf("publication overtook pending predecessor: %s", kind)
	default:
	}
	close(gateway.release)
	select {
	case kind := <-gateway.done:
		if kind != feishu.OperationSendCard {
			t.Fatalf("unexpected %s", kind)
		}
	case <-ctx.Done():
		t.Fatal("notice did not finish")
	}
	if e := <-result; e != nil {
		t.Fatal(e)
	}
	if _, e := publisher.Publish(ctx, o, 1, "effect-1"); e != nil {
		t.Fatal(e)
	}
	select {
	case kind := <-gateway.done:
		if kind != feishu.OperationUpdateCard {
			t.Fatalf("wrong final publication: %s", kind)
		}
	case <-ctx.Done():
		t.Fatal("ready card missing after predecessor completed")
	}
}
