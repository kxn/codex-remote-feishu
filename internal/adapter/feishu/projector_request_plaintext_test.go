package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectRequestPromptKeepsDynamicSectionsOutOfMarkdown(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-unsafe",
		RequestType: "approval",
		ThreadTitle: "# 修复 `登录`",
		Sections: []control.FeishuCardTextSection{{
			Lines: []string{"请原样保留：", "- 列表项", "[链接](local.md)", "```go", "fmt.Println(1)", "```"},
		}},
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许执行", Style: "primary"},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected request prompt body to stay empty, got %#v", ops[0])
	}
	thread := plainTextContent(ops[0].CardElements[0])
	if !containsAll(thread, "当前会话：# 修复 `登录`") {
		t.Fatalf("expected thread title to stay in plain_text, got %q", thread)
	}
	body := plainTextContent(ops[0].CardElements[1])
	if !containsAll(body, "请原样保留：", "- 列表项", "[链接](local.md)", "```go", "fmt.Println(1)") {
		t.Fatalf("expected request body to stay in plain_text, got %q", body)
	}
	if markdownContent(ops[0].CardElements[0]) != "" || markdownContent(ops[0].CardElements[1]) != "" {
		t.Fatalf("expected request sections to avoid markdown elements, got %#v", ops[0].CardElements[:2])
	}
}

func TestProjectRequestUserInputPromptKeepsMarkdownMetacharactersInsidePlainTextQuestionBlock(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-ui-unsafe",
		RequestType: "request_user_input",
		Questions: []control.RequestPromptQuestion{
			{
				ID:           "notes",
				Header:       "# 标题",
				Question:     "请原样保留：\n- 列表项\n[链接](local.md)\n```go\nfmt.Println(1)\n```",
				Answered:     true,
				DefaultValue: "`rm -rf /`",
				AllowOther:   true,
				Options: []control.RequestPromptQuestionOption{
					{Label: "- 选项A", Description: "[描述](demo.md)"},
				},
			},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if got := markdownContent(ops[0].CardElements[1]); !strings.Contains(got, "问题 1") || strings.Contains(got, "# 标题") {
		t.Fatalf("expected question heading markdown to stay fixed-copy only, got %#v", ops[0].CardElements[1])
	}
	question := plainTextContent(ops[0].CardElements[2])
	if !containsAll(question,
		"标题：# 标题",
		"说明：",
		"请原样保留：",
		"- 列表项",
		"[链接](local.md)",
		"```go",
		"当前答案：`rm -rf /`",
		"- - 选项A：[描述](demo.md)",
	) {
		t.Fatalf("expected question block to preserve raw dynamic text inside plain_text, got %q", question)
	}
	if markdownContent(ops[0].CardElements[2]) != "" {
		t.Fatalf("expected question body to stay plain_text, got %#v", ops[0].CardElements[2])
	}
	rendered := renderedV2BodyElements(t, ops[0])
	foundQuestion := false
	for _, element := range rendered {
		if strings.Contains(plainTextContent(element), "标题：# 标题") {
			foundQuestion = true
			break
		}
	}
	if !foundQuestion {
		t.Fatalf("expected rendered V2 card to keep plain_text question block, got %#v", rendered)
	}
}

func TestProjectRequestPromptPromotesDetourLabelToHeaderSubtitle(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", requestPromptEvent(control.FeishuRequestView{
		RequestID:   "req-detour",
		RequestType: "approval",
		Title:       "需要确认",
		DetourLabel: "临时会话 · 分支",
		Options: []control.RequestPromptOption{
			{OptionID: "accept", Label: "允许执行", Style: "primary"},
		},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	header := renderedV2CardHeader(t, ops[0])
	if got := headerTextContent(header, "subtitle"); got != "**临时会话 · 分支**" {
		t.Fatalf("expected detour subtitle on request card, got %#v", header)
	}
}
