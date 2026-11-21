package daemon

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"os"
	"testing"
	"time"
)

func TestTeamReviewSelectsDirectoryAndStartsIndependentReadonlyWork(t *testing.T) {
	a, source := teamPickerFixture(t)
	hostname, e := os.Hostname()
	if e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		menu, _, err := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "review-menu", Tenant: source.Tenant, SenderIDs: source.SenderIDs, EventKey: "review", ReceivedAt: time.Now().UTC()}, source.Gateway)
		if err != nil {
			return err
		}
		source.Key = menu.ID
		source.Authority = workstore.PrivateActionSource{PersonalScopeID: source.ScopeID, MenuInteractionID: menu.ID}
		return tx.PutLocation(work.ExecutionLocation{ID: "selected-review-location", HostID: teamID("host", hostname+":"+a.headlessRuntime.Paths.StateDir), Cwd: t.TempDir(), Generation: 1, Revision: 1}, 0)
	})
	viewID, e := a.queueTeamReview(t.Context(), source, control.Action{Kind: control.ActionReviewCommand, Text: "/review"})
	if e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if view.Kind != "execution.location-picker" {
			t.Fatal("review without a directory did not ask for a real selection")
		}
		contexts, err := tx.ListContexts(source.ScopeID)
		if err != nil {
			return err
		}
		for _, wc := range contexts {
			if wc.Purpose == work.PurposeReview {
				t.Fatal("review started before human selected a directory")
			}
		}
		return nil
	})
	view, interaction := pickerAction(t, a, source, viewID, "picker.location.select")
	if _, e = a.handleTeamPickerAction(t.Context(), interaction, view, "picker.location.select"); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		wc, err := tx.GetContext(teamID("review-context", source.Key))
		if err != nil {
			return err
		}
		if wc.Purpose != work.PurposeReview || wc.Review == nil || wc.Review.LocationID != "selected-review-location" {
			t.Fatalf("review did not freeze selected directory: %+v", wc)
		}
		nav, err := tx.GetPrivateNavigation(source.ScopeID)
		if err != nil {
			return err
		}
		if nav.FrontContextID != wc.Review.SourceContextID {
			t.Fatal("review stole private navigation")
		}
		input, err := tx.GetInbox(wc.ID + ":input")
		if err != nil {
			return err
		}
		if input.RecipientContextID != wc.ID || input.State != work.InboxPending || input.TaskID != "" {
			t.Fatal("review has no independently queued work")
		}
		return nil
	})
}
