package workstore

import (
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func seedMenuRole(t *testing.T, s *Store, role, app string) work.IngressGateway {
	t.Helper()
	gateway := work.IngressGateway{GatewayRef: "gateway:" + role, AppID: app, RoleID: role, BotGeneration: 1}
	requireTx(t, s, func(tx *Tx) error {
		if e := tx.PutRole(work.Role{ID: role, Name: role, Description: "responsibility", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: role, GatewayRef: gateway.GatewayRef, AppID: app, IdentityGeneration: 1}, 0)
	})
	return gateway
}
func menuEnvelope(gateway work.IngressGateway, event, openid string) work.MenuInteractionEnvelope {
	return work.MenuInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: event, Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: openid}, EventKey: "menu", ClickedAt: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), ReceivedAt: time.Date(2026, 11, 21, 12, 0, 1, 0, time.UTC)}
}
func TestMenuFirstClickCreatesRealIdentityAndNavigationWithoutHumanMessage(t *testing.T) {
	s, path := openTestStore(t)
	gateway := seedMenuRole(t, s, "leader", "app")
	var menu MenuInteraction
	requireTx(t, s, func(tx *Tx) error {
		var e error
		menu, _, e = tx.AcceptMenuInteraction(menuEnvelope(gateway, "event", "person"), gateway)
		if e != nil {
			return e
		}
		nav, e := tx.GetPrivateNavigation(menu.ScopeID)
		if e != nil {
			return e
		}
		if menu.ContextID != nav.FrontContextID || menu.NavigationRevision != nav.Revision {
			t.Fatal("menu navigation not frozen")
		}
		var count int
		if e = tx.tx.QueryRowContext(t.Context(), "SELECT count(*) FROM messages").Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			t.Fatal("menu forged human message")
		}
		if e = tx.tx.QueryRowContext(t.Context(), "SELECT count(*) FROM raw_ingress").Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			t.Fatal("menu forged SDK message ingress")
		}
		_, _, e = tx.ValidatePrivateAction(PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID})
		return e
	})
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		canonical, replay, e := tx.AcceptMenuInteraction(menuEnvelope(gateway, "event", "person"), gateway)
		if e != nil {
			return e
		}
		if !replay || canonical.ID != menu.ID || canonical.PrincipalID != menu.PrincipalID {
			t.Fatal("menu replay lost persisted identity")
		}
		return nil
	})
}
func TestMenuOpenIDsDoNotMergeAcrossAppsAndUnknownSendIsNeverRepeated(t *testing.T) {
	s, _ := openTestStore(t)
	firstGateway := seedMenuRole(t, s, "first", "first-app")
	secondGateway := seedMenuRole(t, s, "second", "second-app")
	requireTx(t, s, func(tx *Tx) error {
		first, _, e := tx.AcceptMenuInteraction(menuEnvelope(firstGateway, "first", "same-open-id"), firstGateway)
		if e != nil {
			return e
		}
		second, _, e := tx.AcceptMenuInteraction(menuEnvelope(secondGateway, "second", "same-open-id"), secondGateway)
		if e != nil {
			return e
		}
		if first.PrincipalID == second.PrincipalID || first.ScopeID == second.ScopeID {
			t.Fatal("app-qualified menu identities merged")
		}
		sending, send, e := tx.BeginMenuDestination(first.ID, first.Revision, "Open actual private entry")
		if e != nil {
			return e
		}
		if !send || sending.UUID == "" || sending.DestinationTextRef == "" {
			t.Fatal("missing durable send intent")
		}
		if e = tx.FinishMenuDestination(first.ID, sending.Revision, "", "", "connection lost", false); e != nil {
			return e
		}
		unknown, e := tx.GetMenuInteraction(first.ID)
		if e != nil {
			return e
		}
		_, send, e = tx.BeginMenuDestination(first.ID, unknown.Revision, "Open actual private entry")
		if e != nil {
			return e
		}
		if send {
			t.Fatal("unknown first private send repeated")
		}
		if e = tx.FinishMenuInteraction(first.ID, unknown.Revision, "fake-view"); e == nil {
			t.Fatal("menu completed without actual private chat receipt")
		}
		return nil
	})
}
