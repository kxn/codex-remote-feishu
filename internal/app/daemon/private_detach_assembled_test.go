package daemon

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func TestPrivateDetachedUnknownSDKSlashAndCard(t *testing.T) {
	for _, viaCard := range []bool{false, true} {
		t.Run(map[bool]string{false: "slash", true: "card"}[viaCard], func(t *testing.T) {
			app, gateway := newApplicationRuntimeTestApp(t)
			r := app.applications
			ctx := t.Context()
			now := time.Now().UTC()
			raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "witness", MessageID: "witness-message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: now}
			event, e := ingressmapper.Message(raw, 1)
			if e != nil {
				t.Fatal(e)
			}
			record, _, e := r.router.Admit(ctx, event, applicationOwnerResolver{router: r.router})
			if e != nil || record.Owner == nil {
				t.Fatalf("owner: %v", e)
			}
			o := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
			seed := privatechat.New(nil, orchestrator.Config{})
			if e = seed.RegisterOwner(o); e != nil {
				t.Fatal(e)
			}
			cp, e := seed.Service().ExportPrivateCheckpoint(o.SurfaceID)
			if e != nil {
				t.Fatal(e)
			}
			cp.AttachedInstanceID = "old-instance"
			cp.Holds = []orchestrator.PrivateRecoveryHold{{Kind: "pending_remote", InstanceID: "old-instance", Frozen: json.RawMessage(`{}`)}}
			if e = r.journal.CommitCheckpoint(ctx, privatechat.Checkpoint{Owner: o, Generation: record.Owner.Epoch, Revision: 1, Core: cp}, 0, nil); e != nil {
				t.Fatal(e)
			}
			if !viaCard {
				raw.EventID = "detach"
				raw.MessageID = "detach"
				raw.RawContent = `{"text":"/detach"}`
				if e = r.hook.Receive(ctx, raw); e != nil {
					t.Fatal(e)
				}
			} else {
				raw.EventID = "menu"
				raw.MessageID = "menu"
				raw.RawContent = `{"text":"/menu"}`
				if e = r.hook.Receive(ctx, raw); e != nil {
					t.Fatal(e)
				}
				errors := make(chan error, 4)
				if e = r.startWorkers(ctx, errors); e != nil {
					t.Fatal(e)
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
					t.Fatal("menu card missing")
				}
				for deadline := time.Now().Add(5 * time.Second); ; {
					if _, e = r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); e == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal(e)
					}
					time.Sleep(10 * time.Millisecond)
				}
				body, e := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
				if e != nil {
					t.Fatal(e)
				}
				var doc any
				if e = json.Unmarshal(body, &doc); e != nil {
					t.Fatal(e)
				}
				value := assembledActionKind(doc, "surface.command.workspace")
				if value == nil {
					t.Fatal("workspace menu action missing")
				}
				click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "open-current-work", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
				page, e := r.hook.ReceiveInteractionResponse(ctx, click)
				if e != nil {
					t.Fatal(e)
				}
				if e = json.Unmarshal(page.RawBody, &doc); e != nil {
					t.Fatal(e)
				}
				value = assembledActionKind(doc, "surface.command.workspace.detach")
				if value == nil {
					t.Fatalf("detach action missing from workspace menu: %s", page.RawBody)
				}
				click.EventID = "click-detach"
				click.Values = value
				response, e := r.hook.ReceiveInteractionResponse(ctx, click)
				if e != nil {
					t.Fatal(e)
				}
				if len(response.RawBody) == 0 {
					t.Fatal("same-card response missing")
				}
			}
			if inputs, e := r.journal.PendingIngressWorkPage(ctx, 128, 0); e == nil {
				for _, in := range inputs {
					if _, e = r.processor.ApplyIngress(ctx, in.Owner, in.Generation, in.Sequence); e != nil {
						t.Logf("pending ingress error=%v", e)
					}
				}
			}
			current, e := r.journal.LoadCheckpoint(ctx, o, record.Owner.Epoch)
			if e != nil {
				t.Fatal(e)
			}
			if current.Core.Version != 2 || len(current.Core.DetachedExecutions) != 1 || current.Core.DetachedExecutions[0].InstanceID != "old-instance" || current.Core.AttachedInstanceID != "" {
				t.Fatalf("detached checkpoint mismatch: version=%d instances=%d", current.Core.Version, len(current.Core.DetachedExecutions))
			}
			_ = fmt.Sprint(agentproto.BackendCodex)
		})
	}
}
