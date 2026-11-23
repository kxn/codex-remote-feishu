package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestModelCatalogFailurePatchesOriginalCardAndContinuesObserving(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprint("cache=", cached), func(t *testing.T) {
			f, _, binding := catalogAppFixture(t)
			var creates, patches int
			var lastPatch string
			platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "tenant_access_token"):
					fmt.Fprint(w, `{"code":0,"tenant_access_token":"fixture","expire":7200}`)
				case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
					creates++
					fmt.Fprint(w, `{"code":0,"data":{"message_id":"catalog-failure-card","chat_id":"private-chat"}}`)
				case r.Method == http.MethodPatch && r.URL.Path == "/open-apis/im/v1/messages/catalog-failure-card":
					patches++
					body, _ := io.ReadAll(r.Body)
					lastPatch = string(body)
					fmt.Fprint(w, `{"code":0,"data":{"message_id":"catalog-failure-card","chat_id":"private-chat"}}`)
				case r.URL.Path == "/callback/ws/endpoint":
					fmt.Fprint(w, `{"code":1}`)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer platform.Close()
			useDeliveryJourneyGateway(t, f.a, platform.URL, f.source.Gateway)
			id, err := f.a.queueTeamSettings(t.Context(), f.source, "model", "")
			if err != nil {
				t.Fatal(err)
			}
			publish := func(want int) {
				t.Helper()
				n, e := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16)
				if e != nil || n != want {
					t.Fatalf("publish %d want %d: %v", n, want, e)
				}
			}
			publish(1)
			snapshot, err := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
			if err != nil {
				t.Fatal(err)
			}
			var view workstore.UIView
			var state teamSettingsState
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				var e error
				view, e = tx.GetUIView(id)
				if e != nil {
					return e
				}
				b, e := tx.GetContent(view.ScopeID, id+":settings-state")
				if e != nil {
					return e
				}
				return json.Unmarshal(b.Data, &state)
			})
			owner := worksettings.CatalogOwner{Backend: snapshot.Settings.Backend, ProfileID: teamSettingsProfile(snapshot.Settings), SettingsRevision: 1}
			observed := worksettings.ObservedCatalog{Evidence: "attempt-failed-1", Unavailable: "模型列表刷新失败，请重试。"}
			if cached {
				observed.Snapshot = &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "last-good", Model: "last-good"}}}
			}
			if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, state, owner, snapshot, observed); err != nil {
				t.Fatal(err)
			}
			publish(1)
			if creates != 1 || patches != 1 || !strings.Contains(lastPatch, observed.Unavailable) || !strings.Contains(lastPatch, "刷新列表") {
				t.Fatalf("failure missing original-card retry: creates=%d patches=%d", creates, patches)
			}
			if cached && !strings.Contains(lastPatch, "last-good") {
				t.Fatal("refresh discarded lastgood choices")
			}
			nextID := workui.ID(view.ID, "catalog-ready", observed.Evidence)
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				var e error
				view, e = tx.GetUIView(nextID)
				if e != nil {
					return e
				}
				b, e := tx.GetContent(view.ScopeID, nextID+":catalog-baseline")
				if e != nil {
					return e
				}
				var baseline string
				if e = json.Unmarshal(b.Data, &baseline); e != nil {
					return e
				}
				if baseline != observed.Evidence {
					t.Fatal("missing failure baseline")
				}
				return nil
			})
			if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, state, owner, snapshot, observed); err != nil {
				t.Fatal(err)
			}
			publish(0)
			observed = worksettings.ObservedCatalog{Evidence: "attempt-recovered-2", Snapshot: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "recovered-model", Model: "recovered-model"}}}}
			if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, state, owner, snapshot, observed); err != nil {
				t.Fatal(err)
			}
			publish(1)
			if creates != 1 || patches != 2 || !strings.Contains(lastPatch, "recovered-model") || strings.Contains(lastPatch, "模型列表刷新失败") {
				t.Fatalf("recovery not patched: %s", lastPatch)
			}
			recoveredID := workui.ID(view.ID, "catalog-ready", observed.Evidence)
			pickerTx(t, f.a, func(tx *workstore.Tx) error {
				var e error
				view, e = tx.GetUIView(recoveredID)
				if e != nil {
					return e
				}
				if cached {
					next := binding
					next.IdentityGeneration++
					return tx.PutRoleBotBinding(next, binding.IdentityGeneration)
				}
				nav, e := tx.GetPrivateNavigation(view.ScopeID)
				if e != nil {
					return e
				}
				_, e = tx.ChangePrivateNavigation(workstore.ChangePrivateNavigationRequest{Source: f.source.Authority, ExpectedRevision: nav.Revision, Mode: work.PrivatePersonal, FrontContextID: nav.FrontContextID, Now: time.Now()})
				return e
			})
			observed = worksettings.ObservedCatalog{Evidence: "late-failure", Unavailable: "迟到的失败不得夺回页面"}
			if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, state, owner, snapshot, observed); err != nil {
				t.Fatal(err)
			}
			publish(0)

		})
	}
}

