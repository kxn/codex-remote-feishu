package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func predictionCallbackFixture(t *testing.T) (*App, *recordingGateway, *state.InstanceRecord, control.Action) {
	t.Helper()
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	svc := app.service
	const sid = "feishu:app-1:user:user-1"
	root := evalSymlinkForTest(t, t.TempDir())
	svc.MaterializeSurfaceResume(sid, "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "", state.SurfaceVerbosityNormal, "")
	inst := &state.InstanceRecord{InstanceID: "inst-1", Online: true, Backend: agentproto.BackendCodex, Source: "headless", Managed: true, WorkspaceRoot: root,
		CapabilitiesDeclared: true, Capabilities: agentproto.Capabilities{ThreadPrediction: true},
		Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: root, Loaded: true}}}
	svc.UpsertInstance(inst)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1", Text: "检查实现"})
	initiator := agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: sid}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1", Initiator: initiator})
	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "thread-1", TurnID: "turn-1", Status: "completed", Initiator: initiator})
	var predictionID string
	for _, event := range completed {
		if event.Command != nil && event.Command.Kind == agentproto.CommandThreadPredictionRequest {
			predictionID = event.Command.CommandID
		}
	}
	if predictionID == "" {
		t.Fatal("missing prediction request")
	}
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventThreadPredictionResult, CommandID: predictionID, ThreadID: "thread-1", Prediction: &agentproto.PredictionResult{SourceTurnID: "turn-1", Suggestion: "请补充边界测试"}})
	return app, gateway, inst, control.Action{
		Kind: control.ActionPredictionAccept, GatewayID: "app-1", SurfaceSessionID: sid, ChatID: "chat-1", ActorUserID: "user-1",
		MessageID: "om-prediction", PredictionID: predictionID, LocalPageAction: true,
		Inbound: &control.ActionInboundMeta{CardDaemonLifecycleID: app.daemonLifecycleID},
	}
}

func TestPredictionAcceptReplacesCallbackCardWithoutAsyncPatch(t *testing.T) {
	app, gateway, _, action := predictionCallbackFixture(t)
	result := app.HandleGatewayAction(context.Background(), action)
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("adoption must return the edit form in the synchronous callback, got %#v; async operations=%d", result, len(gateway.operations))
	}
	card := result.ReplaceCurrentCard
	if card.Kind != feishu.OperationSendCard || card.CardTitle != "编辑下一条消息" {
		t.Fatalf("wrong callback replacement: kind=%s title=%s", card.Kind, card.CardTitle)
	}
	if !operationHasActionValue(*card, "page_local_submit", "action_kind", string(control.ActionPredictionSend)) {
		t.Fatal("edit form has no explicit send action")
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("adoption also emitted %d asynchronous operations", len(gateway.operations))
	}
}

