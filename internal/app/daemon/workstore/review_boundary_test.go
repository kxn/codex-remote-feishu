package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func seedHumanReview(t *testing.T, s *Store) (MenuInteraction, work.WorkContext) {
	t.Helper()
	gateway := seedMenuRole(t, s, "reviewer", "review-app")
	var menu MenuInteraction
	var wc work.WorkContext
	requireTx(t, s, func(tx *Tx) error {
		raw := menuEnvelope(gateway, "review-event", "person")
		raw.EventKey = "review"
		var e error
		menu, _, e = tx.AcceptMenuInteraction(raw, gateway)
		if e != nil {
			return e
		}
		_, source, e := tx.ValidatePrivateAction(PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID})
		if e != nil {
			return e
		}
		if e = tx.PutLocation(work.ExecutionLocation{ID: "review-location", HostID: "local", Cwd: "/repo", Generation: 1, Revision: 1}, 0); e != nil {
			return e
		}
		body := work.NewContent("selection-source", menu.ScopeID, "text/plain", []byte("human selected repo"), time.Now().UTC())
		if e = tx.PutContent(body); e != nil {
			return e
		}
		if e = tx.PutExecutionPreference(ExecutionPreference{ScopeID: menu.ScopeID, SelectedLocationIDs: []string{"review-location"}, DefaultLocationID: "review-location", SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: menu.PrincipalID}, SourceRef: body.ID, Revision: 1}, 0); e != nil {
			return e
		}
		now := time.Now().UTC()
		wc = work.WorkContext{ID: "review-context", ScopeID: menu.ScopeID, RoleID: gateway.RoleID, Purpose: work.PurposeReview, Workstream: "review", Revision: 1, CreatedAt: now, UpdatedAt: now, Review: &work.ReviewSpec{SourceContextID: menu.ContextID, ResourceScopeID: menu.ScopeID, LocationID: "review-location", LocationGeneration: 1, Target: agentproto.ReviewTarget{Kind: agentproto.ReviewTargetKindUncommittedChanges}, Source: source}}
		return tx.PutContext(wc, 0)
	})
	return menu, wc
}
func TestReviewRequiresActualHumanSourceAndFrozenSelectedLocation(t *testing.T) {
	s, _ := openTestStore(t)
	_, wc := seedHumanReview(t, s)
	requireTx(t, s, func(tx *Tx) error {
		for _, change := range []func(*work.WorkContext){func(v *work.WorkContext) { v.Review.Source.ID = "invented" }, func(v *work.WorkContext) { v.Review.LocationGeneration = 2 }, func(v *work.WorkContext) { v.Review.SourceContextID = "foreign" }} {
			candidate := wc
			spec := *wc.Review
			candidate.Review = &spec
			candidate.ID = "bad-review"
			change(&candidate)
			if e := tx.PutContext(candidate, 0); e == nil {
				t.Fatal("unfrozen/unproven review accepted")
			}
		}
		if _, e := tx.GetContext("bad-review"); !errors.Is(e, ErrNotFound) {
			t.Fatal("failed review mutated database")
		}
		nav, e := tx.GetPrivateNavigation(wc.ScopeID)
		if e != nil {
			return e
		}
		before := nav.Revision
		nav.Revision++
		nav.FrontContextID = wc.ID
		if e = tx.putPrivateNavigation(nav, before); !errors.Is(e, ErrScopeDenied) && e == nil {
			t.Fatal("review selected as normal private front")
		}
		expected := wc.Revision
		wc.Revision++
		wc.Checkpoint = "review still same scope"
		return tx.PutContext(wc, expected)
	})
}
func TestReviewBridgeRejectsOrdinaryBusinessAttempt(t *testing.T) {
	s, _ := openTestStore(t)
	seedPersonal(t, s)
	seedRunningAttempt(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if _, _, e := tx.RecordReviewResult("attempt", evidenceSource(), "invented final", nil); !errors.Is(e, ErrScopeDenied) {
			t.Fatalf("ordinary attempt accepted final-text bridge: %v", e)
		}
		if _, e := tx.GetProcessingReport("attempt"); !errors.Is(e, ErrNotFound) {
			t.Fatal("invalid native bridge created processing report")
		}
		return nil
	})
}
func TestSessionGoalIntentIsDurableAndCannotBeClaimedByForeignContext(t *testing.T) {
	s, path := openTestStore(t)
	menu, wc := seedHumanReview(t, s)
	budget := int64(1200)
	goal := agentproto.GoalCommand{Objective: "finish existing work", TokenBudget: &budget}
	var intent SessionGoalIntent
	requireTx(t, s, func(tx *Tx) error {
		var e error
		intent, _, e = tx.QueueSessionGoalIntent("idle-goal", menu.ContextID, goal, PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}, time.Now().UTC())
		if e != nil {
			return e
		}
		if _, _, e = tx.QueueSessionGoalIntent("goal-review", wc.ID, goal, PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}, time.Now().UTC()); !errors.Is(e, ErrScopeDenied) {
			t.Fatalf("review accepted goal: %v", e)
		}
		input, e := tx.GetInbox(intent.InputID)
		if e != nil {
			return e
		}
		input.ID = "foreign-input"
		input.RecipientContextID = wc.ID
		if e = tx.PutInbox(input, 0); !errors.Is(e, ErrScopeDenied) {
			t.Fatalf("goal input changed owner: %v", e)
		}
		return nil
	})
	reopened, e := Open(t.Context(), path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		got, e := tx.GetSessionGoalIntent(intent.ID)
		if e != nil {
			return e
		}
		if got.ContextID != menu.ContextID || got.CommandID != "" {
			t.Fatal("reopen lost pending goal/claimed fake native application")
		}
		return nil
	})
}
