package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worksettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestModelCatalogColdPagePreparesCarrierAndHelloQueriesWithoutBusinessPrompt(t *testing.T) {
	base, source := teamPickerFixture(t, isolatedSDKFixtureAppID(t, "cold-model"))
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: source.Gateway.AppID, AppSecret: "fixture"}}
	a, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	a.teamRuntime = base.teamRuntime
	a.runCodexNativeConfigProbe = func(context.Context, codexprofile.NativeConfigProbeOptions) (codexprofile.NativeConfigObservation, error) {
		return codexprofile.NativeConfigObservation{}, nil
	}
	a.ensureCodexNativeConnectionEvidence(t.Context())
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.NativeCodexProfileID})
	var binding work.RoleBotBinding
	pickerTx(t, a, func(tx *workstore.Tx) error { var e error; binding, e = tx.GetRoleBotBinding(source.RoleID); return e })
	snapshot, e := (teamBotSettingsPort{a}).Snapshot(t.Context(), binding)
	if e != nil {
		t.Fatal(e)
	}
	pickerTx(t, a, func(tx *workstore.Tx) error { return tx.SyncRoleSettingsReference(source.RoleID, snapshot.Fingerprint) })
	ctx, cancel := context.WithCancel(t.Context())
	a.teamRuntime.runContext = ctx
	t.Cleanup(func() { cancel(); a.teamRuntime.workers.Wait() })
	relay := &steerOuterRelay{}
	driver, err := worktransport.New(a.teamRuntime.store, relay)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := workruntime.New(a.teamRuntime.store, workruntime.Options{Driver: driver})
	driver.BindRegistrar(coordinator)
	a.teamRuntime.driver, a.teamRuntime.coordinator = driver, coordinator
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	host, err := workhost.New(a.teamRuntime.store, a.teamRuntime.service, signer, workhost.Config{Paths: a.headlessRuntime.Paths, BinaryPath: "fake-wrapper", HostID: "host", ToolEndpoint: "http://127.0.0.1:9090", Settings: teamSettingsResolver{app: a, store: a.teamRuntime.store, hostID: "host"}, Registry: coordinator, Runtime: teamSettingsTestRuntime{}, Start: func(relayruntime.HeadlessLaunchOptions) (int, error) { launches++; return 1234, nil }})
	if err != nil {
		t.Fatal(err)
	}
	a.teamRuntime.host = host
	if _, err = a.queueTeamSettings(t.Context(), source, "model", ""); err != nil {
		t.Fatal(err)
	}
	if launches != 1 {
		t.Fatalf("cold page carrier launches=%d", launches)
	}
	var worker work.WorkerSession
	pickerTx(t, a, func(tx *workstore.Tx) error {
		nav, e := tx.GetPrivateNavigation(source.ScopeID)
		if e != nil {
			return e
		}
		worker, e = tx.CurrentContextWorker(nav.FrontContextID)
		if e != nil {
			return e
		}
		inputs, e := tx.ListInbox(source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("opening model page queued business input")
		}
		return e
	})
	commands := make(chan agentproto.Command, 2)
	relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
	a.handleWorkHello(t.Context(), agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: worker.InstanceID, Backend: agentproto.BackendCodex, Managed: true, Source: "headless"}, WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: worker.CarrierIncarnation})
	select {
	case c := <-commands:
		if c.Kind != agentproto.CommandModelList || c.Execution.AttemptID != "" {
			t.Fatal("cold model page launched a business command")
		}
	case <-time.After(time.Second):
		t.Fatal("cold Hello failed to query model list")
	}
}

func TestModelCatalogExplicitRefreshPreparesCarrierEvenWhenLastGoodCacheHasNoCurrentWorker(t *testing.T) {
	f, _, binding := catalogAppFixture(t)
	commands := make(chan agentproto.Command, 4)
	f.relay.send = func(_ string, c agentproto.Command) error { commands <- c; return nil }
	f.a.prefetchTeamModelCatalog(t.Context(), agentproto.Hello{CarrierIncarnation: f.carrier.Incarnation})
	var c agentproto.Command
	select {
	case c = <-commands:
	case <-time.After(time.Second):
		t.Fatal("no initial catalog")
	}
	catalogReply(t, f, c, "last-good-model")
	f.a.teamRuntime.workers.Wait()
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		worker, e := tx.GetWorker(f.carrier.Owner.WorkerSessionID)
		if e != nil {
			return e
		}
		before := worker.Revision
		worker.Revision++
		worker.State = work.WorkerRetired
		return tx.PutWorker(worker, before)
	})
	owner := worksettings.CatalogOwner{ContextID: f.carrier.Owner.ContextID, SettingsRevision: 1, Backend: agentproto.BackendCodex, ProfileID: state.OAuthCodexProfileID}
	observed, err := (teamBotSettingsPort{f.a}).ReadCatalog(t.Context(), binding, owner)
	if err != nil || observed.Snapshot == nil {
		t.Fatalf("last good unavailable without worker: %+v %v", observed, err)
	}
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	launches := 0
	host, err := workhost.New(f.a.teamRuntime.store, f.a.teamRuntime.service, signer, workhost.Config{Paths: f.a.headlessRuntime.Paths, BinaryPath: "fake-wrapper", HostID: "host", ToolEndpoint: "http://127.0.0.1:9090", Settings: teamSettingsResolver{app: f.a, store: f.a.teamRuntime.store, hostID: "host"}, Registry: f.coordinator, Runtime: teamSettingsTestRuntime{}, Start: func(relayruntime.HeadlessLaunchOptions) (int, error) { launches++; return 1234, nil }})
	if err != nil {
		t.Fatal(err)
	}
	f.a.teamRuntime.host = host
	snapshot, err := (teamBotSettingsPort{f.a}).Snapshot(t.Context(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.a.refreshTeamModelCatalog(t.Context(), f.source, binding, teamSettingsState{ContextID: owner.ContextID, Fingerprint: snapshot.Fingerprint}); err != nil {
		t.Fatal(err)
	}
	if launches != 1 {
		t.Fatalf("explicit refresh reused cached page without preparing carrier: launches=%d", launches)
	}
	pickerTx(t, f.a, func(tx *workstore.Tx) error {
		worker, e := tx.CurrentContextWorker(owner.ContextID)
		if e != nil {
			return e
		}
		if worker.ID == f.carrier.Owner.WorkerSessionID {
			t.Fatal("reused retired worker")
		}
		inputs, e := tx.ListInbox(f.source.ScopeID)
		if len(inputs) != 0 {
			t.Fatal("refresh invented business input")
		}
		return e
	})
}
