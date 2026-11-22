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
	t.Helper()
	f := newPrivateMCPReadFixture(t, isolatedSDKFixtureAppID(t, "model-catalog"))
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: f.source.Gateway.AppID, AppSecret: "fixture"}}
	a, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	a.daemonLifecycleID = "fixture"
	a.headlessRuntime.CodexRealBinary = "fake-codex"
	a.runCodexOAuthProbe = func(context.Context, codexprofile.OAuthProbeOptions) (codexprofile.OAuthProbeObservation, error) {
		return codexprofile.OAuthProbeObservation{Result: codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusDetected, AccountHint: "fixture", PlanType: "plus"}, CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}, nil
	}
	a.teamRuntime = f.a.teamRuntime
	f.a = a
	a.teamRuntime.coordinator = f.coordinator
	ctx, cancel := context.WithCancel(t.Context())
	a.teamRuntime.runContext = ctx
	t.Cleanup(func() { cancel(); a.teamRuntime.workers.Wait() })
	_, _, err := a.codexOAuthProfileState.store.ApplyProbe(codexprofile.OAuthProbeObservation{Result: codexprofile.OAuthProbeResult{Status: codexprofile.OAuthProbeStatusDetected, AccountHint: "fixture", PlanType: "plus"}, CapabilitySet: codexprofile.CodexProfileCapabilitySetV1}, time.Now(), "fixture", false)
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
	connection, err := a.teamCatalogConnection(t.Context(), binding, state.OAuthCodexProfileID)
	if err != nil {
		t.Fatal(err)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error {
		if e := tx.SyncRoleSettingsReference(binding.RoleID, snapshot.Fingerprint); e != nil {
			return e
		}
		raw, _ := json.Marshal(map[string]any{"settings_revision": 1, "preferences_revision": 0, "profile_id": state.OAuthCodexProfileID, "backend": work.BackendCodex, "snapshot": agentproto.RuntimeLaunchSnapshot{Owner: f.carrier.Owner, CarrierIncarnation: f.carrier.Incarnation}, "codex_resume": agentproto.CodexResumePolicy{ConnectionContractID: connection}})
		body := work.NewContent("catalog-launch-body", f.source.ScopeID, "application/json", raw, time.Now())
		if e := tx.PutContent(body); e != nil {
			return e
		}
		_, _, e := tx.PutOperation(work.Operation{ID: "catalog-launch", ScopeID: f.source.ScopeID, ContextID: f.carrier.Owner.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: "worker.launch", TargetID: f.carrier.Owner.WorkerSessionID, TargetGeneration: 1, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: "catalog-launch", State: work.OperationConfirmed, ExternalReceipt: "fixture-carrier", Revision: 1}, 0)
		return e
	})
	return f, connection, binding
}

func catalogReply(t *testing.T, f *privateMCPReadFixture, command agentproto.Command, model string, observedAt ...time.Time) {
	t.Helper()
	f.sequence++
	at := time.Now().UTC()
	if len(observedAt) > 0 {
		at = observedAt[0]
	}
	event := agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, CommandID: command.CommandID, RequestID: command.CommandID, Execution: agentproto.CloneExecutionBinding(command.Execution), Seq: f.sequence, ModelCatalog: &agentproto.ModelCatalogSnapshot{RefreshedAt: at, Entries: []agentproto.ModelCatalogEntry{{ID: model, Model: model}}}}
	hash, e := agentproto.RuntimeEventPayloadHash(event)
	if e != nil {
		t.Fatal(e)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: f.carrier.Incarnation, Sequence: event.Seq, PayloadHash: hash}
	if e = f.coordinator.HandleEvent(t.Context(), f.carrier.OriginInstanceID, event); e != nil {
		t.Fatal(e)
	}
}

func TestModelCatalogHelloPrefetchAndResultUpdateOriginalSDKCard(t *testing.T) {
	f, _, _ := catalogAppFixture(t)
	a := f.a
	commands := make(chan agentproto.Command, 4)
	f.relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
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
	// The initial page may itself request discovery. Hello must coalesce with it.
	a.prefetchTeamModelCatalog(t.Context(), agentproto.Hello{CarrierIncarnation: f.carrier.Incarnation})
	var command agentproto.Command
	select {
	case command = <-commands:
	case <-time.After(time.Second):
		t.Fatal("no model query before manual refresh")
	}
	if command.Kind != agentproto.CommandModelList || command.Execution.AttemptID != "" {
		t.Fatal("catalog started business task")
	}
	catalogReply(t, f, command, "fresh-model")
	a.teamRuntime.workers.Wait()
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
	case c := <-commands:
		t.Fatalf("duplicate query %s", c.Kind)
	default:
	}
}
