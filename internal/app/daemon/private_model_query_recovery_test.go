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
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateModelQueryInterruptedReceiptRecovers(t *testing.T) {
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
			return catalogProbeResult(options, "recovered-model"), nil
		case <-ctx.Done():
			return codexprofile.ModelCatalogProbeObservation{}, ctx.Err()
		}
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "recover-query", MessageID: "recover-query", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model refresh"}`, ReceivedAt: time.Now()}
	if err = r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err = r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe not started")
	}
	o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
	var id string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		cp, e := r.journal.LoadCheckpoint(t.Context(), o, 1)
		if e == nil && cp.Core.UI != nil && cp.Core.UI.ModelWait != nil && cp.Core.UI.ModelWait.ObservationHash != "" {
			id = cp.Core.UI.ModelWait.QueryID
			break
		}
	}
	if id == "" {
		t.Fatal("observed query missing")
	}
	if err = r.journal.MarkEffectSending(t.Context(), o, 1, id); err != nil {
		t.Fatal(err)
	}
	if err = runPrivateModelCatalogQuery(t.Context(), a, r.processor, r.journal, o, 1, id); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("sending observation: %v", err)
	}
	if err = r.journal.ResolveEffect(t.Context(), o, 1, id, "unknown", json.RawMessage(`{"reason":"interrupted"}`), nil); err != nil {
		t.Fatal(err)
	}
	if err = runPrivateModelCatalogQuery(t.Context(), a, r.processor, r.journal, o, 1, id); !errors.Is(err, privatechat.ErrDeferred) {
		t.Fatalf("unknown observation: %v", err)
	}
	if probes.Load() != 1 {
		t.Fatalf("forced duplicate probe: %d", probes.Load())
	}
	close(release)
	for deadline := time.Now().Add(7 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, op := range gateway.snapshotOperations() {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if strings.Contains(string(body), "recovered-model") {
				if probes.Load() != 1 {
					t.Fatalf("duplicate probe: %d", probes.Load())
				}
				return
			}
		}
	}
	t.Fatal("interrupted query did not update card")
}

func TestPrivateModelCatalogSDKUnavailableWithoutNamespace(t *testing.T) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	setTestWorkBotSettings(t, a, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex, CodexProfileID: "missing-profile"})
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "missing-profile", MessageID: "missing-profile", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	if err := r.startWorkers(t.Context(), make(chan error, 8)); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(7 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		for _, op := range gateway.snapshotOperations() {
			body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if op.Kind == feishu.OperationUpdateCard && strings.Contains(string(body), "profile_revision_unavailable") && strings.Contains(string(body), "刷新") {
				var doc any
				if err := json.Unmarshal(body, &doc); err != nil {
					t.Fatal(err)
				}
				value := privateModelRefreshCallback(doc)
				if value == nil {
					t.Fatal("stamped refresh callback missing")
				}
				o := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user"}
				registered := false
				for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
					effects, err := r.journal.RecoveryEffects(t.Context(), o, 1, 0, -1, 100)
					if err != nil {
						t.Fatal(err)
					}
					for _, effect := range effects {
						event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
						if e == nil && event.PageView != nil && event.PageView.PrivateModelResult && effect.State == "confirmed" {
							registered = true
							break
						}
					}
					if registered {
						break
					}
				}
				if !registered {
					t.Fatal("model update receipt not committed")
				}
				click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "retry-missing-profile", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: op.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
				if _, err := r.hook.ReceiveInteractionResponse(t.Context(), click); err != nil {
					t.Fatal(err)
				}
				effects, err := r.journal.RecoveryEffects(t.Context(), o, 1, 0, -1, 100)
				if err != nil {
					t.Fatal(err)
				}
				for _, effect := range effects {
					event, e := privatechat.DecodeEffectEvent(effect.Effect.Body)
					if e == nil && event.DaemonCommand != nil && event.DaemonCommand.Kind == "private.model_catalog.query" {
						var intent privateModelQueryIntent
						if json.Unmarshal([]byte(event.DaemonCommand.Text), &intent) == nil && intent.Force {
							return
						}
					}
				}
				t.Fatal("refresh callback did not freeze a forced query")
			}
		}
	}
	t.Fatal("missing profile error and retry were not shown on original card")
}

func privateModelRefreshCallback(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		if v["action_kind"] == "surface.command.model" && v["action_arg"] == "refresh" && v["private_view"] != nil {
			return v
		}
		for _, child := range v {
			if found := privateModelRefreshCallback(child); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range v {
			if found := privateModelRefreshCallback(child); found != nil {
				return found
			}
		}
	}
	return nil
}
