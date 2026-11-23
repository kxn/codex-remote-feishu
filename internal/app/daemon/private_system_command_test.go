package daemon

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

func TestPrivateSystemMenuUsesOriginalBackendAndUpdatesCurrentCard(t *testing.T) {
	originalDetect, originalApply := detectAutostart, applyAutostart
	defer func() { detectAutostart, applyAutostart = originalDetect, originalApply }()
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			calls := 0
			detectAutostart = func(string) (install.AutostartStatus, error) {
				return install.AutostartStatus{Supported: true, CanApply: true, Status: "disabled"}, nil
			}
			applyAutostart = func(install.AutostartApplyOptions) (install.AutostartStatus, error) {
				calls++
				if failure {
					return install.AutostartStatus{}, fmt.Errorf("controlled service-manager error")
				}
				return install.AutostartStatus{Supported: true, Configured: true, Enabled: true, Status: "enabled"}, nil
			}
			gateway := &privatePublishGateway{}
			host := New(":0", ":0", gateway, agentproto.ServerIdentity{})
			app := privatechat.New(nil, orchestrator.Config{})
			host.service = app.Service()
			j, err := privatechat.OpenJournal(filepath.Join(t.TempDir(), "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
			if err != nil {
				t.Fatal(err)
			}
			o := privatechat.Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
			a := control.Action{Kind: control.ActionAdminCommand, Text: "/admin autostart on", GatewayID: o.GatewayID, SurfaceSessionID: o.SurfaceID, ChatID: o.ChatID, ActorUserID: o.UserID, MessageID: "menu-card", LocalPageAction: true, Inbound: &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "menu-card", CardDaemonLifecycleID: host.daemonLifecycleID}}
			raw, _ := json.Marshal(a)
			seq, _, err := j.Admit(t.Context(), privatechat.Ingress{Owner: o, Generation: 1, EventID: "callback", Lane: "control", Payload: raw})
			if err != nil {
				t.Fatal(err)
			}
			effects, err := p.ApplyIngress(t.Context(), o, 1, seq)
			if err != nil || len(effects) != 1 {
				t.Fatalf("admin input: %#v %v", effects, err)
			}
			if calls != 0 {
				t.Fatal("setting changed before durable effect")
			}
			out, err := runPrivateSystemCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID)
			if err != nil || len(out) != 1 {
				t.Fatalf("admin result: %#v %v", out, err)
			}
			publisher := &privatePublisher{host: host, processor: p, journal: j}
			if _, err = publisher.Publish(t.Context(), o, 1, out[0].Effect.ID); err != nil {
				t.Fatal(err)
			}
			if len(gateway.operations) != 1 || gateway.operations[0].Kind != feishu.OperationUpdateCard || gateway.operations[0].MessageID != "menu-card" {
				t.Fatalf("did not replace original card: %#v", gateway.operations)
			}
			encoded, _ := feishu.FreezePrivateOperation(gateway.operations[0], "test")
			if failure && !strings.Contains(string(encoded), "controlled service-manager error") {
				t.Fatal("actionable failure detail lost")
			}
			if _, err = runPrivateSystemCommand(t.Context(), host, p, j, o, 1, effects[0].Effect.ID); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("confirmed system setting executed again")
			}
		})
	}
}
