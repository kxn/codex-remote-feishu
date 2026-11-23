package daemon

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

func acceptSystemWebStatus(t *testing.T, a *App, source teamHumanSource, viewID, jobID string) (workstore.UIView, workstore.HumanInteraction) {
	t.Helper()
	markPrivateNativeViewPublished(t, a, viewID)
	var view workstore.UIView
	var interaction workstore.HumanInteraction
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var err error
		view, err = tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		interaction, _, err = tx.AcceptHumanInteraction(work.HumanInteractionEnvelope{GatewayRef: source.Gateway.GatewayRef, AppID: source.Gateway.AppID, EventID: "read-web-status:" + viewID, Tenant: source.Tenant, SenderIDs: source.SenderIDs, ChatID: source.ChatID, MessageID: view.ExternalMessageID, Kind: "card", Values: map[string]any{"team_view": viewID, "action": "system.web-status", "job_id": jobID}, ReceivedAt: time.Now().UTC()}, source.Gateway)
		return err
	})
	return view, interaction
}

type systemWebProvider struct {
	calls   atomic.Int32
	entered chan struct{}
	unblock chan struct{}
}

func (*systemWebProvider) Kind() string { return "fixture" }
func (*systemWebProvider) Snapshot() externalaccess.ProviderStatus {
	return externalaccess.ProviderStatus{Kind: "fixture"}
}
func (*systemWebProvider) Close() error { return nil }
func (p *systemWebProvider) EnsurePublicBase(ctx context.Context, _ string) (externalaccess.PublicBase, error) {
	p.calls.Add(1)
	if p.entered != nil {
		p.entered <- struct{}{}
	}
	if p.unblock != nil {
		select {
		case <-ctx.Done():
			return externalaccess.PublicBase{}, ctx.Err()
		case <-p.unblock:
		}
	}
	return externalaccess.PublicBase{BaseURL: "https://system-menu.fixture.invalid", StartedAt: time.Now().UTC()}, nil
}

func systemWebFixture(t *testing.T, slow bool) (*App, teamHumanSource, *systemWebProvider, string) {
	t.Helper()
	a, source, _ := systemMenuFixture(t)
	p := &systemWebProvider{}
	if slow {
		p.entered = make(chan struct{}, 1)
		p.unblock = make(chan struct{})
	}
	a.externalAccessRuntime.Settings.NetworkMode = "wan"
	a.externalAccessRuntime.Settings.ListenHost = "127.0.0.1"
	a.externalAccessRuntime.Settings.ListenPort = 0
	a.externalAccess = externalaccess.NewService(externalaccess.Options{Provider: p, DefaultLinkTTL: time.Minute, DefaultSessionTTL: time.Minute})
	t.Cleanup(func() {
		a.mu.Lock()
		plan := a.prepareExternalAccessShutdownLocked("fixture-finish", true)
		a.mu.Unlock()
		a.executeExternalAccessShutdownPlan(plan)
	})
	id, err := a.handleTeamAdminCommand(t.Context(), source, control.Action{Text: "/admin web"})
	if err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 0 {
		t.Fatal("cold tunnel startup ran on private command ingress")
	}
	markPrivateNativeViewPublished(t, a, id)
	return a, source, p, id
}

func TestSystemWebColdIssueUsesActualServiceAndUpdatesOriginalCard(t *testing.T) {
	a, source, p, pending := systemWebFixture(t, false)
	if err := a.runTeamSystemHostJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 1 {
		t.Fatal("cold issue did not invoke real external access service once")
	}
	id := workui.ID(teamID("system-web", source.Key), "result")
	card := systemCard(t, a, source.ScopeID, id)
	if !strings.Contains(card, "system-menu.fixture.invalid") || !strings.Contains(card, "open_url") || !strings.Contains(card, "有效期") {
		t.Fatalf("actual issued link missing from result: %s", card)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		view, err := tx.GetUIView(id)
		if err != nil {
			return err
		}
		if view.ReplacesViewID != pending {
			t.Fatal("link reply stole a new card instead of original pending page")
		}
		op, err := tx.GetOperation(teamID("system-web", source.Key))
		if op.State != work.OperationConfirmed {
			t.Fatal("actual link result not committed")
		}
		return err
	})
}

