package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateBackendSettingActionsDoNotRemainPending(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		for _, tc := range []struct {
			name string
			kind control.ActionKind
			text string
		}{{"model", control.ActionModelCommand, "/model example"}, {"reasoning", control.ActionReasoningCommand, "/reasoning high"}} {
			for _, card := range []bool{false, true} {
				t.Run(string(backend)+"/"+tc.name+map[bool]string{false: "/slash", true: "/card"}[card], func(t *testing.T) {
					ctx := context.Background()
					cfg := config.DefaultAppConfig()
					cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "secret"}}
					host, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
					setTestWorkBotSettings(t, host, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: backend})
					app := privatechat.New(nil, orchestrator.Config{})
					host.service = app.Service()
					if err := host.BindPrivateServiceOwnership(app); err != nil {
						t.Fatal(err)
					}
					j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "journal"))
					if err != nil {
						t.Fatal(err)
					}
					defer j.Close()
					p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
					if err != nil {
						t.Fatal(err)
					}
					o := privatechat.Owner{GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
					if err = host.refreshPrivateBotSettings(ctx, p, o, 1); err != nil {
						t.Fatal(err)
					}
					a := control.Action{Kind: tc.kind, Text: tc.text, GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
					if card {
						a.Inbound = &control.ActionInboundMeta{CardCallback: true, CardDaemonLifecycleID: host.daemonLifecycleID}
					}
					raw, _ := json.Marshal(a)
					seq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "setting", Lane: "control", Payload: raw})
					if err != nil {
						t.Fatal(err)
					}
					effects, err := p.ApplyIngress(ctx, o, 1, seq)
					if err != nil || len(effects) == 0 {
						t.Fatalf("setting action did not complete: effects=%v err=%v", effects, err)
					}
					for _, effect := range effects {
						event, err := privatechat.DecodeEffectEvent(effect.Effect.Body)
						if err != nil {
							t.Fatal(err)
						}
						if event.Notice != nil && (event.Notice.Code == "old_card_expired" || event.Notice.Code == "old_inbound_ignored") {
							t.Fatalf("setting was rejected as stale: %#v", event.Notice)
						}
						if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateBotSettings {
							if _, err := runPrivateBotSettings(ctx, host, p, j, o, 1, effect.Effect.ID); err != nil {
								t.Fatalf("settings receipt: %v", err)
							}
						}
					}
					a = control.Action{Kind: control.ActionStatus, Text: "/status", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID}
					raw, _ = json.Marshal(a)
					seq, _, err = j.Admit(ctx, privatechat.Ingress{Owner: o, Generation: 1, EventID: "status", Lane: "control", Payload: raw})
					if err != nil {
						t.Fatal(err)
					}
					if _, err = p.ApplyIngress(ctx, o, 1, seq); err != nil {
						t.Fatalf("later status blocked: %v", err)
					}
				})
			}
		}
	}
}
