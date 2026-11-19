package orchestrator

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAsyncUserInputOversizedQuestionsUseCompleteTextFallback(t *testing.T) {
	for _, kind := range []string{"title", "option", "questions", "options"} {
		t.Run(kind, func(t *testing.T) {
			svc, _ := asyncQuestionService(t)
			event := asyncQuestionEvent("oversized-" + kind)
			event.Metadata["text"] = "请先保存文件。"
			questions := []asyncUserInputQuestion{{Title: "如何处理？", Options: []string{"保留", "删除"}}}
			switch kind {
			case "title":
				questions[0].Title = strings.Repeat("长题目", 5000) + "题目结尾"
			case "option":
				questions[0].Options[0] = strings.Repeat("长选项", 5000) + "选项结尾"
			case "questions":
				for len(questions) < 17 {
					questions = append(questions, questions[0])
				}
			case "options":
				for len(questions[0].Options) < 17 {
					questions[0].Options = append(questions[0].Options, "其他选项")
				}
			}
			event.Metadata["questions"] = questions
			events := svc.ApplyAgentEvent("inst-1", event)
			var text strings.Builder
			for _, output := range events {
				if output.PageView != nil {
					t.Fatal("oversized questions must not create an unsendable card")
				}
				if output.Block != nil {
					text.WriteString(output.Block.Text)
				}
			}
			if !strings.Contains(text.String(), "请先保存文件") || !strings.Contains(text.String(), "直接回复") {
				t.Fatal("fallback dropped context or the next action")
			}
			if kind == "title" && !strings.Contains(text.String(), "题目结尾") || kind == "option" && !strings.Contains(text.String(), "选项结尾") {
				t.Fatal("fallback truncated the original question or option")
			}
			if again := svc.ApplyAgentEvent("inst-1", event); len(again) != 0 {
				t.Fatal("fallback replay duplicated the question")
			}
		})
	}
}

func TestAsyncUserInputMaximumInteractiveShapeFitsTransport(t *testing.T) {
	for _, char := range []string{"问", "<", "😀"} {
		t.Run(char, func(t *testing.T) {
			questions := make([]asyncUserInputQuestion, 16)
			for i := range questions {
				questions[i].Title = strings.Repeat(char, 28)
				questions[i].Options = make([]string, 16)
				for j := range questions[i].Options {
					questions[i].Options[j] = strings.Repeat(char, 2)
				}
			}
			if !asyncUserInputFitsCard(questions) {
				t.Fatal("fixture should fit supported interactive shape")
			}
			record := &asyncUserInputRecord{ID: strings.Repeat("id", 100), ThreadID: "thread-1", Questions: questions, Context: strings.Repeat(char, 1000), Revision: 1}
			for i := 0; i <= len(questions); i++ {
				record.Submitted = i == len(questions)
				page := asyncUserInputPageEvent(&state.SurfaceConsoleRecord{SurfaceSessionID: "surface-1"}, record, "", "").PageView
				assertAsyncContextCardBudget(t, page)
				record.Answers = append(record.Answers, strings.Repeat(char, 5000))
			}
		})
	}
}
