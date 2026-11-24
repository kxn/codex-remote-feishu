package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type invalidSettingsCatalog struct{ newWorkspaceJourneyCatalog }

func (c invalidSettingsCatalog) RecentThreadsForBackend(_ agentproto.Backend, limit int) ([]state.ThreadRecord, error) {
	return c.RecentThreads(limit)
}
func (c invalidSettingsCatalog) RecentWorkspacesForBackend(_ agentproto.Backend, limit int) (map[string]time.Time, error) {
	return c.RecentWorkspaces(limit)
}
func (c invalidSettingsCatalog) ThreadByIDForBackend(_ agentproto.Backend, id string) (*state.ThreadRecord, error) {
	return c.ThreadByID(id)
}

func TestInvalidPrivateBackendSettingsStillAllowMenuAndRepair(t *testing.T) {
	for _, mode := range []string{"claude-missing", "opencode-missing", "claude-preferences-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			backend := agentproto.BackendClaude
			if mode == "opencode-missing" {
				backend = agentproto.BackendOpenCode
			}
			cfg := config.DefaultAppConfig()
			cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
			host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
			initial, err := host.readPrivateBotSettings("gateway", "cli_team")
			if err != nil {
				t.Fatal(err)
			}
			broken := initial.Record
			broken.Backend = backend
			if mode == "claude-missing" {
				broken.ClaudeProfileID = "missing-profile"
			} else if mode == "opencode-missing" {
				broken.OpenCodeProfileID = "missing-profile"
			}
			if err = host.botCapabilitySettingsState.store.Put(broken); err != nil {
				t.Fatal(err)
			}
			host.privateSettingsRevision.Add(1)
			if mode == "claude-preferences-unavailable" {
				host.profileContextPreferenceState.store = nil
			}
			if got := host.resolveWorkBotSettings(broken, cfg); got.err == nil {
				t.Fatal("fixture backend unexpectedly valid")
			}
			if backend == agentproto.BackendClaude {
				if _, _, launchErr := host.applyClaudeHeadlessProfileEnv(nil, backend, broken.ClaudeProfileID); launchErr == nil {
					t.Fatal("invalid launch did not report error")
				}
			} else if _, launchErr := resolveOpenCodeLaunchProfile(cfg, broken.OpenCodeProfileID, nil); launchErr == nil {
				t.Fatal("invalid launch did not report error")
			}
			snapshot, err := host.readPrivateBotSettings("gateway", "cli_team")
			if err != nil {
				t.Fatal(err)
			}
			private := privatechat.New(nil, orchestrator.Config{})
			host.service = private.Service()
			if err = host.BindPrivateServiceOwnership(private); err != nil {
				t.Fatal(err)
			}
			j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			p, err := privatechat.NewProcessor(private, j, privateHostTestDecoder{}, privateHostReducer{host})
			if err != nil {
				t.Fatal(err)
			}
			o := privatechat.Owner{GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "cli_team"}
			if err = host.refreshPrivateBotSettings(context.Background(), p, o, 1); err != nil {
				t.Fatal(err)
			}
			action := control.Action{Kind: control.ActionShowCommandMenu, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
			raw, _ := json.Marshal(action)
			seq, _, err := j.Admit(context.Background(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "menu", Lane: "control", Payload: raw})
			if err != nil {
				t.Fatal(err)
			}
			effects, err := p.ApplyIngress(context.Background(), o, 1, seq)
			if err != nil || len(effects) == 0 {
				t.Fatalf("menu effects=%d err=%v", len(effects), err)
			}
			repaired := snapshot.Record
			repaired.Backend = agentproto.BackendCodex
			if _, err = host.compareAndSwapPrivateBotSettings("gateway", "cli_team", "user", snapshot.Fingerprint, repaired); err != nil {
				t.Fatalf("repair: %v", err)
			}
			if _, err = host.readPrivateBotSettings("gateway", "wrong-app"); err == nil {
				t.Fatal("wrong bot identity accepted")
			}
		})
	}
}

