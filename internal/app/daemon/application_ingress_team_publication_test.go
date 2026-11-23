package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestTeamConfirmedPublicationBindsRealReceiptAndRecovers(t *testing.T) {
	ctx := t.Context()
	f := newResourceRecoveryFixture(t)
	store := f.a.teamRuntime.store
	binder := &teamPublicationBinding{store: store}
	router, err := ingressowner.Open(filepath.Join(t.TempDir(), "router.db"), binder, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	binder.router = router
	var binding workstore.UIView
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		role, e := tx.GetRoleBotBinding("leader")
		if e != nil {
			return e
		}
		binding = workstore.UIView{ID: "group-view", ScopeID: "scope", SurfaceKey: "menu", Kind: "project.navigation", TargetID: "project", TargetRevision: 1, Binding: role, Tenant: "tenant", ChatID: "chat", ChannelGeneration: 1}
		card, e := workui.Card(binding.ID, workui.Panel{Title: "team"})
		if e != nil {
			return e
		}
		_, e = workui.Queue(tx, binding, card)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	event := ingressowner.Event{Key: ingressowner.Key{Tenant: "tenant", AppID: binding.Binding.AppID, EventID: "group-message"}, Source: ingressowner.Source{GatewayID: binding.Binding.GatewayRef, BindingGeneration: binding.Binding.IdentityGeneration, ReceivedAt: time.Now().UTC()}, Kind: ingressowner.Message, ChatType: "group", ChatID: "chat", MessageID: "source", Operator: ingressowner.Operator{IDType: "open_id", ID: "user"}, Payload: json.RawMessage(`{"sdk":"fact"}`)}
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		gateway := work.IngressGateway{GatewayRef: binding.Binding.GatewayRef, AppID: binding.Binding.AppID, RoleID: binding.Binding.RoleID, BotGeneration: binding.Binding.IdentityGeneration}
		raw := work.RawIngressEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: "group-message", Tenant: "tenant", ChatID: "chat", ChatType: "group", MessageID: "source", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: time.Now().UTC()}
		_, _, e := tx.AcceptRawIngress(raw, gateway)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = PrepareHistoricalTeamOwners(ctx, router, store); err != nil {
		t.Fatal(err)
	}
	forgedWitness := ingressowner.GroupOwnerWitness{SourceEvent: event.Key, SourceFactHash: "forged", GatewayID: event.Source.GatewayID, BindingGeneration: event.Source.BindingGeneration, ChatID: event.ChatID}
	forgedProof, _ := json.Marshal(teamGroupWitnessProof{IngressID: "forged", Revision: 1, FactHash: "forged"})
	if _, err = router.ImportHistoricalGroupOwner(ctx, forgedWitness, forgedProof); err == nil {
		t.Fatal("forged group witness accepted")
	}
	pub, err := workpublish.New(store, recoveryUISender{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the actual publication committed and before the
	// neutral hook; the reconciliation must recover the exact persisted receipt.
	if _, err = pub.RunOnce(ctx, 32); err != nil {
		t.Fatal(err)
	}
	if err = binder.ReconcileConfirmed(ctx); err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	if err = store.Within(ctx, func(tx *workstore.Tx) error { var e error; view, e = tx.GetUIView(binding.ID); return e }); err != nil {
		t.Fatal(err)
	}
	actual, err := router.PublishedMessage(ctx, "tenant", binding.Binding.AppID, "chat", view.ExternalMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.CardKind != "team_view" || actual.CardRef != binding.ID || actual.Owner.Application != ingressowner.Team {
		t.Fatalf("wrong owner: %+v", actual)
	}
	click := ingressowner.Event{Key: ingressowner.Key{Tenant: "tenant", AppID: binding.Binding.AppID, EventID: "click"}, Source: event.Source, Kind: ingressowner.Card, ChatID: "chat", MessageID: view.ExternalMessageID, CardKind: "team_view", CardRef: binding.ID, Operator: event.Operator, Payload: json.RawMessage(`{"sdk":"callback"}`)}
	routed, _, err := router.Admit(ctx, click, applicationOwnerResolver{router: router})
	if err != nil || routed.Owner == nil || *routed.Owner != actual.Owner {
		t.Fatalf("confirmed card did not route to team: %+v %v", routed, err)
	}
	proof, _ := json.Marshal(teamPublicationProof{Kind: "team_view", ID: binding.ID})
	actual.MessageID = "forged"
	if err = binder.VerifyPlatformReceipt(ctx, actual, proof); err == nil {
		t.Fatal("forged platform receipt accepted")
	}
}
