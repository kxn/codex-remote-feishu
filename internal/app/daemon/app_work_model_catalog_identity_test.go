package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func seedProfileCatalog(t *testing.T, f *privateMCPReadFixture, key string, at time.Time) {
	t.Helper()
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		if _, e := tx.BeginProfileModelCatalogRefresh(key, "seed", at); e != nil {
			return e
		}
		snapshot := catalogProbeResult(codexprofile.ModelCatalogProbeOptions{}, "last-good-model").Snapshot
		snapshot.RefreshedAt = at
		_, e := tx.CompleteProfileModelCatalogRefresh(key, "seed", &snapshot, "native-old-success", "", at)
		return e
	})
}
func catalogOwner(f *privateMCPReadFixture) worksettings.CatalogOwner {
	return worksettings.CatalogOwner{ContextID: f.carrier.Owner.ContextID, Backend: "codex", ProfileID: state.OAuthCodexProfileID}
}

func TestModelCatalogRestartKeepsThirtyDayChoicesFailurePreservesAndExplicitRefreshQueries(t *testing.T) {
	f, key, binding := catalogAppFixture(t)
	seedProfileCatalog(t, f, key, time.Now().Add(-30*24*time.Hour))
	previous, _ := f.a.codexOAuthProfileState.current()
	if _, _, e := f.a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "new-daemon", false); e != nil {
		t.Fatal(e)
	}
	current, _ := f.a.codexOAuthProfileState.current()
	target, e := f.a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, "")
	if e != nil {
		t.Fatal(e)
	}
	if target.Key != key || current.AuthGeneration != previous.AuthGeneration+1 {
		t.Fatal("restart invalidated catalog identity or weakened execution generation")
	}
	calls := make(chan struct{}, 4)
	release := make(chan struct{})
	f.a.runCodexModelCatalogProbe = func(ctx context.Context, _ codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- struct{}{}
		select {
		case <-release:
			return codexprofile.ModelCatalogProbeObservation{}, fmt.Errorf("offline-model-service")
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	got, e := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, catalogOwner(f))
	if e != nil || got.Snapshot == nil || got.Snapshot.Entries[0].Model != "last-good-model" {
		t.Fatalf("old cache not immediately selectable: %+v %v", got, e)
	}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("stale page not refreshed in background")
	}
	close(release)
	f.a.teamRuntime.workers.Wait()
	got, e = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, catalogOwner(f))
	if e != nil || got.Snapshot == nil || got.Unavailable == "" {
		t.Fatalf("failure lost lastgood: %+v %v", got, e)
	}
	release = make(chan struct{})
	f.a.runCodexModelCatalogProbe = func(ctx context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- struct{}{}
		select {
		case <-release:
			return catalogProbeResult(o, "fresh-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	snapshot, e := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if e != nil {
		t.Fatal(e)
	}
	frozen := teamSettingsState{ContextID: f.carrier.Owner.ContextID, Fingerprint: snapshot.Fingerprint}
	for i := 0; i < 2; i++ {
		if e = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, frozen); e != nil {
			t.Fatal(e)
		}
	}
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("explicit refresh returned cache without new query")
	}
	select {
	case <-calls:
		t.Fatal("simultaneous refresh duplicated process")
	default:
	}
	got, e = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, catalogOwner(f))
	if e != nil || got.Snapshot == nil || got.Snapshot.Entries[0].Model != "last-good-model" {
		t.Fatal("pending explicit refresh hid choices")
	}
	close(release)
	f.a.teamRuntime.workers.Wait()
	got, e = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, catalogOwner(f))
	if e != nil || got.Snapshot == nil || got.Snapshot.Entries[0].Model != "fresh-model" {
		t.Fatalf("new result not selected: %+v %v", got, e)
	}
}

func TestModelCatalogAccountPlanAndPageHaveSeparatePersistentNamespaces(t *testing.T) {
	f, key, binding := catalogAppFixture(t)
	seedProfileCatalog(t, f, key, time.Now())
	for _, tc := range []struct{ email, plan, cursor string }{{"different@example.com", "plus", ""}, {"fixture@example.com", "pro", ""}, {"fixture@example.com", "plus", "page-two"}} {
		if _, _, e := f.a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation(tc.email, tc.plan), time.Now(), "fixture", false); e != nil {
			t.Fatal(e)
		}
		target, e := f.a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, tc.cursor)
		if e != nil {
			t.Fatal(e)
		}
		if target.Key == key || target.Options.Cursor != tc.cursor {
			t.Fatal("different account/plan/page reused cached choices")
		}
		pickerTx(t, f.a, func(tx *workstore.Tx) error {
			if _, e := tx.GetProfileModelCatalog(target.Key); e != workstore.ErrNotFound {
				t.Fatalf("unrelated namespace returned cache: %v", e)
			}
			return nil
		})
	}
}

