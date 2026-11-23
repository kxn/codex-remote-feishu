package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type privateHostTestDecoder struct{}

func (privateHostTestDecoder) Decode(e privatechat.JournalEntry) (control.Action, error) {
	var a control.Action
	err := json.Unmarshal(e.Ingress.Payload, &a)
	return a, err
}

type privatePickerFixture struct {
	privateHostReducer
	root, picker string
}

func (r *privatePickerFixture) Reduce(app *privatechat.App, a control.Action) ([]eventcontract.Event, error) {
	if a.Kind == control.ActionStatus {
		events := app.Service().OpenPathPicker(a, control.PathPickerRequest{Mode: control.PathPickerModeDirectory, RootPath: r.root})
		if len(events) == 1 && events[0].PathPickerView != nil {
			r.picker = events[0].PathPickerView.PickerID
			app.Service().RecordPathPickerMessage(a.SurfaceSessionID, r.picker, "card")
		}
		return events, nil
	}
	return r.privateHostReducer.Reduce(app, a)
}

func TestPrivateHostPickerReplayFreezesObservedDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alpha"), 0700); err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "journal")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r := &privatePickerFixture{privateHostReducer: privateHostReducer{host}, root: root}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, r)
	if err != nil {
		t.Fatal(err)
	}
	admit := func(id string, action control.Action) int64 {
		raw, _ := json.Marshal(action)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	base := control.Action{Kind: control.ActionStatus, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"}
	if _, err := p.ApplyIngress(ctx, owner, 1, admit("open", base)); err != nil {
		t.Fatal(err)
	}
	if r.picker == "" {
		t.Fatal("picker not opened")
	}
	base.Kind = control.ActionPathPickerPage
	base.PickerID = r.picker
	base.FieldName = "path_picker_directory"
	base.Inbound = &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "card", CardDaemonLifecycleID: host.daemonLifecycleID}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	host.daemonLifecycleID = "new-lifecycle"
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, r)
	if err != nil {
		t.Fatal(err)
	}
	seq := admit("page", base)
	first, err := p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "later"), 0700); err != nil {
		t.Fatal(err)
	}
	second, err := p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatal("picker replay changed effects")
	}
	card, err := p.ReadCommittedCardResponse(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(card.Response), "alpha") {
		t.Fatalf("picker was not restored: %s", card.Response)
	}
	for i := range first {
		if string(first[i].Effect.Body) != string(second[i].Effect.Body) {
			t.Fatal("replay observed changed directory")
		}
	}
}

func TestPrivateHostSDKMenuResponseIsCommittedAndReplayable(t *testing.T) {
	ctx := context.Background()
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	surface := gatewaypkg.SurfaceIDForInbound("app-1", "oc_chat", "p2p", "ou_user")
	owner := privatechat.Owner{GatewayID: "app-1", SurfaceID: surface, ChatID: "oc_chat", UserID: "ou_user", TenantID: "tenant", AppID: "bot"}
	path := filepath.Join(t.TempDir(), "journal")
	j, err := privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	callback := &larkcallback.CardActionTriggerEvent{Event: &larkcallback.CardActionTriggerRequest{Operator: &larkcallback.Operator{OpenID: "ou_user"}, Context: &larkcallback.Context{OpenChatID: "oc_chat", OpenMessageID: "om-card"}, Action: &larkcallback.CallBackAction{Value: map[string]interface{}{"kind": "page_local_action", "action_kind": string(control.ActionShowCommandMenu), "action_arg": "/menu", "daemon_lifecycle_id": host.daemonLifecycleID}}}}
	action, ok := gatewaypkg.ParseCardActionTriggerEvent(gatewaypkg.RoutingEnv{GatewayID: "app-1", SurfaceForCardAction: func(gatewaypkg.CardActionSurfaceLookup) string { return surface }}, callback)
	if !ok {
		t.Fatal("parse")
	}
	raw, _ := json.Marshal(action)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: "callback", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	card, err := p.ReadCommittedCardResponse(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if card.TargetMessageID != "om-card" || !strings.Contains(string(card.Response), "card") {
		t.Fatalf("card response: %#v effects=%#v", card, effects)
	}
	replay, err := p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay) != len(effects) {
		t.Fatal("replay changed response")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	replay, err = p.ApplyIngress(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := p.ReadCommittedCardResponse(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.TargetMessageID != card.TargetMessageID || string(recovered.Response) != string(card.Response) {
		t.Fatal("restart changed frozen callback")
	}
}

func TestPrivateHostRejectsDifferentService(t *testing.T) {
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	if err := (privateHostReducer{host}).check(privatechat.New(nil, orchestrator.Config{})); err == nil {
		t.Fatal("different service accepted")
	}
}

func TestPrivateHostRejectsOldCardAndForeignRoute(t *testing.T) {
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	if err := app.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionShowCommandMenu, GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u", Inbound: &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "old", CardDaemonLifecycleID: "old"}}
	r := privateHostReducer{host}
	events, err := r.Reduce(app, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "old_card_expired" {
		t.Fatalf("old card: %#v", events)
	}
	a.ChatID = "other"
	if _, err := r.Reduce(app, a); err != privatechat.ErrInvalidOwner {
		t.Fatalf("foreign chat: %v", err)
	}
	a.ChatID = "c"
	a.Kind = control.ActionModeCommand
	a.Text = "/mode vscode"
	if _, err := r.Reduce(app, a); err != privatechat.ErrUnsupportedAction {
		t.Fatalf("vscode: %v", err)
	}
}