func TestModelCatalogReasoningShowsCachedRefreshFailure(t *testing.T) {
	snapshot := worksettings.Snapshot{CatalogUnavailable: "模型列表刷新失败，请重试。", Catalog: &agentproto.ModelCatalogSnapshot{Entries: []agentproto.ModelCatalogEntry{{ID: "cached-default", Model: "cached-default", IsDefault: true, SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}}}}
	card, err := teamSettingsCard("reasoning-error", snapshot, work.ContextPreferences{}, "reasoning", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{snapshot.CatalogUnavailable, "high", "刷新列表"} {
		if !strings.Contains(string(card), want) {
			t.Fatalf("missing %q in reasoning card", want)
		}
	}
}

func TestModelCatalogProjectionRejectsAccountSwitchWithSameMaskedHint(t *testing.T) {
	f, key, binding := catalogAppFixture(t)
	seedProfileCatalog(t, f, key, time.Now())
	var patches int
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"fixture","expire":7200}`)
		case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"identity-card","chat_id":"private-chat"}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/open-apis/im/v1/messages/identity-card":
			patches++
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"identity-card","chat_id":"private-chat"}}`)
		case r.URL.Path == "/callback/ws/endpoint":
			fmt.Fprint(w, `{"code":1}`)
		default:
			t.Errorf("unexpected SDK request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer platform.Close()
	useDeliveryJourneyGateway(t, f.a, platform.URL, f.source.Gateway)
	id, err := f.a.queueTeamSettings(t.Context(), f.source, "model", "")
	if err != nil {
		t.Fatal(err)
	}
	publish := func(want int) {
		t.Helper()
		n, e := f.a.teamRuntime.publisher.RunOnce(t.Context(), 16)
		if e != nil || n != want {
			t.Fatalf("publish %d want %d: %v", n, want, e)
		}
	}
	publish(1)
	snapshot, err := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	var view workstore.UIView
	var frozen teamSettingsState
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		var e error
		view, e = tx.GetUIView(id)
		if e != nil {
			return e
		}
		body, e := tx.GetContent(view.ScopeID, id+":settings-state")
		if e != nil {
			return e
		}
		if e = json.Unmarshal(body.Data, &frozen); e != nil {
			return e
		}
		if _, e = tx.BeginProfileModelCatalogRefresh(key, "late-original-account-result", time.Now()); e != nil {
			return e
		}
		result := catalogProbeResult(codexprofile.ModelCatalogProbeOptions{}, "original-account-model")
		_, e = tx.CompleteProfileModelCatalogRefresh(key, "late-original-account-result", &result.Snapshot, result.EvidenceID, "", time.Now())
		return e
	})
	owner := catalogOwner(f)
	owner.SettingsRevision = 1
	observed, err := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Identity != key || observed.Snapshot == nil || observed.Evidence == "" {
		t.Fatalf("invalid original observation: %+v %v", observed, err)
	}
	previous, _ := f.a.codexOAuthProfileState.current()
	if _, _, err = f.a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture-other@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	current, _ := f.a.codexOAuthProfileState.current()
	if current.AccountHint != previous.AccountHint {
		t.Fatalf("fixture masked hints differ: %q %q", previous.AccountHint, current.AccountHint)
	}
	if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, frozen, owner, snapshot, observed); err != nil {
		t.Fatal(err)
	}
	publish(0)
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		_, e := tx.GetContent(view.ScopeID, id+":catalog-result")
		if e != workstore.ErrNotFound {
			t.Fatalf("stale identity wrote result marker: %v", e)
		}
		return nil
	})
	target, err := f.a.teamProfileCatalog(t.Context(), binding, owner.ProfileID, "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Key == key {
		t.Fatal("actual account switch retained original identity key")
	}
	seedProfileCatalog(t, f, target.Key, time.Now())
	observed, err = (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Identity != target.Key || observed.Snapshot == nil {
		t.Fatalf("invalid current observation: %+v %v", observed, err)
	}
	if err = f.a.projectTeamModelCatalogObservation(t.Context(), view, frozen, owner, snapshot, observed); err != nil {
		t.Fatal(err)
	}
	publish(1)
	if patches != 1 {
		t.Fatalf("current account result patches=%d", patches)
	}
}