func TestModelCatalogAPIEndpointChangeSeparatesNamespaces(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	record, e := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{Name: "fixture", BaseURL: "https://api-one.example/v1", APIKey: "test-secret", Model: "configured-model", ReasoningEffort: "high"})
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := f.a.loadAdminConfig()
	if e != nil {
		t.Fatal(e)
	}
	loaded.Config.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	if e = config.WriteAppConfig(loaded.Path, loaded.Config); e != nil {
		t.Fatal(e)
	}
	prefs, e := f.a.profileContextPreferenceStore()
	if e != nil {
		t.Fatal(e)
	}
	if e = prefs.EnsureCodexProfile(record.ID, state.CodexContextModeDefault); e != nil {
		t.Fatal(e)
	}
	first, e := f.a.teamProfileCatalog(t.Context(), binding, record.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	updated, _, e := config.PrepareCodexAPIProfileUpdate(record, config.CodexAPIProfileInput{Name: "fixture", BaseURL: "https://api-two.example/v1", APIKey: "test-secret", Model: "configured-model", ReasoningEffort: "high"})
	if e != nil {
		t.Fatal(e)
	}
	loaded.Config.Codex.Profiles = []config.CodexAPIProfileRecord{updated}
	if e = config.WriteAppConfig(loaded.Path, loaded.Config); e != nil {
		t.Fatal(e)
	}
	second, e := f.a.teamProfileCatalog(t.Context(), binding, record.ID, "")
	if e != nil {
		t.Fatal(e)
	}
	if first.Key == second.Key {
		t.Fatal("changed endpoint borrowed old directory")
	}
}

func TestModelCatalogIndependentProbePaginationPreservesNativeCursor(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	calls := make(chan string, 3)
	f.a.runCodexModelCatalogProbe = func(_ context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- o.Cursor
		model := "page-one-model"
		if o.Cursor != "" {
			model = "page-two-model"
		}
		result := catalogProbeResult(o, model)
		if o.Cursor == "" {
			result.Snapshot.NextCursor = "native-page-two"
		}
		return result, nil
	}
	owner := catalogOwner(f)
	for _, cursor := range []string{"", "native-page-two"} {
		owner.Cursor = cursor
		if _, e := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner); e != nil {
			t.Fatal(e)
		}
		f.a.teamRuntime.workers.Wait()
		got, e := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
		if e != nil || got.Snapshot == nil {
			t.Fatalf("page unavailable: %+v %v", got, e)
		}
		want := "page-one-model"
		if cursor != "" {
			want = "page-two-model"
		}
		if got.Snapshot.Entries[0].Model != want {
			t.Fatal("page returned another cursor cache")
		}
		if cursor == "" && got.Snapshot.NextCursor != "native-page-two" {
			t.Fatal("lost native continuation cursor")
		}
		select {
		case actual := <-calls:
			if actual != cursor {
				t.Fatal("probe received wrong cursor")
			}
		default:
			t.Fatal("page query absent")
		}
	}
	select {
	case <-calls:
		t.Fatal("fresh page queried repeatedly")
	default:
	}
}

func TestModelCatalogCapacityQueuesExplicitFreshCacheRefreshAndDoesNotLoseRequest(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	targets := make([]teamProfileCatalogTarget, 3)
	for i, cursor := range []string{"slot-one", "slot-two", "queued-third"} {
		target, e := f.a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, cursor)
		if e != nil {
			t.Fatal(e)
		}
		targets[i] = target
	}
	seedProfileCatalog(t, f, targets[2].Key, time.Now())
	calls := make(chan string, 4)
	release := make(chan struct{})
	f.a.runCodexModelCatalogProbe = func(ctx context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- o.Cursor
		if o.Cursor == "queued-third" {
			return codexprofile.ModelCatalogProbeObservation{}, fmt.Errorf("third-refresh-failed")
		}
		select {
		case <-release:
			return catalogProbeResult(o, "slot-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	for i := 0; i < 2; i++ {
		if _, e := f.a.readOrRefreshProfileCatalog(t.Context(), targets[i], true); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(time.Second):
			t.Fatal("two read-only probes did not occupy capacity")
		}
	}
	snapshot, e := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if e != nil {
		t.Fatal(e)
	}
	frozen := teamSettingsState{ContextID: f.carrier.Owner.ContextID, Fingerprint: snapshot.Fingerprint, CursorTrail: []string{"queued-third"}}
	if e = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, frozen); e != nil {
		t.Fatal(e)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		got, e := tx.GetProfileModelCatalog(targets[2].Key)
		if e != nil {
			return e
		}
		if !got.Refreshing || got.EvidenceID != "native-old-success" || got.Snapshot.Entries[0].Model != "last-good-model" {
			t.Fatal("capacity-full explicit request was lost or cache hidden")
		}
		return nil
	})
	select {
	case <-calls:
		t.Fatal("third probe escaped concurrency bound")
	default:
	}
	close(release)
	f.a.teamRuntime.workers.Wait()
	owner := catalogOwner(f)
	owner.Cursor = "queued-third"
	got, e := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if e != nil || got.Snapshot == nil {
		t.Fatalf("queued request hid choices: %+v %v", got, e)
	}
	select {
	case cursor := <-calls:
		if cursor != "queued-third" {
			t.Fatal("wrong queued namespace queried")
		}
	case <-time.After(time.Second):
		t.Fatal("freed capacity did not run persisted explicit refresh")
	}
	f.a.teamRuntime.workers.Wait()
	got, e = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if e != nil || got.Snapshot == nil || got.Snapshot.Entries[0].Model != "last-good-model" || got.Unavailable == "" {
		t.Fatalf("queued failure lost last good choices: %+v %v", got, e)
	}
}
