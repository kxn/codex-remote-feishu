package workpublish

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type cardTestSender struct {
	fakePublisher
	card json.RawMessage
}

func (s *cardTestSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) { return s, nil }
func (s *cardTestSender) PublishCard(ctx context.Context, r feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error) {
	s.card = r.Card
	return s.PublishText(ctx, feishu.ProjectPublishRequest{ChatID: r.ChatID, UUID: r.UUID, ReplyToMessageID: r.ReplyToMessageID})
}

func TestUIViewDeliveryUsesSameDurablePublicationAndRejectsForwardedOrOldCards(t *testing.T) {
	store, _, _, _ := publisherFixture(t)
	sender := &cardTestSender{}
	p, err := New(store, sender, nil)
	if err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	var interaction workstore.HumanInteraction
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
		var e error
		interaction, _, e = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "e1", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "person-open"}, ChatID: "oc_chat", MessageID: "om_receipt", Kind: "card", ReceivedAt: time.Now().UTC()}, gateway)
		if e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "ui-personal", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: interaction.PrincipalID, Revision: 1}, 0); e != nil {
			return e
		}
		view = workstore.UIView{ID: "view1", ScopeID: "ui-personal", SurfaceKey: "personal-menu", Kind: "private.navigation", TargetID: "nav", TargetRevision: 1, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", PrincipalID: interaction.PrincipalID}
		card, e := workui.Card(view.ID, workui.Panel{Title: "当前工作", Paragraphs: []string{"<b>literal text</b>"}, Buttons: []workui.Button{{Label: "新建工作", Action: "private.new"}}})
		if e != nil {
			return e
		}
		view, e = workui.Queue(tx, view, card)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || len(sender.card) == 0 {
		t.Fatal("card did not pass actual publication adapter")
	}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		current, e := tx.GetUIView(view.ID)
		if e != nil {
			return e
		}
		if current.ExternalMessageID != "om_receipt" {
			t.Fatal("card receipt missing")
		}
		if e = tx.ValidateViewInteraction(current, interaction); e != nil {
			return e
		}
		forwarded := interaction
		forwarded.Envelope.ChatID = "another-chat"
		if e = tx.ValidateViewInteraction(current, forwarded); e == nil {
			t.Fatal("forwarded card accepted")
		}
		replacement := view
		replacement.ID = "view2"
		replacement.TargetRevision = 2
		card, e := workui.Card(replacement.ID, workui.Panel{Title: "更新后的工作"})
		if e != nil {
			return e
		}
		if _, e = workui.Queue(tx, replacement, card); e != nil {
			return e
		}
		old, e := tx.GetUIView(view.ID)
		if e != nil {
			return e
		}
		if e = tx.ValidateViewInteraction(old, interaction); e == nil {
			t.Fatal("old card retained authority")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInteractionReplayCannotChangeActorOrAction(t *testing.T) {
	store, _, _, _ := publisherFixture(t)
	raw := work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "sdk-event", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "real"}, ChatID: "oc_chat", MessageID: "om_message", Kind: "card", Values: map[string]any{"action": "native.respond", "option_id": "decline"}, ReceivedAt: time.Now().UTC()}
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
	err := store.Within(context.Background(), func(tx *workstore.Tx) error { _, _, e := tx.AcceptHumanInteraction(raw, gateway); return e })
	if err != nil {
		t.Fatal(err)
	}
	raw.ReceivedAt = raw.ReceivedAt.Add(time.Second)
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		_, replay, e := tx.AcceptHumanInteraction(raw, gateway)
		if !replay {
			t.Fatal("legitimate replay duplicated")
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	raw.Values["option_id"] = "accept"
	err = store.Within(context.Background(), func(tx *workstore.Tx) error { _, _, e := tx.AcceptHumanInteraction(raw, gateway); return e })
	if err == nil {
		t.Fatal("same SDK identity changed approval")
	}
}

func TestSupersededQueuedViewIsNeverPublished(t *testing.T) {
	store, _, _, _ := publisherFixture(t)
	sender := &cardTestSender{}
	publisher, err := New(store, sender, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		for _, id := range []string{"old-view", "current-view"} {
			view := workstore.UIView{ID: id, ScopeID: "scope", SurfaceKey: "same-menu", Kind: "project.status", TargetID: "project", TargetRevision: 1, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", ChannelGeneration: 1}
			card, e := workui.Card(id, workui.Panel{Title: id})
			if e != nil {
				return e
			}
			if _, e = workui.Queue(tx, view, card); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.RunOnce(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 {
		t.Fatalf("published %d views", sender.calls)
	}
	if string(sender.card) == "" {
		t.Fatal("missing current view")
	}
	err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		old, e := tx.GetOutbox("old-view:outbox")
		if e != nil {
			return e
		}
		if old.State != work.OutboxTerminal {
			t.Fatal(old.State)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
