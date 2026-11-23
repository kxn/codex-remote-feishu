package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPredictionCancelDispatchesToOriginalInstanceAfterDetach(t *testing.T) {
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	app.service.UpsertInstance(&state.InstanceRecord{InstanceID: "original", Online: true, Backend: agentproto.BackendCodex})
	var sent int
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "original" || command.Kind != agentproto.CommandThreadPredictionCancel {
			t.Fatalf("cancel went to new/empty target: %s %#v", instanceID, command)
		}
		sent++
		return nil
	}
	app.handleUIEvents(context.Background(), []eventcontract.Event{{Kind: eventcontract.KindAgentCommand, SurfaceSessionID: "surface-1",
		Command: &agentproto.Command{CommandID: "prediction-cancel:prediction:1", Kind: agentproto.CommandThreadPredictionCancel,
			Target: agentproto.Target{ThreadID: "thread-1"}, Prediction: agentproto.PredictionCommand{InstanceID: "original", RequestCommandID: "prediction:1"}}}})
	if sent != 1 {
		t.Fatalf("detached cancellation sent %d times", sent)
	}
	if len(app.service.PendingRemoteTurns()) != 0 {
		t.Fatal("prediction cancel must not create a pending remote turn")
	}
}
