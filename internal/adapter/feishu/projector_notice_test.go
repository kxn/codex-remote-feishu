package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectStructuredDebugErrorNoticeUsesPlainTextSections(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind: eventcontract.KindNotice,
		Notice: &control.Notice{
			Code: "debug_error",
			Text: "位置：`gateway_apply`\n错误码：`send_card_failed`\n\n调试信息：\n```text\nraw `payload`\n```",
			Sections: []control.FeishuCardTextSection{
				{Label: "链路信息", Lines: []string{"位置：gateway_apply", "错误码：send_card_failed"}},
				{Label: "调试信息", Lines: []string{"raw `payload`\nnext line"}},
			},
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.TrimSpace(ops[0].CardBody) != "" {
		t.Fatalf("expected structured notice to stop using markdown body, got %#v", ops[0].CardBody)
	}
	if !containsMarkdownExact(ops[0].CardElements, "**链路信息**") || !containsMarkdownExact(ops[0].CardElements, "**调试信息**") {
		t.Fatalf("expected structured notice section headers, got %#v", ops[0].CardElements)
	}
	if !containsCardTextExact(ops[0].CardElements, "位置：gateway_apply\n错误码：send_card_failed") {
		t.Fatalf("expected metadata to render as plain text block, got %#v", ops[0].CardElements)
	}
	if !containsCardTextExact(ops[0].CardElements, "raw `payload`\nnext line") {
		t.Fatalf("expected debug payload to stay in plain text block, got %#v", ops[0].CardElements)
	}
	for _, element := range ops[0].CardElements {
		if content := markdownContent(element); strings.Contains(content, "gateway_apply") || strings.Contains(content, "raw `payload`") {
			t.Fatalf("expected dynamic notice content to stay out of markdown, got %#v", ops[0].CardElements)
		}
	}
}

func TestProjectGlobalRuntimeNoticeIgnoresReplyAnchor(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindNotice,
		SourceMessageID: "msg-1",
		Notice: &control.Notice{
			Code:             "daemon_shutting_down",
			Text:             "服务正在关闭。",
			DeliveryClass:    control.NoticeDeliveryClassGlobalRuntime,
			DeliveryFamily:   control.NoticeDeliveryFamilyDaemonShutdown,
			DeliveryDedupKey: "daemon_shutting_down",
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one op, got %#v", ops)
	}
	if ops[0].ReplyToMessageID != "" {
		t.Fatalf("expected global runtime notice to stay top-level, got %#v", ops[0])
	}
}

func TestProjectNoticePromotesDetourLabelToHeaderSubtitle(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind: eventcontract.KindNotice,
		Notice: &control.Notice{
			Code:                  "turn_failed",
			TemporarySessionLabel: "临时会话 · 分支",
			Text:                  "当前 turn 失败。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	header := renderedV2CardHeader(t, ops[0])
	if got := headerTextContent(header, "subtitle"); got != "**临时会话 · 分支**" {
		t.Fatalf("expected detour subtitle on notice card, got %#v", header)
	}
}
