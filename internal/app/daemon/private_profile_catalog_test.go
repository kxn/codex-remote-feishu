package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateCatalogUsesSharedProbeWithoutAttachedCarrier(t *testing.T) {
	a, g := newApplicationRuntimeTestApp(t)
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	if _, _, err := a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	r := a.applications
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "catalog-menu", MessageID: "catalog-menu", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/menu"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	if len(g.waitForOperationCount(1, 3*time.Second)) == 0 {
		t.Fatal("menu not published")
	}
	o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gateway.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
	cp, err := r.journal.LoadCheckpoint(t.Context(), o, 1)
	if err != nil || cp.Core.AttachedInstanceID != "" {
		t.Fatalf("unexpected carrier: %+v %v", cp.Core, err)
	}
	settings, err := a.readPrivateBotSettings(o.GatewayID, o.AppID)
	if err != nil {
		t.Fatal(err)
	}
	profile := settings.Record.CodexProfileID
	if profile == "" {
		profile = cp.Core.CodexProfileID
	}
	profile = state.NormalizeCodexProfileID(profile)
	prefs, err := a.profileContextPreferenceStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = prefs.EnsureCodexProfile(profile, state.CodexContextModeDefault); err != nil {
		t.Fatal(err)
	}
	settings, err = a.readPrivateBotSettings(o.GatewayID, o.AppID)
	if err != nil {
		t.Fatal(err)
	}
	a.runCodexModelCatalogProbe = func(_ context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		return catalogProbeResult(options, "private-model"), nil
	}
	first, err := a.ReadOrRefreshPrivateProfileCatalog(t.Context(), o, 1, profile, settings.Fingerprint, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Unavailable != "" && !first.Refreshing {
		t.Fatalf("probe unavailable: %s", first.Unavailable)
	}
	a.profileCatalogWorkers.Wait()
	got, err := a.ReadOrRefreshPrivateProfileCatalog(t.Context(), o, 1, profile, settings.Fingerprint, "", false)
	if err != nil || got.Snapshot == nil || len(got.Snapshot.Entries) == 0 || got.Snapshot.Entries[0].Model != "private-model" {
		t.Fatalf("catalog: %+v %v", got, err)
	}
	if _, err = a.ReadOrRefreshPrivateProfileCatalog(t.Context(), o, 1, profile, "stale", "", false); err != privatechat.ErrJournalConflict {
		t.Fatalf("stale settings: %v", err)
	}
}

func TestProfileCatalogShutdownCancelsAndJoinsProbe(t *testing.T) {
	f, _, binding := catalogAppFixtureWithWorker(t, false)
	started := make(chan struct{})
	f.a.runCodexModelCatalogProbe = func(ctx context.Context, _ codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		close(started)
		<-ctx.Done()
		return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
	}
	target, err := f.a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.readOrRefreshProfileCatalog(t.Context(), target, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = f.a.stopProfileCatalogWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		record, e := tx.GetProfileModelCatalog(target.Key)
		if e != nil {
			return e
		}
		if !record.Refreshing || record.LastError != "" {
			t.Fatalf("shutdown suppressed restart retry: %+v", record)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.a.readOrRefreshProfileCatalog(t.Context(), target, true); err != errApplicationClosing {
		t.Fatalf("shutdown allowed new probe: %v", err)
	}
}