func TestPredictionSendRejectionKeepsInlineEditableDraft(t *testing.T) {
	for _, reason := range []string{"empty", "whitespace", "validation"} {
		t.Run(reason, func(t *testing.T) {
			app, gateway, inst, action := predictionCallbackFixture(t)
			app.HandleGatewayAction(context.Background(), action)
			gateway.operations = nil
			action.Kind = control.ActionPredictionSend
			action.Text = ""
			if reason == "whitespace" {
				action.Text = "  \n  "
			}
			if reason == "validation" {
				action.Text = "用户编辑后的内容\n保留换行"
				inst.WorkspaceRoot, inst.Threads["thread-1"].CWD = "", ""
			}
			sends := 0
			app.sendAgentCommand = func(_ string, command agentproto.Command) error {
				if command.Kind == agentproto.CommandPromptSend {
					sends++
					if command.Target.ThreadID != "thread-1" || command.Prompt.Inputs[0].Text != action.Text {
						t.Error("retry changed target or edited content")
					}
				}
				return nil
			}
			result := app.HandleGatewayAction(context.Background(), action)
			card := requirePredictionDraftReplacement(t, result)
			want := ""
			if reason == "validation" {
				want = action.Text
			}
			if got := predictionInputDefault(t, card); got != want {
				t.Errorf("draft content=%q, want %q", got, want)
			}
			if sends != 0 {
				t.Fatal("rejected send dispatched a prompt")
			}
			for _, op := range gateway.operations {
				if op.Kind == feishu.OperationUpdateCard || op.MessageID == action.MessageID {
					t.Fatal("callback also asynchronously updated original card")
				}
			}
			if reason != "validation" {
				// A second adoption must not refill a draft the user deliberately cleared.
				action.Kind = control.ActionPredictionAccept
				card = requirePredictionDraftReplacement(t, app.HandleGatewayAction(context.Background(), action))
				if got := predictionInputDefault(t, card); got != "" {
					t.Fatalf("empty draft reset to %q", got)
				}
				action.Kind = control.ActionPredictionSend
				action.Text = "修正后显式发送"
			} else {
				inst.WorkspaceRoot, inst.Threads["thread-1"].CWD = t.TempDir(), t.TempDir()
			}
			app.HandleGatewayAction(context.Background(), action)
			if sends != 1 {
				t.Fatalf("explicit retry dispatched %d prompts, want 1", sends)
			}
		})
	}
}

func requirePredictionDraftReplacement(t *testing.T, result *feishu.ActionResult) feishu.Operation {
	t.Helper()
	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatal("missing synchronous draft replacement")
	}
	card := *result.ReplaceCurrentCard
	if card.Kind != feishu.OperationSendCard || card.CardTitle != "编辑下一条消息" || !operationHasActionValue(card, "page_local_submit", "action_kind", string(control.ActionPredictionSend)) {
		t.Fatalf("callback lost editable draft: kind=%s title=%s", card.Kind, card.CardTitle)
	}
	return card
}

func predictionInputDefault(t *testing.T, card feishu.Operation) string {
	t.Helper()
	for _, element := range card.CardElements {
		if element["tag"] != "form" {
			continue
		}
		fields, _ := element["elements"].([]map[string]any)
		for _, field := range fields {
			if field["name"] == "prediction_text" {
				value, _ := field["default_value"].(string)
				return value
			}
		}
	}
	t.Fatal("prediction text field not found")
	return ""
}

func TestPredictionDraftUsesFullWidthMultilineInput(t *testing.T) {
	app, _, _, action := predictionCallbackFixture(t)
	card := requirePredictionDraftReplacement(t, app.HandleGatewayAction(context.Background(), action))
	for _, element := range card.CardElements {
		if element["tag"] != "form" {
			continue
		}
		fields, _ := element["elements"].([]map[string]any)
		for _, field := range fields {
			if field["name"] == "prediction_text" {
				if field["input_type"] != "multiline_text" {
					t.Fatalf("prediction input_type=%v, want multiline_text", field["input_type"])
				}
				if field["width"] != "fill" || field["label_position"] != "top" {
					t.Fatalf("prediction editor must fill available width with label above, got width=%v label_position=%v", field["width"], field["label_position"])
				}
				return
			}
		}
	}
	t.Fatal("missing prediction text field")
}

func TestPredictionRejectedCallbackPreservesOriginalCard(t *testing.T) {
	app, gateway, _, owner := predictionCallbackFixture(t)
	other := owner
	other.ActorUserID = "another-user"
	result := app.HandleGatewayAction(context.Background(), other)
	if result != nil && result.ReplaceCurrentCard != nil {
		t.Fatal("another user's rejected click replaced the original card")
	}
	for _, op := range gateway.operations {
		if op.Kind == feishu.OperationUpdateCard && op.MessageID == owner.MessageID {
			t.Fatal("another user's rejected click patched the original card")
		}
	}
	requirePredictionDraftReplacement(t, app.HandleGatewayAction(context.Background(), owner))
}
