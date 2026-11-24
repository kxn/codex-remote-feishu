package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/codexprofile"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestPrivateModelCatalogSDKExplicitRefreshRetriesError(t *testing.T) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	if _, _, err := a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	prefs, err := a.profileContextPreferenceStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = prefs.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	a.runCodexModelCatalogProbe = func(_ context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		if probes.Add(1) == 1 {
			return codexprofile.ModelCatalogProbeObservation{}, errors.New("controlled catalog failure")
		}
		return catalogProbeResult(options, "after-explicit-refresh"), nil
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "first-failure", MessageID: "first-failure", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err = r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(6 * time.Second); ; {
		found := false
		for _, op := range gateway.snapshotOperations() {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if strings.Contains(string(body), "controlled catalog failure") {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first failure not shown, probes=%d", probes.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := len(gateway.snapshotOperations())
	raw.EventID, raw.MessageID, raw.RawContent = "retry-model", "retry-model", `{"text":"/model refresh"}`
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(7 * time.Second); ; {
		for _, op := range gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if strings.Contains(string(body), "after-explicit-refresh") {
				if probes.Load() != 2 {
					t.Fatalf("unexpected probe count: %d", probes.Load())
				}
				settings, e := a.readPrivateBotSettings("gateway", "app")
				if e != nil || settings.Record.PromptOverride.Model == "refresh" {
					t.Fatal("refresh was stored as model")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("explicit retry did not produce result, probes=%d", probes.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPrivateModelCatalogSDKLastGoodThenFresh(t *testing.T) {
	testPrivateModelCatalogSDKLastGood(t, false, false)
}
func TestPrivateModelCatalogSDKLastGoodPublicationDelayed(t *testing.T) {
	testPrivateModelCatalogSDKLastGood(t, true, false)
}
func TestPrivateModelCatalogSDKSuppressesStalePreparedPage(t *testing.T) {
	testPrivateModelCatalogSDKLastGood(t, true, true)
}

type delayedModelGateway struct {
	*messageIDAssigningGateway
	started, release chan struct{}
	held             atomic.Bool
}

func (g *delayedModelGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	for _, op := range ops {
		if op.Kind == feishu.OperationUpdateCard && g.held.CompareAndSwap(false, true) {
			close(g.started)
			select {
			case <-g.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return g.messageIDAssigningGateway.Apply(ctx, ops)
}

func testPrivateModelCatalogSDKLastGood(t *testing.T, delayed, leave bool) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	var blocked *delayedModelGateway
	if delayed {
		blocked = &delayedModelGateway{messageIDAssigningGateway: gateway, started: make(chan struct{}), release: make(chan struct{})}
		a.gateway = blocked
	}
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	if _, _, err := a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	prefs, err := a.profileContextPreferenceStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = prefs.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); err != nil {
		t.Fatal(err)
	}
	target, err := a.withProfileCatalog(t.Context(), profileCatalogScope{teamID("role", "gateway"), "gateway", "app", 1}, state.OAuthCodexProfileID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	old := catalogProbeResult(target.Options, "last-good-model")
	oldTime := time.Now().Add(-25 * time.Hour)
	old.Snapshot.RefreshedAt = oldTime
	if err = a.teamRuntime.store.Within(t.Context(), func(tx *workstore.Tx) error {
		if _, e := tx.BeginProfileModelCatalogRefresh(target.Key, "seed", oldTime); e != nil {
			return e
		}
		_, e := tx.CompleteProfileModelCatalogRefresh(target.Key, "seed", &old.Snapshot, old.EvidenceID, "", oldTime)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	a.runCodexModelCatalogProbe = func(ctx context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		close(started)
		select {
		case <-release:
			return catalogProbeResult(options, "fresh-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "stale-model", MessageID: "stale-model", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if leave {
		raw.RawContent = `{"text":"/menu send_settings"}`
	}
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err = r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	var leaveClick platformingress.HumanInteractionEnvelope
	if leave {
		ops := gateway.waitForOperationCount(1, 5*time.Second)
		if len(ops) == 0 || ops[0].MessageID == "" {
			t.Fatal("settings card missing")
		}
		card := ops[0]
		for deadline := time.Now().Add(5 * time.Second); ; {
			if _, e := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); e == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("settings card receipt missing")
			}
			time.Sleep(10 * time.Millisecond)
		}
		body, e := feishu.FreezePrivateOperation(card, a.daemonLifecycleID)
		if e != nil {
			t.Fatal(e)
		}
		var doc any
		if e = json.Unmarshal(body, &doc); e != nil {
			t.Fatal(e)
		}
		value := assembledActionKind(doc, "surface.command.model")
		if value == nil {
			t.Fatal("model callback missing")
		}
		leaveClick = platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "open-model-stale", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, e := r.hook.ReceiveInteractionResponse(t.Context(), leaveClick)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(response.RawBody, &doc); e != nil {
			t.Fatal(e)
		}
		leaveClick.EventID = "back-model-stale"
		leaveClick.Values = assembledMenuValue(doc, "send_settings")
		if leaveClick.Values == nil {
			t.Fatal("back callback missing")
		}
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not start")
	}
	if delayed {
		select {
		case <-blocked.started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("last-good publication did not start")
		}
		close(release)
		a.profileCatalogWorkers.Wait()
		// The newer observation is committed while the previous platform write
		// remains blocked; delivery must preserve the card's order.
		o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
		for deadline := time.Now().Add(5 * time.Second); ; {
			cp, e := r.journal.LoadCheckpoint(t.Context(), o, 1)
			if e == nil && cp.Core.UI != nil && cp.Core.UI.ModelWait != nil && cp.Core.UI.ModelWait.CatalogRevision >= 4 {
				break
			}
			if time.Now().After(deadline) {
				close(blocked.release)
				t.Fatal("fresh observation not committed while prior write blocked")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if leave {
			if _, e := r.hook.ReceiveInteractionResponse(t.Context(), leaveClick); e != nil {
				close(blocked.release)
				t.Fatal(e)
			}
		}
		close(blocked.release)
		if leave {
			for deadline := time.Now().Add(6 * time.Second); ; {
				effects, e := r.journal.RecoveryEffects(t.Context(), o, 1, 0, -1, 100)
				if e != nil {
					t.Fatal(e)
				}
				for _, effect := range effects {
					var receipt privatechat.PublicationReceipt
					if json.Unmarshal(effect.Receipt, &receipt) == nil && receipt.Kind == "suppressed" && receipt.Reason == "stale_model_view" {
						pub, e := r.journal.LoadPublication(t.Context(), o, 1, effect.Effect.ID)
						if e != nil {
							t.Fatal(e)
						}
						for _, op := range pub.Operations {
							if op.State != "prepared" {
								t.Fatalf("suppressed operation sent: %s", op.State)
							}
						}
						return
					}
				}
				if time.Now().After(deadline) {
					t.Fatal("stale model publication was not durably suppressed")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
	var cardID string
	for deadline := time.Now().Add(5 * time.Second); ; {
		for _, op := range gateway.snapshotOperations() {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if strings.Contains(string(body), "last-good-model") {
				cardID = op.MessageID
				break
			}
		}
		if cardID != "" {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("last-good model unavailable while refreshing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !delayed {
		close(release)
	}
	for deadline := time.Now().Add(6 * time.Second); ; {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != cardID {
				continue
			}
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if strings.Contains(string(body), "fresh-model") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("fresh result did not replace last-good on same card")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The platform and catalog response are controlled, but admission, owner,
// query effect, durable cache and card publication are production paths.
func TestPrivateModelCatalogSDKColdAndCached(t *testing.T) {
	testPrivateModelCatalogSDK(t, false)
}

func TestPrivateModelCatalogSDKRestartWhileWaiting(t *testing.T) {
	old, gateway, lock := newApplicationRuntimeTestAppWithLock(t)
	r := old.applications
	setTestWorkBotSettings(t, old, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	if _, _, err := old.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	old.codexOAuthProfileState.probeCompleted = true
	prefs, err := old.profileContextPreferenceStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = prefs.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	old.runCodexModelCatalogProbe = func(ctx context.Context, _ codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		close(started)
		<-ctx.Done()
		return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "restart-model", MessageID: "restart-model", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err = r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("initial probe missing")
	}
	ops := gateway.waitForOperationCount(1, 5*time.Second)
	var card feishu.Operation
	for _, op := range ops {
		if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
			card = op
			break
		}
	}
	if card.MessageID == "" {
		t.Fatal("model waiting card missing")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, e := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiting card receipt missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err = r.stopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if err = old.stopProfileCatalogWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if err = r.drainNativeDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	if err = r.close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err := captureTeamLegacyFiles(old.headlessRuntime.Paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	a := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = a.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	a.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Error("unexpected native launch")
		return 0, context.Canceled
	}
	a.SetHeadlessRuntime(old.headlessRuntime)
	a.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if err = a.configureWorkRuntime(ctx, teamPrivateCommands{app: a}, a.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	if err = a.activateWorkRuntime(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = a.configureApplicationIngress(ctx, feishu.NewMultiGatewayController(), lock); err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	var probes atomic.Int32
	a.runCodexModelCatalogProbe = func(_ context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		probes.Add(1)
		return catalogProbeResult(options, "restart-catalog-model"), nil
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = a.applications.stopWorkers(c)
		_ = a.stopProfileCatalogWorkers(c)
		_ = a.applications.drainNativeDeliveries(c)
		_ = a.applications.close()
		_ = a.teamRuntime.store.Close()
	})
	before := len(gateway.snapshotOperations())
	if err = a.applications.startWorkers(ctx, make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(7 * time.Second); ; {
		for _, op := range gateway.snapshotOperations()[before:] {
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == card.MessageID {
				body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
				if strings.Contains(string(body), "restart-catalog-model") {
					if probes.Load() != 1 {
						t.Fatalf("restart probes=%d", probes.Load())
					}
					goto reopenCached
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting card did not resume after restart; probes=%d", probes.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
reopenCached:
	if err = a.applications.stopWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if err = a.stopProfileCatalogWorkers(ctx); err != nil {
		t.Fatal(err)
	}
	if err = a.applications.drainNativeDeliveries(ctx); err != nil {
		t.Fatal(err)
	}
	if err = a.applications.close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = captureTeamLegacyFiles(old.headlessRuntime.Paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	next := New("127.0.0.1:0", "127.0.0.1:0", gateway, agentproto.ServerIdentity{})
	if err = next.prepareApplicationCore(); err != nil {
		t.Fatal(err)
	}
	next.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		t.Error("unexpected native launch")
		return 0, context.Canceled
	}
	next.SetHeadlessRuntime(old.headlessRuntime)
	next.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: old.headlessRuntime.Paths.ConfigFile, Services: defaultFeishuServices()})
	if err = next.configureWorkRuntime(ctx, teamPrivateCommands{app: next}, next.authorizeTeamControl); err != nil {
		t.Fatal(err)
	}
	if err = next.activateWorkRuntime(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = next.configureApplicationIngress(ctx, feishu.NewMultiGatewayController(), lock); err != nil {
		t.Fatal(err)
	}
	next.codexOAuthProfileState.probeCompleted = true
	var repeated atomic.Int32
	next.runCodexModelCatalogProbe = func(_ context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		repeated.Add(1)
		return catalogProbeResult(options, "unexpected-probe"), nil
	}
	t.Cleanup(func() {
		c, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = next.applications.stopWorkers(c)
		_ = next.stopProfileCatalogWorkers(c)
		_ = next.applications.drainNativeDeliveries(c)
		_ = next.applications.close()
		_ = next.teamRuntime.store.Close()
	})
	before = len(gateway.snapshotOperations())
	raw.EventID, raw.MessageID = "reopened-cached-model", "reopened-cached-model"
	if err = next.applications.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if err = next.applications.startWorkers(ctx, make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(6 * time.Second); ; {
		for _, op := range gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, next.daemonLifecycleID)
			if strings.Contains(string(body), "restart-catalog-model") {
				if repeated.Load() != 0 {
					t.Fatal("cached page unnecessarily reprobed")
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("cached result missing after reopen; probes=%d", repeated.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPrivateModelCatalogSDKNavigationDuringProbe(t *testing.T) {
	testPrivateModelCatalogSDK(t, true)
}

func testPrivateModelCatalogSDK(t *testing.T, leave bool) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: state.OAuthCodexProfileID})
	if _, _, err := a.codexOAuthProfileState.store.ApplyProbe(catalogOAuthObservation("fixture@example.com", "plus"), time.Now(), "fixture", false); err != nil {
		t.Fatal(err)
	}
	a.codexOAuthProfileState.probeCompleted = true
	prefs, err := a.profileContextPreferenceStore()
	if err != nil {
		t.Fatal(err)
	}
	if err = prefs.EnsureCodexProfile(state.OAuthCodexProfileID, state.CodexContextModeDefault); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var probes atomic.Int32
	a.runCodexModelCatalogProbe = func(ctx context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		if probes.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return catalogProbeResult(options, "shared-catalog-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "model-cold", MessageID: "model-cold", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if leave {
		raw.RawContent = `{"text":"/menu send_settings"}`
	}
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err = r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	ops := gateway.waitForOperationCount(1, 5*time.Second)
	var card feishu.Operation
	for _, op := range ops {
		if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
			card = op
			break
		}
	}
	if card.MessageID == "" {
		t.Fatal("model page was not published")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
			cp, e := r.journal.LoadCheckpoint(t.Context(), o, 1)
			if e == nil && cp.Core.UI != nil {
				t.Logf("model wait: %+v", cp.Core.UI.ModelWait)
			}
			effects, e := r.journal.RecoveryEffects(t.Context(), o, 1, 0, 0, 100)
			if e == nil {
				for _, effect := range effects {
					t.Logf("effect %s lane=%s state=%s revision=%d", effect.Effect.ID, effect.Effect.Lane, effect.State, effect.Revision)
				}
			}
			t.Fatal("model card receipt was not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if leave {
		body, e := feishu.FreezePrivateOperation(card, a.daemonLifecycleID)
		if e != nil {
			t.Fatal(e)
		}
		var doc any
		if e = json.Unmarshal(body, &doc); e != nil {
			t.Fatal(e)
		}
		value := assembledActionKind(doc, "surface.command.model")
		if value == nil {
			t.Fatal("model button missing")
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "open-model", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, e := r.hook.ReceiveInteractionResponse(t.Context(), click)
		if e != nil {
			t.Fatal(e)
		}
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("model callback did not request probe")
		}
		if e = json.Unmarshal(response.RawBody, &doc); e != nil {
			t.Fatal(e)
		}
		back := assembledMenuValue(doc, "send_settings")
		if back == nil {
			close(release)
			t.Fatalf("model callback missing back: %s", response.RawBody)
		}
		click.EventID = "leave-model"
		click.Values = back
		response, e = r.hook.ReceiveInteractionResponse(t.Context(), click)
		if e != nil || len(response.RawBody) == 0 {
			t.Fatalf("model back: %v", e)
		}
		before := len(gateway.snapshotOperations())
		close(release)
		a.profileCatalogWorkers.Wait()
		// The query worker polls at one second. Give it two full turns to
		// observe the result and attempt an obsolete-card projection.
		time.Sleep(2200 * time.Millisecond)
		for _, op := range gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == card.MessageID && strings.Contains(string(body), "shared-catalog-model") {
				t.Fatal("late model result replaced a page the user left")
			}
		}
		return
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("cold model page did not request shared probe")
	}
	close(release)
	for deadline := time.Now().Add(6 * time.Second); ; {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind != feishu.OperationUpdateCard || op.MessageID != card.MessageID {
				continue
			}
			body, e := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if e == nil && strings.Contains(string(body), "shared-catalog-model") {
				goto cached
			}
		}
		if time.Now().After(deadline) {
			o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
			cp, e := r.journal.LoadCheckpoint(t.Context(), o, 1)
			if e == nil && cp.Core.UI != nil {
				t.Logf("model wait: %+v", cp.Core.UI.ModelWait)
			}
			effects, e := r.journal.RecoveryEffects(t.Context(), o, 1, 0, 0, 100)
			if e == nil {
				for _, effect := range effects {
					t.Logf("effect %s lane=%s state=%s revision=%d", effect.Effect.ID, effect.Effect.Lane, effect.State, effect.Revision)
				}
			}
			for _, op := range gateway.snapshotOperations() {
				body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
				t.Logf("operation %s %s %s", op.Kind, op.MessageID, body)
			}
			t.Fatal("cold result did not update original card")
		}
		time.Sleep(10 * time.Millisecond)
	}
cached:
	before := len(gateway.snapshotOperations())
	raw.EventID, raw.MessageID = "model-cached", "model-cached"
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		ops = gateway.snapshotOperations()
		for _, op := range ops[before:] {
			body, e := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if e == nil && strings.Contains(string(body), "shared-catalog-model") {
				if probes.Load() != 1 {
					t.Fatalf("cold query sent %d probes", probes.Load())
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("cached model missing from next page")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
