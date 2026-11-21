package workpublish

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type cardObservationSender struct {
	cardTestSender
	reads  int
	actual feishu.ObservedPublication
}

func (s *cardObservationSender) Resolve(context.Context, work.RoleBotBinding) (Sender, error) {
	return s, nil
}
func (s *cardObservationSender) ReadPublishedMessage(context.Context, string) (feishu.ObservedPublication, error) {
	s.reads++
	return s.actual, nil
}
func unknownCardFixture(t *testing.T) (*workstore.Store, *Publisher, *cardObservationSender, workstore.UIView, workstore.HumanInteraction) {
	t.Helper()
	store, _, _, _ := publisherFixture(t)
	sender := &cardObservationSender{cardTestSender: cardTestSender{fakePublisher: fakePublisher{failFirst: true}}, actual: feishu.ObservedPublication{MessageID: "om_receipt", ChatID: "oc_chat", MessageType: "interactive", SenderType: "app", SenderIDType: "app_id", SenderID: "app"}}
	p, err := New(store, sender, nil)
	if err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	var interaction workstore.HumanInteraction
	err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		var err error
		interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "actual-sdk-card", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "actual-person"}, ChatID: "oc_chat", MessageID: "om_receipt", Kind: "card", Values: map[string]any{"team_view": "original-card", "action": "private.new"}, ReceivedAt: time.Now()}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		if err != nil {
			return err
		}
		if err = tx.PutScope(work.WorkScope{ID: "ui-personal", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: interaction.PrincipalID, Revision: 1}, 0); err != nil {
			return err
		}
		view = workstore.UIView{ID: "original-card", ScopeID: "ui-personal", SurfaceKey: "original-menu", Kind: "private.navigation", TargetID: "nav", TargetRevision: 1, Binding: work.RoleBotBinding{RoleID: "role", GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, Tenant: "tenant", ChatID: "oc_chat", PrincipalID: interaction.PrincipalID}
		card, err := workui.Card(view.ID, workui.Panel{Title: "原始菜单", Buttons: []workui.Button{{Label: "新建工作", Action: "private.new"}}})
		if err != nil {
			return err
		}
		view, err = workui.Queue(tx, view, card)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.RunOnce(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	if err = store.Within(t.Context(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox(view.ID + ":outbox")
		if err == nil && item.State != work.OutboxUnknown {
			t.Fatalf("expected unknown actual send, got %s", item.State)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return store, p, sender, view, interaction
}
func TestUnknownCardObservationUsesOriginalSDKCallbackAndDoesNotResend(t *testing.T) {
	store, p, sender, view, interaction := unknownCardFixture(t)
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(view, interaction); err == nil {
			t.Fatal("unknown delivery authorized a business action before exact receipt observation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.ObserveUnknownCardInteraction(t.Context(), view.ID+":outbox", interaction.ID); err != nil {
		t.Fatal(err)
	}
	if sender.calls != 1 || sender.effects != 1 || sender.reads != 1 {
		t.Fatalf("writes were repeated %+v", sender)
	}
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		item, err := tx.GetOutbox(view.ID + ":outbox")
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(view.OperationID)
		if err != nil {
			return err
		}
		if item.State != work.OutboxSent || item.ExternalMessageID != "om_receipt" || op.State != work.OperationConfirmed {
			t.Fatal("actual receipt not atomically recorded")
		}
		mapping, err := tx.GetPlatformMessageMapping("tenant", "oc_chat", "om_receipt")
		if err != nil {
			return err
		}
		if mapping.OperationID != op.ID || mapping.ScopeID != view.ScopeID {
			t.Fatal("receipt escaped original scope")
		}
		confirmed, err := tx.GetUIView(view.ID)
		if err != nil {
			return err
		}
		return tx.ValidateViewInteraction(confirmed, interaction)
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.ObserveUnknownCardInteraction(t.Context(), view.ID+":outbox", interaction.ID); err != nil {
		t.Fatal(err)
	}
	if sender.reads != 1 || sender.calls != 1 {
		t.Fatal("recorded receipt was re-read or re-sent")
	}
}
func TestUnknownCardObservationRejectsOtherPayloadOrTopic(t *testing.T) {
	store, p, sender, view, _ := unknownCardFixture(t)
	var wrong workstore.HumanInteraction
	if err := store.Within(t.Context(), func(tx *workstore.Tx) error {
		var err error
		wrong, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "forged-extra-field", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "actual-person"}, ChatID: "oc_chat", MessageID: "om_receipt", Kind: "card", Values: map[string]any{"team_view": view.ID, "action": "private.new", "extra": "not-in-original-card"}, ReceivedAt: time.Now()}, work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.ObserveUnknownCardInteraction(t.Context(), view.ID+":outbox", wrong.ID); err == nil {
		t.Fatal("new payload falsely proved old card")
	}
	if sender.reads != 0 {
		t.Fatal("invalid SDK payload reached platform read")
	}
	_, p, sender, view, actual := unknownCardFixture(t)
	sender.actual.ParentID = "unrelated-root"
	if err := p.ObserveUnknownCardInteraction(t.Context(), view.ID+":outbox", actual.ID); err == nil {
		t.Fatal("same chat/card in unrelated topic adopted")
	}
}
