package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestModelCatalogModelEditReusesLoginButAccountSwitchCannotBorrowOldCarrier(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	commands := make(chan agentproto.Command, 4)
	f.relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
	f.a.prefetchTeamModelCatalog(t.Context(), agentproto.Hello{CarrierIncarnation: f.carrier.Incarnation})
	var command agentproto.Command
	select {
	case command = <-commands:
	case <-time.After(time.Second):
		t.Fatal("Hello did not prefetch")
	}
	catalogReply(t, f, command, "account-a-model")
	f.a.teamRuntime.workers.Wait()
	owner := worksettings.CatalogOwner{ContextID: f.carrier.Owner.ContextID, WorkerID: f.carrier.Owner.WorkerSessionID, SessionGeneration: 1, SettingsRevision: 1, Backend: agentproto.BackendCodex, ProfileID: state.OAuthCodexProfileID}
	// Selecting a model must not invalidate an account-wide catalog.
	setTestWorkBotSettings(t, f.a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID, PromptOverride: state.ModelConfigRecord{Model: "account-a-model", ReasoningEffort: "high"}})
	snapshot, err := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		if e := tx.SyncRoleSettingsReference(binding.RoleID, snapshot.Fingerprint); e != nil {
			return e
		}
		role, e := tx.GetRole(binding.RoleID)
		owner.SettingsRevision = role.SettingsRevision
		return e
	})
	observed, err := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Snapshot == nil || observed.Evidence != command.CommandID {
		t.Fatalf("thread edit lost catalog: %+v %v", observed, err)
	}
	// Explicit refresh issues a new owner-only query even while cache is fresh.
	frozen := teamSettingsState{ContextID: owner.ContextID, Fingerprint: snapshot.Fingerprint}
	if err = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, frozen); err != nil {
		t.Fatal(err)
	}
	if err = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, frozen); err != nil {
		t.Fatal(err)
	}
	var refresh agentproto.Command
	select {
	case refresh = <-commands:
	case <-time.After(time.Second):
		t.Fatal("refresh only returned cached page")
	}
	if refresh.CommandID == command.CommandID || refresh.Kind != agentproto.CommandModelList || refresh.Execution.AttemptID != "" {
		t.Fatal("refresh lost read-only request boundary")
	}
	select {
	case <-commands:
		t.Fatal("repeated refresh duplicated in-flight query")
	default:
	}
	stillVisible, e := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if e != nil || stillVisible.Snapshot == nil || stillVisible.Evidence != command.CommandID {
		t.Fatal("refresh discarded last good choices while request pending")
	}
	catalogReply(t, f, refresh, "account-a-new-model")
	f.a.teamRuntime.workers.Wait()
	observed, err = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Evidence != refresh.CommandID || observed.Snapshot.Entries[0].Model != "account-a-new-model" {
		t.Fatalf("refresh did not replace observed page: %+v %v", observed, err)
	}
	_, _, err = f.a.codexOAuthProfileState.store.ApplyProbe(codexprofile.OAuthProbeObservation{Result: codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusDetected, AccountHint: "different-account", PlanType: "plus"}, CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}, time.Now(), "fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	observed, err = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Snapshot != nil {
		t.Fatalf("new login borrowed old model choices: %+v %v", observed, err)
	}
	if err = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, frozen); err == nil {
		t.Fatal("disconnected identity refresh gave no explanation")
	}
	select {
	case c := <-commands:
		t.Fatalf("new login queried old identity: %s", c.CommandID)
	default:
	}
}

func TestModelCatalogLateResultCannotReplaceNewSettingsPage(t *testing.T) {
	f, _, _ := catalogAppFixture(t)
	commands := make(chan agentproto.Command, 2)
	f.relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
	old, err := f.a.queueTeamSettings(t.Context(), f.source, "model", "")
	if err != nil {
		t.Fatal(err)
	}
	var c agentproto.Command
	select {
	case c = <-commands:
	case <-time.After(time.Second):
		t.Fatal("no discovery")
	}
	source := f.source
	source.Key = "newer-overview"
	newer, err := f.a.queueTeamSettings(t.Context(), source, "overview", "")
	if err != nil {
		t.Fatal(err)
	}
	catalogReply(t, f, c, "late-model")
	f.a.teamRuntime.workers.Wait()
	if err = f.a.advanceTeamModelCatalogViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		oldView, e := tx.GetUIView(old)
		if e != nil {
			return e
		}
		if !oldView.Invalidated {
			t.Fatal("old model page remains current")
		}
		newView, e := tx.GetUIView(newer)
		if e != nil {
			return e
		}
		if newView.Invalidated {
			t.Fatal("late catalog replaced newer overview")
		}
		views, e := tx.ListUIViews(f.source.ScopeID)
		if len(views) != 2 {
			t.Fatalf("late result created extra view: %d", len(views))
		}
		return e
	})
}

func TestModelCatalogExpiredExactPageRefreshesWhileRetainingLastGoodChoices(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	commands := make(chan agentproto.Command, 4)
	f.relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
	f.a.prefetchTeamModelCatalog(t.Context(), agentproto.Hello{CarrierIncarnation: f.carrier.Incarnation})
	var c agentproto.Command
	select {
	case c = <-commands:
	case <-time.After(time.Second):
		t.Fatal("no discovery")
	}
	catalogReply(t, f, c, "last-good-model", time.Now().Add(-6*time.Minute))
	f.a.teamRuntime.workers.Wait()
	owner := worksettings.CatalogOwner{ContextID: f.carrier.Owner.ContextID, WorkerID: f.carrier.Owner.WorkerSessionID, SessionGeneration: 1, SettingsRevision: 1, Backend: agentproto.BackendCodex, ProfileID: state.OAuthCodexProfileID}
	got, err := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || got.Snapshot == nil || got.Evidence != c.CommandID {
		t.Fatalf("last good not retained: %+v %v", got, err)
	}
	var refresh agentproto.Command
	select {
	case refresh = <-commands:
	case <-time.After(time.Second):
		t.Fatal("expired exact key bypassed TTL without new query")
	}
	catalogReply(t, f, refresh, "fresh-model")
	f.a.teamRuntime.workers.Wait()
	got, err = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || got.Evidence != refresh.CommandID {
		t.Fatalf("fresh result not selected: %+v %v", got, err)
	}
}
