package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type testPrivateSettingsLease struct {
	owner      privatechat.Owner
	generation int64
}

type privateSettingsNativeAuthority struct{}

func (privateSettingsNativeAuthority) WithNativeLease(_ context.Context, _ agentproto.NativeOwner, _ privatechat.Owner, _ int64, fn func() error) error {
	return fn()
}

func (l testPrivateSettingsLease) WithPrivateOwner(_ context.Context, o privatechat.Owner, g int64, fn func() error) error {
	if o != l.owner || g != l.generation {
		return privatechat.ErrInvalidOwner
	}
	return fn()
}

func TestPrivateBotSettingsOwnerCASUsesSharedFile(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	first, err := app.readPrivateBotSettings("gateway", "cli_team")
	if err != nil {
		t.Fatal(err)
	}
	next := first.Record
	next.PromptOverride.Model = "model-one"
	updated, err := app.compareAndSwapPrivateBotSettings("gateway", "cli_team", "user-one", first.Fingerprint, next)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Record.PromptOverride.Model != "model-one" || updated.Revision <= first.Revision {
		t.Fatalf("updated: %#v", updated)
	}
	replayed, err := app.compareAndSwapPrivateBotSettings("gateway", "cli_team", "user-one", first.Fingerprint, next)
	if err != nil || replayed.Record.UpdatedAt != updated.Record.UpdatedAt || replayed.Revision != updated.Revision {
		t.Fatalf("replay rewrote settings: %#v %v", replayed, err)
	}
	if _, err = app.compareAndSwapPrivateBotSettings("gateway", "cli_team", "user-two", first.Fingerprint, first.Record); err == nil {
		t.Fatal("stale owner overwrote newer setting")
	}
	loaded, err := botcapabilitysettings.LoadStore(app.botCapabilitySettingsState.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	record, ok := loaded.Get(state.BotCapabilitySettingsKey("gateway"))
	if !ok || record.PromptOverride.Model != "model-one" {
		t.Fatalf("file did not retain update: %#v", record)
	}
	other, err := app.readPrivateBotSettings("gateway", "cli_team")
	if err != nil || other.Fingerprint != updated.Fingerprint {
		t.Fatalf("cross-owner read: %#v %v", other, err)
	}
	private := privatechat.New(nil, orchestrator.Config{})
	app.service = private.Service()
	if err = app.BindPrivateServiceOwnership(private); err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(private, j, privateHostTestDecoder{}, privateHostReducer{app})
	if err != nil {
		t.Fatal(err)
	}
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "chat", "p2p", "user-one")
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "chat", UserID: "user-one", TenantID: "tenant", AppID: "cli_team"}
	if err = app.refreshPrivateBotSettings(context.Background(), p, o, 1); err != nil {
		t.Fatal(err)
	}
	if got := private.Service().Surface(surface); got == nil || got.PromptOverride.Model != "model-one" {
		t.Fatalf("owner projection: %#v", got)
	}
	stale := other.Record
	stale.PromptOverride.Model = "stale"
	if err = p.RefreshBotSettings(context.Background(), o, 1, stale, func() bool { return false }); err == nil {
		t.Fatal("stale config projection accepted")
	}
	if got := private.Service().Surface(surface); got.PromptOverride.Model != "model-one" {
		t.Fatal("stale snapshot changed private projection")
	}
}

