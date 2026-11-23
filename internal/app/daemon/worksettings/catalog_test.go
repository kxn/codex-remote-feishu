package worksettings

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestSettingsCatalogIgnoresAmbiguousRecoveringWorkersButKeepsNavigationAndAuthority(t *testing.T) {
	base, fake, _ := settingsFixture(t)
	port := &scopedCatalogPort{settingsFakePort: fake}
	gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: "role", BotGeneration: 1}
	var menu workstore.MenuInteraction
	sourceFor := func(event string) workstore.PrivateActionSource {
		t.Helper()
		if e := base.store.Within(t.Context(), func(tx *workstore.Tx) error {
			accepted, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: gateway.GatewayRef, AppID: gateway.AppID, EventID: event, Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "catalog-human"}, EventKey: "menu", ReceivedAt: time.Now()}, gateway)
			if e != nil {
				return e
			}
			sending, _, e := tx.BeginMenuDestination(accepted.ID, accepted.Revision, "catalog settings")
			if e != nil {
				return e
			}
			if e = tx.FinishMenuDestination(accepted.ID, sending.Revision, "private-chat", "menu-card-"+event, "", true); e != nil {
				return e
			}
			menu, e = tx.GetMenuInteraction(accepted.ID)
			return e
		}); e != nil {
			t.Fatal(e)
		}
		return workstore.PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}
	}
	source := sourceFor("first-menu")
	principal := menu.PrincipalID
	service, e := New(base.store, port, func(_ context.Context, p string, b work.RoleBotBinding, ref work.SourceRef) error {
		if p != principal || b != fake.binding || ref.ID == "" {
			return workstore.ErrScopeDenied
		}
		return nil
	}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = base.store.Within(t.Context(), func(tx *workstore.Tx) error {
		for _, id := range []string{"recovering-one", "recovering-two"} {
			if e := tx.PutWorker(work.WorkerSession{ID: id, ContextID: menu.ContextID, ScopeID: menu.ScopeID, RoleID: "role", Backend: work.BackendCodex, State: work.WorkerRecovering, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: id, InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}, 0); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	snapshot, e := service.DiscoverPage(t.Context(), principal, "role", source, "page-2")
	if e != nil || snapshot.Catalog == nil || snapshot.Catalog.Entries[0].Model != "second-model" {
		t.Fatalf("business worker ambiguity blocked independent catalog: %+v %v", snapshot, e)
	}
	if len(port.owners) != 1 || port.owners[0].ContextID != menu.ContextID || port.owners[0].Cursor != "page-2" {
		t.Fatal("catalog lost frozen context/page")
	}
	if _, e = service.DiscoverPage(t.Context(), "another-human", "role", source, ""); e == nil {
		t.Fatal("foreign principal read model catalog")
	}
	if len(port.owners) != 1 {
		t.Fatal("unauthorized source reached directory reader")
	}
	if e = base.store.Within(t.Context(), func(tx *workstore.Tx) error {
		next := work.WorkContext{ID: "new-front", ScopeID: menu.ScopeID, RoleID: "role", Workstream: "new-personal", Revision: 1}
		if e := tx.PutContext(next, 0); e != nil {
			return e
		}
		_, e := tx.ChangePrivateNavigation(workstore.ChangePrivateNavigationRequest{Source: source, ExpectedRevision: menu.NavigationRevision, Mode: work.PrivatePersonal, FrontContextID: next.ID, Now: time.Now()})
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = service.DiscoverPage(t.Context(), principal, "role", source, ""); e == nil {
		t.Fatal("old menu silently retargeted catalog after navigation")
	}
	if len(port.owners) != 1 {
		t.Fatal("stale navigation reached directory reader")
	}
	newer := sourceFor("second-menu")
	if _, e = service.DiscoverPage(t.Context(), principal, "role", newer, ""); e != nil {
		t.Fatal(e)
	}
	if len(port.owners) != 2 || port.owners[1].ContextID != "new-front" {
		t.Fatal("current authorized menu did not use its frozen context")
	}
}
