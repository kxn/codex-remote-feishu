package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

func findPrivateModelSubmit(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "page_submit" && x["action_kind"] == "surface.command.model" && x["private_view"] != nil && x["field_name"] == "command_args_model_preset" {
			return x
		}
		for _, child := range x {
			if found := findPrivateModelSubmit(child); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range x {
			if found := findPrivateModelSubmit(child); found != nil {
				return found
			}
		}
	}
	return nil
}

// exerciseAssembledSDKModelChoice uses the already-running controlled native
// catalog and genuine private SDK/card transport from the execution journey.
func exerciseAssembledSDKModelChoice(t *testing.T, ctx context.Context, app *App, gateway *messageIDAssigningGateway, before int) {
	t.Helper()
	r := app.applications
	var card feishu.Operation
	var value map[string]any
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		ops := gateway.snapshotOperations()
		for _, op := range ops[before:] {
			if op.MessageID == "" {
				continue
			}
			body, err := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if err != nil || !strings.Contains(string(body), "Fixture Model") {
				continue
			}
			var document any
			if json.Unmarshal(body, &document) != nil {
				continue
			}
			if found := findPrivateModelSubmit(document); found != nil {
				// Apply records the physical PATCH before the publisher commits its
				// receipt and installs the new callback view. An earlier message
				// receipt alone does not authorize this particular card version.
				message, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", op.MessageID)
				if token, ok := found["private_view"].(string); err == nil && ok && message.CardRef == token {
					card, value = op, found
				}
			}
		}
		if value != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if value == nil {
		t.Fatal("controlled native model selector missing")
	}
	beforeSubmit := len(gateway.snapshotOperations())
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "fixture-model-select", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, Form: map[string]any{"command_args_model_preset": "fixture-model"}, ReceivedAt: time.Now()}
	if _, err := r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		snapshot, err := app.readPrivateBotSettings("gateway", "app")
		if err == nil && snapshot.Record.PromptOverride.Model == "fixture-model" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("model not persisted: %#v %v", snapshot.Record, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		found := false
		for _, op := range gateway.snapshotOperations()[beforeSubmit:] {
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == card.MessageID {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("model setting did not update the original card")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
