package workingress

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestHistoricalPrivateMenuCannotRetargetOrdinaryInput(t *testing.T) {
	ingress, store, directory := ingressFixture(t, false)
	source, original := privateCommandSource(t, ingress, store, "navigation-source")
	gateway := directory.gateways["gateway:leader"]
	acceptMenu := func(event string) workstore.MenuInteraction {
		t.Helper()
		var menu workstore.MenuInteraction
		txRun(t, store, func(tx *workstore.Tx) error {
			var err error
			menu, _, err = tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: event, Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "open-person", UserID: "person"}, EventKey: "menu", ReceivedAt: ingressNow}, gateway)
			if err != nil {
				return err
			}
			sending, _, err := tx.BeginMenuDestination(menu.ID, menu.Revision, "Private menu destination")
			if err != nil {
				return err
			}
			return tx.FinishMenuDestination(menu.ID, sending.Revision, "dm", "historical-menu-entry:"+event, "", true)
		})
		return menu
	}
	historical := acceptMenu("old-menu")
	before := receive(t, ingress, rawMessage("leader", "before-new", "dm", "p2p", "open-person", "person", "finish this existing work"))
	selected, err := privateTestService(t, store).TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "new", Source: source, Action: "new", ExpectedRevision: original.Revision})
	if err != nil {
		t.Fatal(err)
	}
	currentMenu := acceptMenu("current-menu")
	if historical.ContextID != original.FrontContextID || currentMenu.ContextID != selected.Navigation.FrontContextID || historical.ContextID == currentMenu.ContextID {
		t.Fatalf("fixture needs two distinct menu contexts: old=%+v current=%+v", historical, currentMenu)
	}
	after := receive(t, ingress, rawMessage("leader", "after-new", "dm", "p2p", "open-person", "person", "start the new work"))
	result := process(t, ingress)
	if result.Processed != 2 || result.Commands != 0 {
		t.Fatalf("menus became model inputs or ordinary input was lost: %+v", result)
	}
	txRun(t, store, func(tx *workstore.Tx) error {
		for _, test := range []struct{ ingressID, contextID string }{{before.IngressID, original.FrontContextID}, {after.IngressID, selected.Navigation.FrontContextID}} {
			raw, err := tx.GetIngress(test.ingressID)
			if err != nil {
				return err
			}
			if raw.Route.ContextID != test.contextID || raw.Route.ReplyRoutingError != "" || raw.Envelope.ParentID != "" || raw.Envelope.RootID != "" || raw.Envelope.ThreadID != "" {
				t.Fatalf("historical menu altered ordinary receive-time ownership: %+v", raw)
			}
			inputs, err := tx.ListInbox(original.PersonalScopeID)
			if err != nil {
				return err
			}
			if len(inputs) != 2 {
				t.Fatalf("read-only menus created executable inputs: %+v", inputs)
			}
			found := false
			for _, input := range inputs {
				if input.MessageID == raw.MessageID {
					found = true
					if input.RecipientContextID != test.contextID {
						t.Fatalf("input ownership drifted: %+v", input)
					}
				}
			}
			if !found {
				t.Fatalf("no durable input for %s", test.ingressID)
			}
		}
		navigation, err := tx.GetPrivateNavigation(original.PersonalScopeID)
		if err != nil {
			return err
		}
		if navigation != selected.Navigation {
			t.Fatal("menu read changed the selected work")
		}
		return nil
	})
}
