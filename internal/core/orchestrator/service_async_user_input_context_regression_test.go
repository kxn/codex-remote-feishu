package orchestrator

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestAsyncUserInputContextSurvivesQuestionProgressAndSubmission(t *testing.T) {
	svc, _ := asyncQuestionService(t)
	event := asyncQuestionEvent("context-item")
	context := "确认后会删除工作区内的临时文件，请先保存需要保留的内容。"
	event.Metadata["text"] = context
	event.Metadata["questions"] = []any{
		map[string]any{"title": "是否清理？", "options": []any{"允许", "暂不清理"}},
		map[string]any{"title": "补充要求"},
	}
	first := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	assertAsyncContextVisible(t, first, context)
	action := asyncAnswerAction(first, 0)
	second := asyncPage(t, svc.ApplySurfaceAction(action))
	assertAsyncContextVisible(t, second, context)
	payload := second.Sections[0].Entries[0].Form.SubmitValue
	action.Request = &control.ActionRequestResponse{
		RequestID: payload["request_id"].(string), RequestRevision: payload["request_revision"].(int),
		Answers: map[string][]string{"answer": {"只清理构建缓存"}},
	}
	events := svc.ApplySurfaceAction(action)
	submitted := asyncPage(t, events)
	assertAsyncContextVisible(t, submitted, context)
	if !submitted.Sealed {
		t.Fatal("submitted card must be sealed")
	}
	command := firstCodexCommand(events, agentproto.CommandTurnSteer)
	if command == nil || !strings.Contains(command.Prompt.Inputs[0].Text, "问题：补充要求\n回答：只清理构建缓存") {
		t.Fatalf("context display must preserve original answer dispatch: %#v", command)
	}
}

func TestAsyncUserInputLongContextFitsTransportWithDisclosure(t *testing.T) {
	svc, _ := asyncQuestionService(t)
	event := asyncQuestionEvent("long-context-item")
	context := "清理前说明：" + strings.Repeat("请保存重要文件。", 3000) + "完整说明末尾：请确认备份已经完成。"
	event.Metadata["text"] = context
	events := svc.ApplyAgentEvent("inst-1", event)
	page := asyncPage(t, events)
	var fullExplanation strings.Builder
	for _, emitted := range events {
		if emitted.PageView != nil {
			break
		}
		if emitted.Block != nil {
			fullExplanation.WriteString(emitted.Block.Text)
		}
	}
	if !strings.Contains(fullExplanation.String(), "完整说明末尾：请确认备份已经完成。") || !strings.Contains(fullExplanation.String(), "清理前说明：") {
		t.Fatal("complete long explanation must be emitted before its question card, including its tail")
	}
	if !containsSectionLine(page, "清理前说明") || !containsSectionLine(page, "截断") {
		t.Fatal("long context must remain visible and disclose display truncation")
	}
	assertAsyncContextCardBudget(t, page)
	if len(page.Sections) == 0 || len(page.Sections[0].Entries[0].Buttons) != 2 {
		t.Fatal("context budgeting must retain every original answer choice")
	}
}

func TestAsyncUserInputLongAnswerDisplayDoesNotTruncateDispatch(t *testing.T) {
	svc, _ := asyncQuestionService(t)
	event := asyncQuestionEvent("long-answer-item")
	event.Metadata["text"] = "请写明需要保留的内容。"
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", event))
	action := asyncAnswerAction(page, 0)
	payload := page.Sections[0].Entries[0].Form.SubmitValue
	answer := strings.Repeat("保留原始测试数据。", 2000)
	action.Request = &control.ActionRequestResponse{
		RequestID: payload["request_id"].(string), RequestRevision: payload["request_revision"].(int),
		Answers: map[string][]string{"answer": {answer}},
	}
	events := svc.ApplySurfaceAction(action)
	command := firstCodexCommand(events, agentproto.CommandTurnSteer)
	if command == nil || command.Prompt.Inputs[0].Text != "问题：允许切换吗？\n回答："+answer {
		t.Fatal("display budgeting must preserve the complete answer sent to the original thread")
	}
	submitted := asyncPage(t, events)
	if !submitted.Sealed || !containsSectionLine(submitted, "截断") {
		t.Fatal("long submitted answer display must be sealed and disclose truncation")
	}
	assertAsyncContextCardBudget(t, submitted)
}

func assertAsyncContextVisible(t *testing.T, page *control.FeishuPageView, context string) {
	t.Helper()
	if !containsSectionLine(page, context) {
		t.Fatal("structured question card dropped the original agentMessage.text explanation")
	}
}

func assertAsyncContextCardBudget(t *testing.T, page *control.FeishuPageView) {
	t.Helper()
	elements := projector.PageElementsWithOptions(*page, "context-test-daemon", projector.PageRenderOptions{})
	payload := cardtransport.RenderInteractiveCardPayload(page.Title, "", page.ThemeKey, elements, true)
	messageSize, err := cardtransport.InteractiveMessagePayloadSize(payload)
	if err != nil {
		t.Fatal(err)
	}
	inlineSize, err := cardtransport.InlineCallbackPayloadSize(payload)
	if err != nil {
		t.Fatal(err)
	}
	if messageSize > cardtransport.InteractiveCardTransportLimitBytes || inlineSize > cardtransport.InteractiveCardTransportLimitBytes {
		t.Fatalf("async card exceeds actual transport budgets: message=%d inline=%d", messageSize, inlineSize)
	}
}
