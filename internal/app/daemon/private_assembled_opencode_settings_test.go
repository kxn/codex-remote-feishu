package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
)

// exerciseAssembledSDKOpenCodeReasoning uses the observed ACP session options
// from the controlled wrapper, including the actual SDK callback and store.
func exerciseAssembledSDKOpenCodeReasoning(t *testing.T, ctx context.Context, app *App, gateway *messageIDAssigningGateway) {
	t.Helper()
	r := app.applications
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "user"), ChatID: "private-chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	for deadline := time.Now().Add(5 * time.Second); ; {
		ready := false
		status, err := r.processor.ReadRuntimeStatus(ctx)
		if err == nil {
			for _, inst := range status.Instances {
				thread := inst.Threads["session"]
				if thread != nil && thread.ThreadSettings != nil && thread.ThreadSettings.Model == "fixture/model" && inst.ModelCatalog != nil && len(inst.ModelCatalog.Entries) != 0 {
					ready = true
					break
				}
			}
		}
		checkpoint, checkpointErr := r.journal.LoadCheckpoint(ctx, owner, 1)
		ready = ready && checkpointErr == nil && checkpoint.Core.SelectedThreadID == "session"
		if ready {
			break
		}
		if time.Now().After(deadline) {
			body, _ := json.Marshal(status.Instances)
			t.Fatalf("OpenCode native settings did not reach core: %s %v", body, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := len(gateway.snapshotOperations())
	raw := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", SenderTenant: "tenant", EventID: "opencode-reasoning-open", MessageID: "opencode-reasoning-open", ChatID: "private-chat", ChatType: "p2p", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, MessageType: "text", RawContent: `{"text":"/reasoning"}`, ReceivedAt: time.Now()}
	if err := r.hook.Receive(ctx, raw); err != nil {
		t.Fatal(err)
	}
	var card feishu.Operation
	var value map[string]any
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		ops := gateway.snapshotOperations()
		for _, op := range ops[before:] {
			if op.MessageID == "" {
				continue
			}
			body, err := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			if err != nil {
				continue
			}
			var doc any
			if json.Unmarshal(body, &doc) != nil {
				continue
			}
			if found := findPrivateSettingsAction(doc, "surface.command.reasoning", "high"); found != nil {
				card, value = op, found
			}
		}
		if value != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if value == nil {
		for _, op := range gateway.snapshotOperations()[before:] {
			body, _ := feishu.FreezePrivateOperation(op, app.daemonLifecycleID)
			t.Logf("reasoning operation %s: %s", op.Kind, body)
		}
		t.Fatal("OpenCode observed high effort option missing")
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, err := r.router.PublishedMessage(ctx, "tenant", "app", "private-chat", card.MessageID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("OpenCode reasoning card receipt missing")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before = len(gateway.snapshotOperations())
	click := platformingress.HumanInteractionEnvelope{GatewayRef: "gateway", AppID: "app", Tenant: "tenant", EventID: "opencode-reasoning-high", SenderIDs: platformingress.SenderIDs{OpenID: "user"}, ChatID: "private-chat", MessageID: card.MessageID, Kind: "card", Values: value, ReceivedAt: time.Now()}
	if _, err := r.hook.ReceiveInteractionResponse(ctx, click); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		snapshot, err := app.readPrivateBotSettings("gateway", "app")
		if err == nil && snapshot.Record.PromptOverride.ReasoningEffort == "high" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("OpenCode reasoning not saved: %#v %v", snapshot.Record, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for deadline := time.Now().Add(5 * time.Second); ; {
		updated := false
		for _, op := range gateway.snapshotOperations()[before:] {
			if op.Kind == feishu.OperationUpdateCard && op.MessageID == card.MessageID {
				updated = true
				break
			}
		}
		if updated {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("OpenCode reasoning did not update its card")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
