package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/claudeworkspaceprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateClaudeWorkspaceReceiptPersistsOrderedOwnerUpdates(t *testing.T) {
	ctx := context.Background()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "secret"}}
	host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	setTestWorkBotSettings(t, host, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: "default"})
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	if err := host.BindPrivateServiceOwnership(app); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	host.configureClaudeWorkspaceProfileStateLocked(dir)
	j, err := privatechat.OpenJournal(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	owners := []privatechat.Owner{{GatewayID: "gateway", SurfaceID: "surface-a", ChatID: "chat-a", UserID: "a", TenantID: "tenant", AppID: "app"}, {GatewayID: "gateway", SurfaceID: "surface-b", ChatID: "chat-b", UserID: "b", TenantID: "tenant", AppID: "app"}}
	var ids []string
	for i, o := range owners {
		if err = host.refreshPrivateBotSettings(ctx, p, o, 1); err != nil {
			t.Fatal(err)
		}
		s := app.Service().Surface(o.SurfaceID)
		s.ProductMode, s.Backend, s.ClaudeProfileID = state.ProductModeNormal, agentproto.BackendClaude, "default"
		s.ClaimedWorkspaceKey = "/workspace"
		text := "/access confirm"
		if i == 1 {
			text = "/access full"
		}
		a := control.Action{Kind: control.ActionAccessCommand, Text: text, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "access-" + o.UserID, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		effects, e := p.ApplyIngress(ctx, o, 1, seq)
		if e != nil {
			t.Fatal(e)
		}
		var id string
		for _, effect := range effects {
			event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
			if e != nil {
				t.Fatal(e)
			}
			if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateClaudeWorkspace {
				id = effect.Effect.ID
			}
		}
		if id == "" {
			t.Fatalf("missing workspace write for %s: %#v", o.UserID, effects)
		}
		ids = append(ids, id)
	}
	key := state.ClaudeWorkspaceProfileSnapshotStorageKey("/workspace", agentproto.BackendClaude, "default")
	if _, ok := host.claudeWorkspaceProfileState.store.Get(key); ok {
		t.Fatal("workspace changed before durable effect")
	}
	if err = runPrivateClaudeWorkspace(ctx, host, p, j, owners[1], 1, ids[1]); err != privatechat.ErrDeferred {
		t.Fatalf("later owner should wait: %v", err)
	}
	// A confirmed write may outlive its owner's checkpoint projection.
	if err = j.MarkEffectSending(ctx, owners[0], 1, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err = host.claudeWorkspaceProfileState.store.Put(key, state.ClaudeWorkspaceProfileSnapshotRecord{AccessMode: agentproto.AccessModeConfirm}); err != nil {
		t.Fatal(err)
	}
	first, err := j.LoadEffect(ctx, owners[0], 1, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	firstEvent, _, err := privatechat.DecodeStoredEffect(first.Effect.Body)
	if err != nil {
		t.Fatal(err)
	}
	var firstChange orchestrator.PrivateClaudeSnapshotChange
	if err = json.Unmarshal([]byte(firstEvent.DaemonCommand.Text), &firstChange); err != nil {
		t.Fatal(err)
	}
	firstReceipt, _ := json.Marshal(privateClaudeWorkspaceReceipt{Version: 1, EffectID: ids[0], Change: firstChange, Actual: state.ClaudeWorkspaceProfileSnapshotRecord{AccessMode: agentproto.AccessModeConfirm}})
	if err = j.ConfirmLocalEffect(ctx, owners[0], 1, ids[0], firstReceipt, privateLocalVerifier{owners[0], 1, first.Effect, firstReceipt}); err != nil {
		t.Fatal(err)
	}
	if err = runPrivateClaudeWorkspace(ctx, host, p, j, owners[1], 1, ids[1]); err != nil {
		t.Fatal(err)
	}
	store, err := claudeworkspaceprofile.LoadStore(claudeworkspaceprofile.StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := store.Get(key); !ok || got.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("disk snapshot: %#v %v", got, ok)
	}
	if err = runPrivateClaudeWorkspace(ctx, host, p, j, owners[0], 1, ids[0]); err != nil {
		t.Fatalf("receipt replay: %v", err)
	}
	store, err = claudeworkspaceprofile.LoadStore(claudeworkspaceprofile.StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(key); got.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatalf("old receipt overwrote new store: %#v", got)
	}
	// An unconfirmed preference is owner-local and must survive restart so a
	// subsequent change computes its delta against the correct value.
	a := control.Action{Kind: control.ActionAccessCommand, Text: "/access confirm", GatewayID: owners[0].GatewayID, SurfaceSessionID: owners[0].SurfaceID, ChatID: owners[0].ChatID, ActorUserID: owners[0].UserID}
	raw, _ := json.Marshal(a)
	seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: owners[0], Generation: 1, EventID: "pending-before-restart", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	firstPending, err := p.ApplyIngress(ctx, owners[0], 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var firstPendingID string
	for _, effect := range firstPending {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateClaudeWorkspace {
			firstPendingID = effect.Effect.ID
		}
	}
	if firstPendingID == "" {
		t.Fatal("missing pending change")
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = privatechat.OpenJournal(filepath.Join(dir, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	app = privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	if err = host.BindPrivateServiceOwnership(app); err != nil {
		t.Fatal(err)
	}
	host.configureClaudeWorkspaceProfileStateLocked(dir)
	p, err = privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.refreshPrivateBotSettings(ctx, p, owners[0], 1); err != nil {
		t.Fatal(err)
	}
	a.Text = "/access clear"
	raw, _ = json.Marshal(a)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: owners[0], Generation: 1, EventID: "after-restart", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := p.ApplyIngress(ctx, owners[0], 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	var middleID string
	for _, effect := range effects {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateClaudeWorkspace {
			continue
		}
		var change orchestrator.PrivateClaudeSnapshotChange
		if e = json.Unmarshal([]byte(event.DaemonCommand.Text), &change); e != nil {
			t.Fatal(e)
		}
		if change.Before.AccessMode != agentproto.AccessModeConfirm || change.After.AccessMode != "" {
			t.Fatalf("restored overlay: %#v", change)
		}
		found = true
		middleID = effect.Effect.ID
	}
	if !found {
		t.Fatal("missing change after restart")
	}
	a.Text = "/access confirm"
	raw, _ = json.Marshal(a)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: owners[0], Generation: 1, EventID: "aba-last", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyIngress(ctx, owners[0], 1, seq); err != nil {
		t.Fatal(err)
	}
	// Recover a write that reached disk before its durable receipt.
	if err = j.MarkEffectSending(ctx, owners[0], 1, firstPendingID); err != nil {
		t.Fatal(err)
	}
	if err = host.claudeWorkspaceProfileState.store.Put(key, state.ClaudeWorkspaceProfileSnapshotRecord{AccessMode: agentproto.AccessModeConfirm}); err != nil {
		t.Fatal(err)
	}
	if err = runPrivateClaudeWorkspace(ctx, host, p, j, owners[0], 1, firstPendingID); err != nil {
		t.Fatal(err)
	}
	if err = runPrivateClaudeWorkspace(ctx, host, p, j, owners[0], 1, middleID); err != nil {
		t.Fatal(err)
	}
	a.Text = "/access full"
	raw, _ = json.Marshal(a)
	seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: owners[0], Generation: 1, EventID: "aba-after-receipts", Lane: "control", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	effects, err = p.ApplyIngress(ctx, owners[0], 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, effect := range effects {
		event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand == nil || event.DaemonCommand.Kind != control.DaemonCommandPrivateClaudeWorkspace {
			continue
		}
		var change orchestrator.PrivateClaudeSnapshotChange
		if e = json.Unmarshal([]byte(event.DaemonCommand.Text), &change); e != nil {
			t.Fatal(e)
		}
		if change.Before.AccessMode != agentproto.AccessModeConfirm {
			t.Fatalf("ABA overlay lost after older receipts: %#v", change)
		}
		found = true
	}
	if !found {
		t.Fatal("missing ABA successor change")
	}
}
