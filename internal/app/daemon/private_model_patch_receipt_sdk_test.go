package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
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
)

type visibleBeforeReceiptGateway struct {
	*messageIDAssigningGateway
	mu      sync.Mutex
	updates int
	visible chan feishu.Operation
	release chan struct{}
}

func (g *visibleBeforeReceiptGateway) Apply(ctx context.Context, ops []feishu.Operation) error {
	if err := g.messageIDAssigningGateway.Apply(ctx, ops); err != nil {
		return err
	}
	for _, op := range ops {
		if op.Kind != feishu.OperationUpdateCard {
			continue
		}
		g.mu.Lock()
		g.updates++
		hold := g.updates == 2
		g.mu.Unlock()
		if hold {
			g.visible <- op
			select {
			case <-g.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}
func TestPrivateSDKModelClickWhilePatchReceiptPending(t *testing.T) {
	testPrivateModelPatchReceiptWindow(t, false, false, false)
}
func TestPrivateSDKModelClickTimeoutThenRedelivery(t *testing.T) {
	testPrivateModelPatchReceiptWindow(t, true, false, false)
}
func TestPrivateSDKMenuModelClickWhilePatchReceiptPending(t *testing.T) {
	testPrivateModelPatchReceiptWindow(t, false, true, false)
}
func TestPrivateSDKModelClickWithoutChatWhilePatchReceiptPending(t *testing.T) {
	testPrivateModelPatchReceiptWindow(t, false, false, true)
}
func testPrivateModelPatchReceiptWindow(t *testing.T, timeout, fromMenu, emptyChat bool) {
	a, gateway := newApplicationRuntimeTestApp(t)
	r := a.applications
	g := &visibleBeforeReceiptGateway{messageIDAssigningGateway: gateway, visible: make(chan feishu.Operation, 1), release: make(chan struct{})}
	a.gateway = g
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(g.release) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
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
	target, err := a.withProfileCatalog(ctx, profileCatalogScope{teamID("role", "gateway"), "gateway", "app", 1}, state.OAuthCodexProfileID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	old := catalogProbeResult(target.Options, "old-model")
	oldTime := time.Now().Add(-25 * time.Hour)
	old.Snapshot.RefreshedAt = oldTime
	if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, e := tx.BeginProfileModelCatalogRefresh(target.Key, "seed", oldTime); e != nil {
			return e
		}
		_, e := tx.CompleteProfileModelCatalogRefresh(target.Key, "seed", &old.Snapshot, old.EvidenceID, "", oldTime)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	probeStarted, probeRelease := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-probeRelease:
		default:
			close(probeRelease)
		}
	}()
	a.runCodexModelCatalogProbe = func(c context.Context, options codexprofile.ModelCatalogProbeOptions) (codexprofile.ModelCatalogProbeObservation, error) {
		close(probeStarted)
		select {
		case <-probeRelease:
			return catalogProbeResult(options, "fresh-model"), nil
		case <-c.Done():
			return codexprofile.ModelCatalogProbeObservation{}, c.Err()
		}
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "model-open", MessageID: "model-open", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/model"}`, ReceivedAt: time.Now()}
	if fromMenu {
		raw.RawContent = `{"text":"/menu send_settings"}`
	}
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	if err = r.startWorkers(ctx, errs); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = r.stopWorkers(stop)
	}()
	if fromMenu {
		ops := gateway.waitForOperationCount(1, 5*time.Second)
		if len(ops) == 0 || ops[0].MessageID == "" {
			t.Fatal("menu card missing")
		}
		card := ops[0]
		for {
			if _, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); e == nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("menu receipt missing")
			case <-time.After(15 * time.Millisecond):
			}
		}
		f, e := feishu.FreezePrivateOperation(card, a.daemonLifecycleID)
		if e != nil {
			t.Fatal(e)
		}
		var menu any
		if e = json.Unmarshal(f, &menu); e != nil {
			t.Fatal(e)
		}
		value := assembledActionKind(menu, "surface.command.model")
		if value == nil {
			t.Fatal("menu model action missing")
		}
		click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "open-model-from-menu", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatal(e)
		}
		if len(response.RawBody) == 0 {
			t.Fatal("model loading response missing")
		}
	}
	var oldCard feishu.Operation
	var oldValue map[string]any
	for oldValue == nil {
		for _, op := range gateway.snapshotOperations() {
			body, e := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if e != nil || !strings.Contains(string(body), "old-model") {
				continue
			}
			var doc any
			if json.Unmarshal(body, &doc) == nil {
				v := findPrivateModelSubmit(doc)
				if v != nil {
					if message, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", op.MessageID); e == nil && message.CardRef == v["private_view"] {
						oldCard, oldValue = op, v
					}
				}
			}
		}
		if oldValue != nil {
			break
		}
		select {
		case e := <-errs:
			t.Fatal(e)
		case <-ctx.Done():
			t.Fatal("old model page not confirmed")
		case <-time.After(15 * time.Millisecond):
		}
	}
	select {
	case <-probeStarted:
	case <-ctx.Done():
		t.Fatal("catalog probe did not start")
	}
	close(probeRelease)
	var fresh feishu.Operation
	select {
	case fresh = <-g.visible:
	case <-ctx.Done():
		t.Fatal("fresh model patch not physically applied")
	}
	if fresh.MessageID != oldCard.MessageID {
		t.Fatal("model patch changed card")
	}
	body, err := feishu.FreezePrivateOperation(fresh, a.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	value := findPrivateModelSubmit(doc)
	if value == nil || value["private_view"] == oldValue["private_view"] {
		t.Fatal("missing distinct fresh model token")
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "fresh-click", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: fresh.MessageID, Kind: "card", Values: value, Form: map[string]any{"command_args_model_preset": "fresh-model"}, ReceivedAt: time.Now()}
	if emptyChat {
		click.ChatID = ""
	}
	oldClick := click
	oldClick.EventID, oldClick.Values = "old-token-click", oldValue
	oldClick.Form = map[string]any{"command_args_model_preset": "old-model"}
	foreign := click
	foreign.EventID = "foreign-click"
	foreign.SenderIDs = platformingress.SenderIDs{OpenID: "other-user"}
	foreign.Form = map[string]any{"command_args_model_preset": "old-model"}
	foreignResponse, foreignErr := r.hook.ReceiveInteractionResponse(ctx, foreign)
	if foreignErr == nil && len(foreignResponse.RawBody) == 0 {
		t.Fatal("foreign click returned no rejection")
	}
	if current, e := a.readPrivateBotSettings("gateway", "app"); e != nil || current.Record.PromptOverride.Model == "old-model" || current.Record.PromptOverride.Model == "fresh-model" {
		t.Fatalf("stale or foreign click changed settings: %+v %v", current.Record, e)
	}
	type result struct {
		body []byte
		err  error
	}
	before := len(gateway.snapshotOperations())
	var appliedResponse []byte
	done := make(chan result, 1)
	go func() {
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		done <- result{response.RawBody, e}
	}()
	if timeout {
		select {
		case out := <-done:
			if out.err != nil || !strings.Contains(string(out.body), "未执行") {
				t.Fatalf("timeout result=%s error=%v", out.body, out.err)
			}
		case <-ctx.Done():
			t.Fatal("click timeout not returned")
		}
		settings, e := a.readPrivateBotSettings("gateway", "app")
		if e != nil || settings.Record.PromptOverride.Model == "fresh-model" {
			t.Fatalf("timed-out click executed: %+v %v", settings.Record, e)
		}
		releaseOnce.Do(func() { close(g.release) })
		for {
			message, e := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", fresh.MessageID)
			if e == nil && message.CardRef == value["private_view"] {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("receipt not registered")
			case <-time.After(15 * time.Millisecond):
			}
		}
		if settings, e := a.readPrivateBotSettings("gateway", "app"); e != nil || settings.Record.PromptOverride.Model == "fresh-model" {
			t.Fatalf("timed-out click executed after receipt without retry: %+v %v", settings.Record, e)
		}
		response, e := r.hook.ReceiveInteractionResponse(ctx, click)
		if e != nil {
			t.Fatalf("same event retry: %v response=%s", e, response.RawBody)
		}
		appliedResponse = response.RawBody
	} else {
		select {
		case out := <-done:
			t.Fatalf("click completed before platform receipt: %s %v", out.body, out.err)
		case <-time.After(100 * time.Millisecond):
		}
		releaseOnce.Do(func() { close(g.release) })
		select {
		case out := <-done:
			if out.err != nil {
				t.Fatal(out.err)
			}
			appliedResponse = out.body
		case <-ctx.Done():
			t.Fatal("click did not complete after receipt")
		}
	}
	for {
		settings, e := a.readPrivateBotSettings("gateway", "app")
		if e == nil && settings.Record.PromptOverride.Model == "fresh-model" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("model setting not committed: %+v %v", settings.Record, e)
		case <-time.After(15 * time.Millisecond):
		}
	}
	oldResponse, oldErr := r.hook.ReceiveInteractionResponse(ctx, oldClick)
	if oldErr == nil && !strings.Contains(string(oldResponse.RawBody), "过期") {
		t.Fatalf("superseded token was not rejected: %s", oldResponse.RawBody)
	}
	terminal := false
	for !terminal {
		if strings.Contains(string(appliedResponse), "已更新") && strings.Contains(string(appliedResponse), "fresh-model") {
			terminal = true
		}
		ops := gateway.snapshotOperations()
		for _, op := range ops[before:] {
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == fresh.MessageID {
				body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
				if strings.Contains(string(body), "已更新") && strings.Contains(string(body), "fresh-model") {
					terminal = true
				}
			}
		}
		if terminal {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("confirmed original card not updated: response=%s ops=%d", appliedResponse, len(ops)-before)
		case <-time.After(15 * time.Millisecond):
		}
	}
	beforeNext := len(gateway.snapshotOperations())
	raw.EventID, raw.MessageID, raw.RawContent, raw.ReceivedAt = "reopen-model", "reopen-model", `{"text":"/model"}`, time.Now()
	if err = r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	owner := privatechat.Owner{GatewayID: "gateway", AppID: "app", TenantID: "tenant", ChatID: "private-chat", UserID: "user", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user")}
	binding, err := r.ingress.privatePublicationBindingFor(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	for {
		ops := gateway.snapshotOperations()
		for _, op := range ops[beforeNext:] {
			if (op.Kind != feishu.OperationSendCard && op.Kind != feishu.OperationUpdateCard) || op.MessageID == "" || op.MessageID == fresh.MessageID {
				continue
			}
			body, e := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
			if e != nil {
				continue
			}
			var doc any
			if json.Unmarshal(body, &doc) != nil {
				continue
			}
			nextValue := findPrivateModelSubmit(doc)
			if nextValue == nil {
				continue
			}
			token, ok := nextValue["private_view"].(string)
			if !ok {
				continue
			}
			known, current, e := r.router.CurrentPrivateView(ctx, binding.Owner, "tenant", "app", "private-chat", op.MessageID, token, "")
			if e != nil || !known || !current {
				continue
			}
			next := click
			next.EventID, next.MessageID, next.Values, next.ReceivedAt = "next-model-choice", op.MessageID, nextValue, time.Now()
			response, e := r.hook.ReceiveInteractionResponse(ctx, next)
			if e != nil || strings.Contains(string(response.RawBody), "过期") {
				t.Fatalf("reopened card not selectable: %s %v", response.RawBody, e)
			}
			return
		}
		select {
		case <-ctx.Done():
			for _, op := range gateway.snapshotOperations()[beforeNext:] {
				body, _ := feishu.FreezePrivateOperation(op, a.daemonLifecycleID)
				t.Logf("reopened op=%s message=%s body=%s", op.Kind, op.MessageID, body)
			}
			t.Fatal("reopened model card not selectable")
		case <-time.After(15 * time.Millisecond):
		}
	}
}
