package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func TestPrivateReplyUnknownAnchorQueuesNoticeWithoutModelInput(t *testing.T) {
	a, source := teamPickerFixture(t)
	var ingressID string
	pickerTx(t, a, func(tx *workstore.Tx) error {
		raw, _, err := tx.AcceptRawIngress(work.RawIngressEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "unknown-reply", Tenant: source.Tenant, ChatID: source.ChatID, ChatType: "p2p", MessageID: "reply-message", ParentID: "unknown-original", SenderType: "user", SenderIDs: source.SenderIDs, MessageType: "text", RawContent: `{"text":"approve it"}`, ReceivedAt: time.Now().UTC()}, source.Gateway)
		if err != nil {
			return err
		}
		ingressID = raw.ID
		nav, err := tx.EnsurePrivateNavigation(source.ScopeID, raw.ID, time.Now().UTC())
		if err != nil {
			return err
		}
		return tx.FreezePrivateIngressNavigation(raw.ID, nav)
	})
	for n := 0; n < 2; n++ {
		if err := a.presentBlockedPrivateReplies(t.Context(), []string{ingressID}); err != nil {
			t.Fatal(err)
		}
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		raw, err := tx.GetIngress(ingressID)
		if err != nil {
			return err
		}
		if raw.State != work.IngressIgnored || raw.Route.ReplyRoutingError == "" || raw.Envelope.RawContent == "" {
			t.Fatal("lost original/rejection state")
		}
		views, err := tx.ListUIViews(source.ScopeID)
		if err != nil {
			return err
		}
		if len(views) != 1 || views[0].Kind != "private.reply.notice" || views[0].ContextID != "" {
			t.Fatalf("wrong recovery projection: %+v", views)
		}
		outbox, err := tx.ListOutbox(source.ScopeID)
		if err != nil {
			return err
		}
		if len(outbox) != 1 || outbox[0].State != work.OutboxPending {
			t.Fatal("notice bypassed durable publisher or duplicated")
		}
		inputs, err := tx.ListInbox(source.ScopeID)
		if err != nil {
			return err
		}
		if len(inputs) != 0 {
			t.Fatal("unproven reply admitted as model instruction")
		}
		return nil
	})
}