func TestInvalidBackendSettingsSDKMenuAndAsyncModeRepair(t *testing.T) {
	for _, mode := range []string{"claude-missing", "opencode-missing", "claude-preferences-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			backend := agentproto.BackendClaude
			if mode == "opencode-missing" {
				backend = agentproto.BackendOpenCode
			}
			app, gateway := newApplicationRuntimeTestApp(t)
			broken := state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: backend}
			if mode == "claude-missing" {
				broken.ClaudeProfileID = "missing-profile"
			} else if mode == "opencode-missing" {
				broken.OpenCodeProfileID = "missing-profile"
			}
			setTestWorkBotSettings(t, app, broken)
			if mode == "claude-preferences-unavailable" {
				app.profileContextPreferenceState.store = nil
			}
			r := app.applications
			errs := make(chan error, 4)
			if err := r.startWorkers(t.Context(), errs); err != nil {
				t.Fatal(err)
			}
			raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "menu", MessageID: "message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
			if err := r.hook.Receive(t.Context(), raw); err != nil {
				t.Fatal(err)
			}
			var card feishu.Operation
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				for _, op := range gateway.snapshotOperations() {
					if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
						card = op
						break
					}
				}
				if card.MessageID != "" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if card.MessageID == "" {
				t.Fatal("menu not published with invalid profile")
			}
			for deadline := time.Now().Add(5 * time.Second); ; {
				if _, err := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("menu ownership not registered")
				}
				time.Sleep(10 * time.Millisecond)
			}
			frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err = json.Unmarshal(frozen, &doc); err != nil {
				t.Fatal(err)
			}
			value := assembledMenuValue(doc, "current_work")
			if value == nil {
				t.Fatal("menu action missing")
			}
			click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
			response, err := r.hook.ReceiveInteractionResponse(t.Context(), click)
			if err != nil || !strings.Contains(string(response.RawBody), "基本命令") {
				t.Fatalf("sync card: %s %v", response.RawBody, err)
			}
			raw.EventID = "repair"
			raw.MessageID = "repair-message"
			raw.RawContent = `{"text":"/mode codex"}`
			if err = r.hook.Receive(t.Context(), raw); err != nil {
				t.Fatal(err)
			}
			for deadline := time.Now().Add(5 * time.Second); ; {
				current, e := app.readPrivateBotSettings("gateway", "app")
				if e == nil && current.Record.Backend == agentproto.BackendCodex {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("async repair not saved: %v", e)
				}
				time.Sleep(10 * time.Millisecond)
			}
			select {
			case err := <-errs:
				t.Fatal(err)
			default:
			}
		})
	}
}

func TestInvalidProfileStartupFailureStillAllowsSDKRepair(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	r := app.applications
	cwd := t.TempDir()
	app.service.SetPersistedThreadCatalog(invalidSettingsCatalog{newWorkspaceJourneyCatalog{cwd: cwd}})
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude, ClaudeProfileID: "missing-profile"})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	errs := make(chan error, 4)
	if err := r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "list", MessageID: "source", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/list"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	for card.MessageID == "" {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
				raw, e := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
				var doc any
				if e == nil && json.Unmarshal(raw, &doc) == nil && findPrivateCallback(doc, "target_picker_confirm") != nil {
					card = op
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker missing")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		if _, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("picker not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(frozen, &doc); err != nil {
		t.Fatal(err)
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", ReceivedAt: time.Now()}
	if v := findPrivateCallback(doc, "target_picker_select_workspace"); v != nil {
		click.EventID = "select"
		click.Values = v
		click.Option = cwd
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &doc); e != nil {
			t.Fatal(e)
		}
	}
	click.EventID = "confirm"
	click.Values = findPrivateCallback(doc, "target_picker_confirm")
	click.Option = ""
	if click.Values == nil {
		t.Fatal("confirm missing")
	}
	if _, err = r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	for {
		for _, op := range gateway.snapshotOperations() {
			b, _ := json.Marshal(op)
			if strings.Contains(string(b), "missing-profile") {
				goto failed
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("specific startup failure not published: %+v", gateway.snapshotOperations())
		case <-time.After(10 * time.Millisecond):
		}
	}
failed:
	raw.EventID = "repair"
	raw.MessageID = "repair-message"
	raw.RawContent = `{"text":"/mode codex"}`
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	for {
		current, e := app.readPrivateBotSettings("gateway", "app")
		if e == nil && current.Record.Backend == agentproto.BackendCodex {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("repair did not complete after failure")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
