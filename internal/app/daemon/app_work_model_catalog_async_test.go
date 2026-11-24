package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func catalogAppFixture(t *testing.T) (*privateMCPReadFixture, string, work.RoleBotBinding) {
	return catalogAppFixtureWithWorker(t, true)
}
func catalogAppFixtureWithWorker(t *testing.T, withWorker bool) (*privateMCPReadFixture, string, work.RoleBotBinding) {
	t.Helper()
	var f *privateMCPReadFixture
	if withWorker {
		f = newPrivateMCPReadFixture(t, isolatedSDKFixtureAppID(t, "model-catalog"))
	} else {
		a, source := teamPickerFixture(t, isolatedSDKFixtureAppID(t, "cold-catalog"))
		f = &privateMCPReadFixture{a: a, source: source}
		pickerTx(t, a, func(tx *workstore.Tx) error {
			nav, e := tx.GetPrivateNavigation(source.ScopeID)
			f.carrier.Owner.ContextID = nav.FrontContextID
			return e
		})
	}
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: f.source.Gateway.AppID, AppSecret: "fixture"}}
	a, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	a.daemonLifecycleID = "fixture"
	a.headlessRuntime.CodexRealBinary = "fake-codex"
	a.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return catalogOAuthObservation("fixture@example.com", "plus"), nil
	}
	a.teamRuntime = f.a.teamRuntime
	f.a = a
	a.teamRuntime.coordinator = f.coordinator
	ctx, cancel := context.WithCancel(t.Context())
	a.teamRuntime.runContext = ctx
	t.Cleanup(func() { cancel(); _ = a.stopProfileCatalogWorkers(context.Background()) })
	_, _, err := a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	prefs, e := a.profileContextPreferenceStore()
	if e != nil {
		t.Fatal(e)
	}
	if e = prefs.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); e != nil {
		t.Fatal(e)
	}
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	var binding work.RoleBotBinding
	pickerTx(t, a, func(tx *workstore.Tx) error {
		var e error
		binding, e = tx.GetRoleBotBinding(f.source.RoleID)
		return e
	})
	snapshot, err := (teamBotSettingsPort{a}).Snapshot(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	target, err := a.teamProfileCatalog(t.Context(), binding, state.OAuthCodexProfileID, "")
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		return tx.SyncRoleSettingsReference(binding.RoleID, snapshot.Fingerprint)
	})
	a.runCodexModelCatalogProbe = func(ctx context.Context, _ codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		<-ctx.Done()
		return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
	}
	return f, target.Key, binding
}

func catalogOAuthObservation(email, plan string) codexprofile.OAuthProbeObservation {
	return codexprofile.OAuthProbeObservation{Result: codexprofile.ClassifyOAuthProbe(codexprofile.OAuthProbeEvidence{AccountType: "chatgpt", AuthMethod: "chatgpt", Email: email, PlanType: plan}), CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}
}
func catalogProbeResult(options codexprofile.ModelCatalogProbeOptions, model string) codexprofile.ModelCatalogProbeObservation {
	return codexprofile.ModelCatalogProbeObservation{Snapshot: agentproto.ModelCatalogSnapshot{RefreshedAt: time.Now().UTC(), Entries: []agentproto.ModelCatalogEntry{{ID: model, Model: model, IsDefault: true, DefaultReasoningEffort: "high", SupportedReasoningEfforts: []agentproto.ReasoningEffortOption{{ReasoningEffort: "high"}}}}}, EvidenceID: "native-readonly-" + model, AccountFingerprint: options.ExpectedAccountFingerprint, PlanType: options.ExpectedPlanType}
}

