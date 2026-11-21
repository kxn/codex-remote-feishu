package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"strings"
	"testing"
	"time"
)

func TestBlockedPrivateReplyPresenterContinuesAndTerminatesChangedBotWithoutSending(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "continues", true: "changed_identity"}[changed], func(t *testing.T) {
			a, source := teamPickerFixture(t)
			ingress, e := workingress.New(a.teamRuntime.store, workingress.Config{Directory: steerSDKDirectory{source.Gateway}})
			if e != nil {
				t.Fatal(e)
			}
			receipt, e := ingress.Receive(t.Context(), work.RawIngressEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "unresolved-private-reply", Tenant: source.Tenant, ChatID: source.ChatID, ChatType: "p2p", MessageID: "unresolved-private-reply", ParentID: "unknown-parent", SenderType: "user", SenderIDs: source.SenderIDs, MessageType: "text", RawContent: `{"text":"continue"}`, ReceivedAt: time.Now().UTC()})
			if e != nil {
				t.Fatal(e)
			}
			if changed {
				pickerTx(t, a, func(tx *workstore.Tx) error {
					binding, e := tx.GetRoleBotBinding(source.RoleID)
					if e != nil {
						return e
					}
					binding.IdentityGeneration++
					return tx.PutRoleBotBinding(binding, 1)
				})
			}
			e = a.presentBlockedPrivateReplies(t.Context(), []string{"missing-first-item", receipt.IngressID})
			if e == nil {
				t.Fatal("lost first invalid item error")
			}
			pickerTx(t, a, func(tx *workstore.Tx) error {
				raw, e := tx.GetIngress(receipt.IngressID)
				if e != nil {
					return e
				}
				if raw.State != work.IngressIgnored || raw.MessageID != "" {
					t.Fatal("later reply was starved or sent into the model")
				}
				_, e = tx.GetUIView(workui.ID("private-reply-routing", raw.ID))
				if changed {
					if e != workstore.ErrNotFound || !strings.Contains(raw.LastError, "未发送") {
						t.Fatal("changed Bot fabricated notice or remained endlessly pending")
					}
					return nil
				}
				if e != nil {
					return e
				}
				return nil
			})
		})
	}
}