func TestSystemWebMigrationFenceBlocksLateCredentialAndWaitsForInflight(t *testing.T) {
	t.Run("late", func(t *testing.T) {
		a, source, provider, _ := systemWebFixture(t, false)
		if _, _, err := a.teamRuntime.beginPersonalSourceFence(t.Context(), source.ScopeID, "migration", time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := a.runTeamSystemHostJobs(t.Context()); err != nil {
			t.Fatal(err)
		}
		if provider.calls.Load() != 0 {
			t.Fatal("fenced private job issued a new admin credential")
		}
	})
	t.Run("inflight", func(t *testing.T) {
		a, source, provider, _ := systemWebFixture(t, true)
		done := make(chan error, 1)
		go func() { done <- a.runTeamSystemHostJobs(context.Background()) }()
		select {
		case <-provider.entered:
		case <-time.After(time.Second):
			t.Fatal("admin issuance did not start")
		}
		_, active, err := a.teamRuntime.beginPersonalSourceFence(t.Context(), source.ScopeID, "migration", time.Now())
		if err != nil || active != 1 {
			close(provider.unblock)
			t.Fatalf("active=%d err=%v", active, err)
		}
		wait, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if !errors.Is(a.teamRuntime.waitPersonalSourceActivities(wait, source.ScopeID), context.DeadlineExceeded) {
			close(provider.unblock)
			t.Fatal("migration did not wait for in-flight issuance")
		}
		close(provider.unblock)
		if err = <-done; err != nil {
			t.Fatal(err)
		}
		if err = a.teamRuntime.waitPersonalSourceActivities(t.Context(), source.ScopeID); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSystemWebSlowIssueDoesNotBlockPrivateNavigationOrReclaimMenu(t *testing.T) {
	a, source, p, _ := systemWebFixture(t, true)
	done := make(chan error, 1)
	go func() { done <- a.runTeamSystemHostJobs(context.Background()) }()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("slow fixture did not start")
	}
	next := source
	next.Key = "newer-menu-while-tunnel-starts"
	navigation := make(chan error, 1)
	go func() { _, err := a.queuePrivatePanel(context.Background(), next, "status", ""); navigation <- err }()
	select {
	case err := <-navigation:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow link issue blocked unrelated menu")
	}
	close(p.unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if _, err := tx.GetUIView(workui.ID(teamID("system-web", source.Key), "result")); err != workstore.ErrNotFound {
			t.Fatal("late link response reclaimed a menu user left")
		}
		return nil
	})
}

func TestSystemWebInterruptedMutationRemainsUnknownAndNeverReplays(t *testing.T) {
	a, source, p, _ := systemWebFixture(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.runTeamSystemHostJobs(ctx) }()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("fixture did not start")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := a.runTeamSystemHostJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 1 {
		t.Fatal("uncertain tunnel mutation was automatically repeated")
	}
	id := teamID("system-web", source.Key)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if op.State != work.OperationUnknown {
			t.Fatal("interrupted mutation claimed definite success/failure")
		}
		return err
	})
	card := systemCard(t, a, source.ScopeID, workui.ID(id, "result"))
	if !strings.Contains(card, "不会自动重发") || !strings.Contains(card, "再次操作") {
		t.Fatalf("unknown state lacks accurate next actions: %s", card)
	}
	previous := workui.ID(id, "result")
	view, read := acceptSystemWebStatus(t, a, source, previous, id)
	if err := a.handleTeamViewAction(t.Context(), read, view, "system.web-status"); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		saved, err := tx.GetHumanInteraction(read.ID)
		if err != nil {
			return err
		}
		next, err := tx.GetUIView(saved.ResultRef)
		if err != nil {
			return err
		}
		if next.ReplacesViewID != previous || next.TargetRevision != view.TargetRevision {
			t.Fatal("status read created a separate card or lost navigation ownership")
		}
		return nil
	})
	if p.calls.Load() != 1 {
		t.Fatal("reading uncertain status issued another URL")
	}
}

func TestSystemWebStatusRejectsLaterNavigationWithoutIssuingNewLink(t *testing.T) {
	a, source, p, _ := systemWebFixture(t, false)
	id := teamID("system-web", source.Key)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		before := op.Revision
		op.Revision++
		op.State = work.OperationSending
		_, _, err = tx.PutOperation(op, before)
		return err
	})
	if err := a.runTeamSystemHostJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, read := acceptSystemWebStatus(t, a, source, workui.ID(id, "result"), id)
	if _, err := a.teamRuntime.service.TrustedPrivateNavigate(t.Context(), workservice.TrustedPrivateNavigateRequest{Key: "leave-web-status", Source: source.Authority, Action: "new", ExpectedRevision: source.NavigationRevision}); err != nil {
		t.Fatal(err)
	}
	if err := a.handleTeamViewAction(t.Context(), read, view, "system.web-status"); err == nil {
		t.Fatal("old status button crossed a later navigation revision")
	}
	if p.calls.Load() != 0 {
		t.Fatal("stale status query started a tunnel")
	}
}

func TestSystemWebPersistedSendingIsNotRepeatedAfterRestart(t *testing.T) {
	a, source, p, _ := systemWebFixture(t, false)
	id := teamID("system-web", source.Key)
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		previous := op.Revision
		op.Revision++
		op.State = work.OperationSending
		_, _, err = tx.PutOperation(op, previous)
		return err
	})
	if err := a.runTeamSystemHostJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	if p.calls.Load() != 0 {
		t.Fatal("restart replayed an unknown link mutation")
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if op.State != work.OperationUnknown {
			t.Fatal("interrupted sending job was not kept unknown")
		}
		return err
	})
}