func TestModelCatalogIndependentProbeAndResultUpdateOriginalSDKCard(t *testing.T) {
	f, _, _ := catalogAppFixture(t)
	a := f.a
	calls := make(chan codexprofile.ModelCatalogProbeOptions, 4)
	release := make(chan struct{})
	a.runCodexModelCatalogProbe = func(ctx context.Context, o codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		calls <- o
		select {
		case <-release:
			return catalogProbeResult(o, "fresh-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	f.relay.send = func(_ string, c agentproto.Command) error {
		t.Errorf("business carrier received %s", c.Kind)
		return nil
	}
	var creates, patches atomic.Int32
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/callback/ws/endpoint":
			fmt.Fprint(w, `{"code":1,"msg":"fixture uses HTTP only"}`)
		case strings.Contains(r.URL.Path, "tenant_access_token"):
			fmt.Fprint(w, `{"code":0,"tenant_access_token":"catalog-token","expire":7200}`)
		case r.Method == http.MethodPost && r.URL.Path == "/open-apis/im/v1/messages":
			creates.Add(1)
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"receive_id":"private-chat"`) {
				t.Error("wrong private target")
			}
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"catalog-card","chat_id":"private-chat"}}`)
		case r.Method == http.MethodPatch && r.URL.Path == "/open-apis/im/v1/messages/catalog-card":
			patches.Add(1)
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), "fresh-model") {
				t.Error("catalog result missing from patch")
			}
			var envelope struct{ Content string }
			if e := json.Unmarshal(raw, &envelope); e != nil {
				t.Error(e)
			}
			var card struct {
				Body struct {
					Elements []json.RawMessage `json:"elements"`
				} `json:"body"`
			}
			if e := json.Unmarshal([]byte(envelope.Content), &card); e != nil {
				t.Error(e)
			}
			for i, want := range []string{"菜单首页 › 参数设置 › 模型", "返回参数设置", "返回菜单首页"} {
				if len(card.Body.Elements) <= i || !strings.Contains(string(card.Body.Elements[i]), want) {
					t.Errorf("automatic patch lost top navigation %s", want)
				}
			}
			fmt.Fprint(w, `{"code":0,"data":{"message_id":"catalog-card","chat_id":"private-chat"}}`)
		default:
			t.Errorf("unexpected SDK route %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer platform.Close()
	useDeliveryJourneyGateway(t, a, platform.URL, f.source.Gateway)
	id, err := a.queueTeamSettings(t.Context(), f.source, "model", "")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case o := <-calls:
		if o.ExpectedAccountFingerprint == "" {
			t.Fatal("probe missing exact account proof")
		}
	case <-time.After(time.Second):
		t.Fatal("empty model page did not launch independent probe")
	}
	close(release)
	a.profileCatalogWorkers.Wait()
	// Thirty-two older views have no publication receipt yet. They must not
	// permanently starve this ready result at position 33.
	pickerTx(t, a, func(tx *workstore.Tx) error {
		original, e := tx.GetUIView(id)
		if e != nil {
			return e
		}
		body, e := tx.GetContent(original.ScopeID, id+":settings-state")
		if e != nil {
			return e
		}
		var frozen teamSettingsState
		if e = json.Unmarshal(body.Data, &frozen); e != nil {
			return e
		}
		for i := 0; i < 32; i++ {
			view := original
			view.ID = fmt.Sprintf("a-blocked-%02d", i)
			view.Revision = 1
			if e = tx.PutUIView(view, 0); e != nil {
				return e
			}
			if e = a.registerTeamCatalogView(tx, view, frozen, worksettings.Snapshot{}); e != nil {
				return e
			}
		}
		return nil
	})
	// Data arrived before first publish: no fresh message may race the initial card.
	if err = a.advanceTeamModelCatalogViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n, e := a.teamRuntime.publisher.RunOnce(t.Context(), 16); e != nil || n != 1 {
		t.Fatalf("initial publish %d: %v", n, e)
	}
	if err = a.advanceTeamModelCatalogViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n, e := a.teamRuntime.publisher.RunOnce(t.Context(), 16); e != nil || n != 1 {
		t.Fatalf("result publish %d: %v", n, e)
	}
	if creates.Load() != 1 || patches.Load() != 1 {
		t.Fatalf("create=%d patch=%d", creates.Load(), patches.Load())
	}
	if err = a.advanceTeamModelCatalogViews(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n, e := a.teamRuntime.publisher.RunOnce(t.Context(), 16); e != nil || n != 0 {
		t.Fatalf("duplicate catalog publish %d: %v", n, e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		old, e := tx.GetUIView(id)
		if e != nil {
			return e
		}
		if !old.Invalidated {
			t.Fatal("old callbacks still active")
		}
		inputs, e := tx.ListInbox(f.source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("catalog invented user input")
		}
		return e
	})
	select {
	case <-calls:
		t.Fatal("duplicate independent query")
	default:
	}
}
