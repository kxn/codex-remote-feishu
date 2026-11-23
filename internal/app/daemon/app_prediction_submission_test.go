package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func predictionSubmissionFixture(t *testing.T) (*App, *recordingGateway, control.Action) {
	t.Helper()
	app, gateway, _, action := predictionCallbackFixture(t)
	app.HandleGatewayAction(context.Background(), action)
	gateway.operations = nil
	action.Kind = control.ActionPredictionSend
	action.Text = "用户编辑后的正文\n保留第二行及部署边界。"
	return app, gateway, action
}

func lastPredictionCardUpdate(t *testing.T, gateway *recordingGateway, messageID string) feishu.Operation {
	t.Helper()
	for i := len(gateway.operations) - 1; i >= 0; i-- {
		op := gateway.operations[i]
		if op.Kind == feishu.OperationUpdateCard && op.MessageID == messageID {
			return op
		}
	}
	t.Fatal("original prediction card was not updated")
	return feishu.Operation{}
}

func requirePredictionReceipt(t *testing.T, card feishu.Operation, text, status string) {
	t.Helper()
	if operationHasActionValue(card, "page_local_submit", "action_kind", string(control.ActionPredictionSend)) {
		t.Fatal("submitted card still offers send")
	}
	body := fmt.Sprint(card.CardElements)
	if strings.Contains(body, "prediction_text") || strings.Contains(body, "tag:form") {
		t.Fatal("submitted card still contains the editor")
	}
	if !strings.Contains(body, text) || !strings.Contains(body, status) {
		t.Fatalf("receipt lost edited text or status %q: %s", status, body)
	}
}

func TestPredictionSubmissionSealsOriginalCardBeforeDispatch(t *testing.T) {
	app, gateway, action := predictionSubmissionFixture(t)
	sends := 0
	var receiptBeforeDispatch *feishu.Operation
	var sentCommand agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind == agentproto.CommandPromptSend {
			sends++
			sentCommand = command
			for _, op := range gateway.operations {
				if op.Kind == feishu.OperationUpdateCard && op.MessageID == action.MessageID {
					copy := op
					receiptBeforeDispatch = &copy
				}
			}
		}
		return nil
	}
	if result := app.HandleGatewayAction(context.Background(), action); result != nil && result.ReplaceCurrentCard != nil {
		t.Fatal("callback replacement could overwrite a later failure recovery")
	}
	app.HandleGatewayAction(context.Background(), action)
	if sends != 1 {
		t.Fatalf("duplicate click dispatched %d prompts, want 1", sends)
	}
	if receiptBeforeDispatch == nil {
		t.Fatal("original prediction card was not sealed before dispatch")
	}
	requirePredictionReceipt(t, *receiptBeforeDispatch, action.Text, "已提交")
	if sentCommand.Target.ThreadID != "thread-1" || sentCommand.Prompt.Inputs[0].Text != action.Text {
		t.Fatal("receipt changed prompt target or content")
	}
}

func TestPredictionQueuedSubmissionSealsCardWithoutStartingTask(t *testing.T) {
	app, gateway, action := predictionSubmissionFixture(t)
	surface := app.service.Surface(action.SurfaceSessionID)
	surface.DispatchMode = state.DispatchModePausedForLocal
	sends := 0
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind == agentproto.CommandPromptSend {
			sends++
		}
		return nil
	}
	app.HandleGatewayAction(context.Background(), action)
	app.HandleGatewayAction(context.Background(), action)
	requirePredictionReceipt(t, lastPredictionCardUpdate(t, gateway, action.MessageID), action.Text, "已提交")
	if sends != 0 || len(surface.QueuedQueueItemIDs) != 1 {
		t.Fatalf("queued submission dispatched %d prompts, queued %d items", sends, len(surface.QueuedQueueItemIDs))
	}
}

func TestPredictionDispatchFailureRestoresEditorAfterReceipt(t *testing.T) {
	app, gateway, action := predictionSubmissionFixture(t)
	sends := 0
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind == agentproto.CommandPromptSend {
			sends++
			if sends == 1 {
				return errors.New("relay unavailable before dispatch")
			}
		}
		return nil
	}
	result := app.HandleGatewayAction(context.Background(), action)
	if result != nil && result.ReplaceCurrentCard != nil {
		t.Fatal("callback must not overwrite the recovered draft")
	}
	card := lastPredictionCardUpdate(t, gateway, action.MessageID)
	if !operationHasActionValue(card, "page_local_submit", "action_kind", string(control.ActionPredictionSend)) || predictionInputDefault(t, card) != action.Text {
		t.Fatal("dispatch failure did not restore the original edited draft")
	}
	if sends != 1 {
		t.Fatal("dispatch failure automatically retried the prompt")
	}
	app.HandleGatewayAction(context.Background(), action)
	if sends != 2 {
		t.Fatal("explicit retry did not dispatch once")
	}
	requirePredictionReceipt(t, lastPredictionCardUpdate(t, gateway, action.MessageID), action.Text, "已提交")
}

func TestPredictionStartedSubmissionRemainsReadOnlyAfterExecutionFailure(t *testing.T) {
	for _, activeFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(activeFirst), func(t *testing.T) {
			app, gateway, action := predictionSubmissionFixture(t)
			app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
			app.HandleGatewayAction(context.Background(), action)
			if activeFirst {
				app.handleUIEvents(context.Background(), app.service.ApplyAgentEvent("inst-1", agentproto.Event{
					Kind: agentproto.EventThreadRuntimeStatusUpdated, ThreadID: "thread-1",
					RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeActive},
				}))
			}
			initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: action.SurfaceSessionID}
			app.handleUIEvents(context.Background(), app.service.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2", Initiator: initiator,
			}))
			requirePredictionReceipt(t, lastPredictionCardUpdate(t, gateway, action.MessageID), action.Text, "已发送")
			app.handleUIEvents(context.Background(), app.service.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-2", Status: "failed", Initiator: initiator,
			}))
			requirePredictionReceipt(t, lastPredictionCardUpdate(t, gateway, action.MessageID), action.Text, "已发送")
		})
	}
}

type predictionReceiptFailureGateway struct {
	*recordingGateway
	failed bool
}

func (g *predictionReceiptFailureGateway) Apply(ctx context.Context, operations []feishu.Operation) error {
	for _, op := range operations {
		if !g.failed && op.Kind == feishu.OperationUpdateCard && op.MessageID == "om-prediction" {
			g.failed = true
			return io.ErrUnexpectedEOF
		}
	}
	return g.recordingGateway.Apply(ctx, operations)
}

func TestPredictionReceiptDeliveryFailureDoesNotRetryPrompt(t *testing.T) {
	app, gateway, action := predictionSubmissionFixture(t)
	app.gateway = &predictionReceiptFailureGateway{recordingGateway: gateway}
	sends := 0
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		if command.Kind == agentproto.CommandPromptSend {
			sends++
		}
		return nil
	}
	app.HandleGatewayAction(context.Background(), action)
	app.HandleGatewayAction(context.Background(), action)
	if sends != 1 {
		t.Fatalf("receipt failure caused %d prompt sends, want 1", sends)
	}
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: action.SurfaceSessionID}
	app.handleUIEvents(context.Background(), app.service.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-2", Initiator: initiator,
	}))
	requirePredictionReceipt(t, lastPredictionCardUpdate(t, gateway, action.MessageID), action.Text, "已发送")
}