func TestPrivateModelSettingWaitsForOwnerReceipt(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	if err := host.BindPrivateServiceOwnership(app); err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	surface := gatewaypkg.SurfaceIDForInbound("gateway", "chat", "p2p", "user")
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: surface, ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "cli_team"}
	if err = host.refreshPrivateBotSettings(ctx, p, o, 1); err != nil {
		t.Fatal(err)
	}
	otherOwner := o
	otherOwner.UserID = "user-two"
	otherOwner.ChatID = "chat-two"
	otherOwner.SurfaceID = gatewaypkg.SurfaceIDForInbound("gateway", "chat-two", "p2p", "user-two")
	if err = host.refreshPrivateBotSettings(ctx, p, otherOwner, 1); err != nil {
		t.Fatal(err)
	}
	menu := control.Action{Kind: control.ActionShowCommandMenu, Text: "/menu", GatewayID: otherOwner.GatewayID, SurfaceSessionID: otherOwner.SurfaceID, ChatID: otherOwner.ChatID, ActorUserID: otherOwner.UserID}
	menuRaw, _ := json.Marshal(menu)
	menuSeq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: otherOwner, Generation: 1, EventID: "other-menu", Lane: "control", Payload: menuRaw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, otherOwner, 1, menuSeq); err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionModelCommand, Text: "/model model-one", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "model", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, effect := range effects {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateBotSettings {
			id = effect.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("missing owner config intent")
	}
	before, err := host.readPrivateBotSettings("gateway", "cli_team")
	if err != nil || before.Record.PromptOverride.Model == "model-one" {
		t.Fatalf("settings changed before receipt: %#v %v", before, err)
	}
	follow, err := runPrivateBotSettings(ctx, host, p, j, o, 1, id)
	if err != nil || len(follow) == 0 {
		t.Fatalf("config completion: %v", err)
	}
	after, err := host.readPrivateBotSettings("gateway", "cli_team")
	if err != nil || after.Record.PromptOverride.Model != "model-one" {
		t.Fatalf("settings not persisted: %#v %v", after, err)
	}
	versions, err := j.PendingSettingsReconciliations(ctx, otherOwner, 1, 64)
	if err != nil || len(versions) != 1 || versions[0] != after.Fingerprint {
		t.Fatalf("other owner fanout: %#v %v", versions, err)
	}
	if got := app.Service().Surface(otherOwner.SurfaceID).PromptOverride.Model; got == "model-one" {
		t.Fatal("other owner mutated before reconcile")
	}
	if err = reconcilePrivateBotSettings(ctx, host, p, j, testPrivateSettingsLease{otherOwner, 1}, otherOwner, 1); err != nil {
		t.Fatal(err)
	}
	if got := app.Service().Surface(otherOwner.SurfaceID).PromptOverride.Model; got != "model-one" {
		t.Fatalf("other owner missed setting: %s", got)
	}
	if _, err = p.ApplySettingsReconcile(ctx, otherOwner, 1, versions[0], after.Fingerprint, func() bool { return true }, nil); err != nil {
		t.Fatalf("reconcile replay: %v", err)
	}
	if _, err = runPrivateBotSettings(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatal(err)
	}
	replayed, err := host.readPrivateBotSettings("gateway", "cli_team")
	if err != nil || replayed.Record.UpdatedAt != after.Record.UpdatedAt {
		t.Fatal("replay rewrote config")
	}
	access := control.Action{Kind: control.ActionAccessCommand, Text: "/access full", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ = json.Marshal(access)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "access", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil || checkpoint.Core.PromptOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("access not durable: %q %v", checkpoint.Core.PromptOverride.AccessMode, err)
	}
	a.Text = "/model model-two"
	raw, _ = json.Marshal(a)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "model-two", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err = p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	id = ""
	for _, effect := range effects {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateBotSettings {
			id = effect.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("second settings intent missing")
	}
	concurrent, err := host.readPrivateBotSettings("gateway", "cli_team")
	if err != nil {
		t.Fatal(err)
	}
	newer := concurrent.Record
	newer.PromptOverride.Model = "model-three"
	if _, err = host.compareAndSwapPrivateBotSettings("gateway", "cli_team", "other-user", concurrent.Fingerprint, newer); err != nil {
		t.Fatal(err)
	}
	follow, err = runPrivateBotSettings(ctx, host, p, j, o, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	conflict := false
	for _, effect := range follow {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Notice != nil && event.Notice.Code == "private_settings_conflict" {
			conflict = true
		}
	}
	if !conflict {
		t.Fatal("concurrent settings change was not reported")
	}
	after, err = host.readPrivateBotSettings("gateway", "cli_team")
	if err != nil || after.Record.PromptOverride.Model != "model-three" {
		t.Fatalf("concurrent setting overwritten: %v", err)
	}
}

func TestFreshPrivateModeWaitsForOwnerReceipt(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	if err := host.BindPrivateServiceOwnership(app); err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "chat", "p2p", "user"), ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "cli_team"}
	if err = host.refreshPrivateBotSettings(ctx, p, o, 1); err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionModeCommand, Text: "/mode claude", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "mode", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateBotSettings {
			id = e.Effect.ID
		}
	}
	if id == "" {
		t.Fatal("mode intent missing")
	}
	input := control.Action{Kind: control.ActionTextMessage, Text: "wait", MessageID: "input", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ = json.Marshal(input)
	inputSeq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "input", Lane: "input", Payload: raw, Sources: []string{"input"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, inputSeq); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("input was not held: %v", err)
	}
	if _, err = runPrivateBotSettings(ctx, host, p, j, o, 1, id); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, inputSeq); err != nil {
		t.Fatal(err)
	}
}

func TestAttachedPrivateModeTransitionsOnlyAfterSettingsReceipt(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	if err := host.BindPrivateServiceOwnership(app); err != nil {
		t.Fatal(err)
	}
	j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "chat", "p2p", "user"), ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "cli_team"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	lease, err := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	if err = p.RegisterNativeHello(ctx, lease, hello); err != nil {
		t.Fatal(err)
	}
	if err = host.refreshPrivateBotSettings(ctx, p, o, 1); err != nil {
		t.Fatal(err)
	}
	attach := control.Action{Kind: control.ActionAttachInstance, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, InstanceID: "instance"}
	raw, _ := json.Marshal(attach)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "attach", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
		t.Fatal(err)
	}
	a := control.Action{Kind: control.ActionModeCommand, Text: "/mode claude", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
	raw, _ = json.Marshal(a)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "mode", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, e := range effects {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateBotSettings {
			id = e.Effect.ID
		}
	}
	if id == "" || app.Service().Surface(o.SurfaceID).AttachedInstanceID != "instance" {
		t.Fatal("preview changed attachment or missed intent")
	}
	follow, err := runPrivateBotSettings(ctx, host, p, j, o, 1, id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range follow {
		event, e2 := privatechat.DecodeEffectEvent(e.Effect.Body)
		if e2 != nil {
			t.Fatal(e2)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			found = true
		}
	}
	if !found {
		t.Fatal("receipt did not emit original headless continuation")
	}
}
