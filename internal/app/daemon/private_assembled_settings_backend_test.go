package daemon

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func findPrivateSettingsAction(v any, kind, arg string) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["action_kind"] == kind && x["action_arg"] == arg && x["private_view"] != nil {
			return x
		}
		for _, child := range x {
			if found := findPrivateSettingsAction(child, kind, arg); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := findPrivateSettingsAction(child, kind, arg); found != nil {
				return found
			}
		}
	}
	return nil
}

func TestPrivateClaudeReasoningCardThroughSDK(t *testing.T) {
	app, gateway := newApplicationRuntimeTestApp(t)
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude})
	r := app.applications
	errs := make(chan error, 4)
	if err := r.startWorkers(t.Context(), errs); err != nil {
		t.Fatal(err)
	}
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "claude-reasoning-open", MessageID: "claude-reasoning-message", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/reasoning"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationSendCard && op.MessageID != "" {
				card = op
				break
			}
		}
		if card.MessageID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if card.MessageID == "" {
		t.Fatal("reasoning card missing")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := r.router.PublishedMessage(t.Context(), "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reasoning card receipt missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	frozen, err := feishu.FreezePrivateOperation(card, app.daemonLifecycleID)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err = json.Unmarshal(frozen, &doc); err != nil {
		t.Fatal(err)
	}
	value := findPrivateSettingsAction(doc, "surface.command.reasoning", "high")
	if value == nil {
		t.Fatalf("high action missing: %s", frozen)
	}
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "claude-reasoning-high", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
	if _, err = r.hook.ReceiveInteractionResponse(t.Context(), click); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		snapshot, e := app.readPrivateBotSettings("gateway", "app")
		if e == nil && snapshot.Record.PromptOverride.ReasoningEffort == "high" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(fmt.Sprintf("Claude setting not saved: %#v %v", snapshot.Record, e))
		}
		time.Sleep(10 * time.Millisecond)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		updated := false
		for _, op := range gateway.snapshotOperations() {
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == card.MessageID {
				updated = true
				break
			}
		}
		if updated {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("saved setting did not update its original card")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}
