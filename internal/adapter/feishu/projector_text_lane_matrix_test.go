package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestTextLaneMatrix_NonFinalAssistantBlockUsesDirectTextLane(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-1",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  "# 标题\n- 列表项\n[本地链接](docs/demo.md)",
			Final: false,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendText {
		t.Fatalf("expected non-final assistant block to use send_text lane, got %#v", ops)
	}
	if ops[0].Text != "# 标题\n- 列表项\n[本地链接](docs/demo.md)" {
		t.Fatalf("expected direct text lane to preserve raw text, got %#v", ops[0])
	}
}

func TestTextLaneMatrix_RequestPromptUsesStructuredCardLane(t *testing.T) {
	projector := NewProjector()
	threadTitle := "# 修复 `登录`"
	question := "请原样保留：\n- 列表项\n[链接](local.md)\n```go\nfmt.Println(1)\n```"
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventFeishuDirectRequestPrompt,
		FeishuDirectRequestPrompt: &control.FeishuDirectRequestPrompt{
			RequestID:   "req-matrix",
			RequestType: "request_user_input",
			ThreadTitle: threadTitle,
			Questions: []control.RequestPromptQuestion{{
				ID:       "notes",
				Header:   "问题标题",
				Question: question,
			}},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.TrimSpace(ops[0].CardBody) != "" {
		t.Fatalf("expected structured card lane to keep markdown body empty, got %#v", ops[0])
	}
	elements := renderedV2BodyElements(t, ops[0])
	if !containsPlainTextFragment(elements, "当前会话："+threadTitle) {
		t.Fatalf("expected thread title to render in plain_text, got %#v", elements)
	}
	if !containsPlainTextFragment(elements, question) {
		t.Fatalf("expected question text to render in plain_text, got %#v", elements)
	}
	for _, fragment := range []string{threadTitle, question} {
		if containsMarkdownFragment(elements, fragment) {
			t.Fatalf("expected dynamic fragment %q to stay out of markdown elements, got %#v", fragment, elements)
		}
	}
}

func TestTextLaneMatrix_FixedCopyNoticeKeepsMarkdownBodyLane(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "当前只支持 `/mode codex` 和 `/mode claude`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.TrimSpace(ops[0].CardBody) == "" {
		t.Fatalf("expected fixed-copy notice to keep markdown body lane, got %#v", ops[0])
	}
	elements := renderedV2BodyElements(t, ops[0])
	if len(elements) == 0 || markdownContent(elements[0]) != ops[0].CardBody {
		t.Fatalf("expected notice body to render as markdown payload body, got %#v", elements)
	}
}

func TestTextLaneMatrix_FinalReplyUsesFinalMarkdownLane(t *testing.T) {
	projector := NewProjector()
	raw := "先看 [Guide](./docs/guide.md:12)，再看 [RFC](https://example.com/rfc)。"
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-final",
		Block: &render.Block{
			Kind:  render.BlockAssistantMarkdown,
			Text:  raw,
			Final: true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].FinalSourceBody() != raw {
		t.Fatalf("expected final lane to retain raw source body, got %#v", ops[0])
	}
	if ops[0].CardBody != "先看 Guide (`./docs/guide.md:12`)，再看 [RFC](https://example.com/rfc)。" {
		t.Fatalf("expected final lane to render normalized markdown body, got %#v", ops[0])
	}
	elements := renderedV2BodyElements(t, ops[0])
	if len(elements) == 0 || markdownContent(elements[0]) != ops[0].CardBody {
		t.Fatalf("expected final payload to render body as markdown component, got %#v", elements)
	}
}

func containsMarkdownFragment(elements []map[string]any, want string) bool {
	for _, element := range elements {
		if strings.Contains(markdownContent(element), want) {
			return true
		}
	}
	return false
}

func containsPlainTextFragment(elements []map[string]any, want string) bool {
	for _, element := range elements {
		if strings.Contains(plainTextContent(element), want) {
			return true
		}
	}
	return false
}
