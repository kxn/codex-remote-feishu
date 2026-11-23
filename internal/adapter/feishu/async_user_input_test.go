package feishu

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestAsyncUserInputNativeMessageCardCallbackRoundTrip(t *testing.T) {
	const question = "待批工作任务收到修改，本期如何处理？"
	const firstOption = "先拒绝本次修改，保留原任务待审批，待当前任务处理完毕后再重新发起修改申请。"
	const secondOption = "本期增加待批卡更新，修改后让审批人重新确认；保留原任务的处理记录和修改原因。\n请原样保留 [说明](details.md) 与 `revision`。"
	for _, freeText := range []bool{false, true} {
		name := "option"
		if freeText {
			name = "free_text"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
			svc := orchestrator.NewService(func() time.Time { return now }, orchestrator.Config{}, renderer.NewPlanner())
			surfaceID := "feishu:app-1:user:user-1"
			svc.MaterializeSurfaceResume(surfaceID, "app-1", "chat-1", "user-1", state.ProductModeVSCode, agentproto.BackendCodex, "", state.SurfaceVerbosityNormal, "")
			svc.UpsertInstance(&state.InstanceRecord{
				InstanceID: "inst-1", Online: true, WorkspaceRoot: "/tmp/project", ObservedFocusedThreadID: "thread-1",
				Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: "/tmp/project", Loaded: true}},
			})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: surfaceID, ActorUserID: "user-1", InstanceID: "inst-1"})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: surfaceID, ActorUserID: "user-1", ThreadID: "thread-1"})
			svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surfaceID, ActorUserID: "user-1", MessageID: "initial", Text: "请继续"})
			svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1"})
			tr := codex.NewTranslator("inst-1")
			native, err := json.Marshal(map[string]any{
				"method": "item/completed",
				"params": map[string]any{"threadId": "thread-1", "turnId": "turn-1", "item": map[string]any{
					"id": "item-1", "type": "agentMessage", "text": question, "delivery": "async",
					"questions": []map[string]any{{"title": question, "options": []string{firstOption, secondOption}}},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tr.ObserveServer(native)
			if err != nil || len(result.Events) != 1 {
				t.Fatalf("native protocol: %v %#v", err, result)
			}
			events := svc.ApplyAgentEvent("inst-1", result.Events[0])
			if len(events) != 1 || events[0].PageView == nil {
				t.Fatalf("expected question page: %#v", events)
			}
			events[0].DaemonLifecycleID = "life-1"
			projector := NewProjector()
			ops := projector.ProjectEvent("chat-1", events[0])
			if len(ops) != 1 || ops[0].Kind != OperationSendCard {
				t.Fatalf("question card projection: %#v", ops)
			}
			elements := renderedV2BodyElements(t, ops[0])
			var bodyParts []string
			for _, element := range elements {
				if text := plainTextContent(element); text != "" {
					bodyParts = append(bodyParts, text)
				}
			}
			questionBody := strings.Join(bodyParts, "\n")
			if !strings.Contains(questionBody, question+"\n\n1. "+firstOption+"\n\n2. "+secondOption) {
				t.Errorf("question body must separate the title and complete numbered options with blank lines, got %q", questionBody)
			}
			var optionGroup, answerInput map[string]any
			for _, element := range elements {
				if element["tag"] == "column_set" {
					optionGroup = element
				}
				if element["tag"] == "form" {
					fields, _ := element["elements"].([]map[string]any)
					if len(fields) != 0 {
						answerInput = fields[0]
					}
				}
			}
			columns, _ := optionGroup["columns"].([]map[string]any)
			if optionGroup["flex_mode"] != "flow" || len(columns) != 2 {
				t.Errorf("short option buttons must share a wrapping row, got %#v", optionGroup)
			}
			for _, column := range columns {
				if column["width"] != "auto" {
					t.Errorf("option buttons must keep natural width on narrow screens, got %#v", column)
				}
			}
			if answerInput["tag"] != "input" || answerInput["name"] != "answer" || answerInput["width"] != "fill" || answerInput["label_position"] != "top" || answerInput["input_type"] != nil {
				t.Errorf("answer must remain a single-line input with a top label and adaptive width, got %#v", answerInput)
			}
			labels := map[string]string{}
			for _, button := range cardActionsFromElements(elements) {
				payload := cardValueMap(button)
				if payload["kind"] != "async_user_input" || payload["option_id"] == "" {
					continue
				}
				optionID, _ := payload["option_id"].(string)
				text, _ := button["text"].(map[string]any)
				labels[optionID], _ = text["content"].(string)
			}
			if labels["0"] != "选 1" || labels["1"] != "选 2" {
				t.Fatalf("option buttons must stay short and match body numbering, got %#v", labels)
			}
			payload := findAsyncCallbackPayload(ops[0].CardElements, freeText)
			if payload == nil || payload["daemon_lifecycle_id"] != "life-1" {
				t.Fatalf("missing stamped %s payload: %#v", name, ops[0].CardElements)
			}
			// Feishu serializes callback numbers as JSON numbers, and target data
			// in the callback must not override the server-side question binding.
			raw, _ := json.Marshal(payload)
			var wirePayload map[string]interface{}
			if err := json.Unmarshal(raw, &wirePayload); err != nil {
				t.Fatal(err)
			}
			wirePayload["thread_id"] = "wrong-thread"
			gateway := NewLiveGateway(LiveGatewayConfig{GatewayID: "app-1"})
			gateway.recordSurfaceMessage("om-question", surfaceID)
			userID := "user-1"
			callback := &larkcallback.CallBackAction{Value: wirePayload}
			wantAnswer := secondOption
			if freeText {
				wantAnswer = "等全部空闲再切换"
				callback.FormValue = map[string]interface{}{"answer": wantAnswer}
			}
			action, ok := gateway.parseCardActionTriggerEvent(&larkcallback.CardActionTriggerEvent{Event: &larkcallback.CardActionTriggerRequest{
				Operator: &larkcallback.Operator{UserID: &userID}, Action: callback,
				Context: &larkcallback.Context{OpenChatID: "chat-1", OpenMessageID: "om-question"},
			}})
			if !ok || action.Kind != control.ActionAsyncUserInput || action.Request == nil || control.AllowsInlineCardReplacement(action) || !control.ResolveFeishuFrontstageActionContract(action).RejectsUnstampedCallback {
				t.Fatalf("question callback parse/lifecycle: %#v", action)
			}
			answered := svc.ApplySurfaceAction(action)
			var command *agentproto.Command
			var pageEvent *eventcontract.Event
			for i := range answered {
				if answered[i].Command != nil {
					command = answered[i].Command
				}
				if answered[i].PageView != nil {
					pageEvent = &answered[i]
				}
			}
			if command == nil || command.Kind != agentproto.CommandTurnSteer || command.Target.ThreadID != "thread-1" || !strings.Contains(command.Prompt.Inputs[0].Text, "回答："+wantAnswer) {
				t.Fatalf("callback delivered to wrong target: %#v", answered)
			}
			if pageEvent == nil || !pageEvent.InlineReplaceCurrentCard || !pageEvent.PageView.Sealed {
				t.Fatalf("answer must replace and seal question card: %#v", answered)
			}
			updated := projector.ProjectEvent("chat-1", *pageEvent)
			if len(updated) != 1 || updated[0].Kind != OperationUpdateCard || updated[0].MessageID != "om-question" {
				t.Fatalf("expected update of same question card: %#v", updated)
			}
		})
	}
}

func findAsyncCallbackPayload(value any, form bool) map[string]any {
	switch value := value.(type) {
	case map[string]any:
		if payload := cardValueMap(value); payload["kind"] == "async_user_input" {
			option, _ := payload["option_id"].(string)
			if (form && option == "") || (!form && option == "1") {
				return payload
			}
		}
		for _, child := range value {
			if found := findAsyncCallbackPayload(child, form); found != nil {
				return found
			}
		}
	case []map[string]any:
		for _, child := range value {
			if found := findAsyncCallbackPayload(child, form); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := findAsyncCallbackPayload(child, form); found != nil {
				return found
			}
		}
	}
	return nil
}
