package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestHumanInteractionHeadsPreserveArrivalAndExposeOtherChats(t *testing.T) {
	store, _ := openTestStore(t)
	seedPersonal(t, store)
	var first, second string
	requireTx(t, store, func(tx *Tx) error {
		if e := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: "r", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); e != nil {
			return e
		}
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "r", BotGeneration: 1}
		for i := 0; i < 40; i++ {
			chat := "busy-chat"
			if i == 39 {
				chat = "other-chat"
			}
			v, _, e := tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: fmt.Sprintf("event-%d", i), Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "person"}, ChatID: chat, MessageID: "message", Kind: "card", ReceivedAt: time.Now().UTC()}, gateway)
			if e != nil {
				return e
			}
			if i == 0 {
				first = v.ID
			}
			if i == 1 {
				second = v.ID
			}
		}
		heads, e := tx.ListPendingHumanInteractionHeads(2)
		if e != nil {
			return e
		}
		if len(heads) != 2 || heads[0].ID != first || heads[1].Envelope.ChatID != "other-chat" {
			t.Fatal("batch hid another chat or reordered arrival")
		}
		if e = tx.FinishHumanInteraction(first, 1, "recorded"); e != nil {
			return e
		}
		heads, e = tx.ListPendingHumanInteractionHeads(2)
		if e != nil {
			return e
		}
		if heads[0].ID != second {
			t.Fatal("second actual arrival lost FIFO")
		}
		return nil
	})
}
