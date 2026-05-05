package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestFinalReplyCardRendersDetourLabel(t *testing.T) {
	projector := NewProjector()

	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:                 eventcontract.KindBlockCommitted,
		SourceMessageID:      "msg-1",
		SourceMessagePreview: "顺手问个岔题",
		Block: &render.Block{
			ThreadID:              "thread-detour",
			TurnID:                "turn-detour",
			ItemID:                "item-1",
			Kind:                  render.BlockAssistantMarkdown,
			Text:                  "已经处理完了。",
			TemporarySessionLabel: "临时会话 · 分支",
			Final:                 true,
		},
	})

	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("expected one final reply card, got %#v", ops)
	}
	header := renderedV2CardHeader(t, ops[0])
	if got := headerTextContent(header, "subtitle"); got != "**临时会话 · 分支**" {
		t.Fatalf("expected detour subtitle in final card header, got %#v", header)
	}
	if got := headerTextTag(header, "subtitle"); got != cardTextTagLarkMarkdown {
		t.Fatalf("expected detour subtitle to use markdown header text, got %#v", header)
	}
}

func TestStreamingTextLaneIgnoresDetourLabel(t *testing.T) {
	projector := NewProjector()

	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-1",
		Block: &render.Block{
			ThreadID:              "thread-detour",
			TurnID:                "turn-detour",
			ItemID:                "item-1",
			Kind:                  render.BlockAssistantMarkdown,
			Text:                  "我先看一下目录结构。",
			TemporarySessionLabel: "临时会话 · 分支",
			Final:                 false,
		},
	})

	if len(ops) != 1 || ops[0].Kind != OperationSendText {
		t.Fatalf("expected streaming text op, got %#v", ops)
	}
	if ops[0].Text != "我先看一下目录结构。" {
		t.Fatalf("expected streaming text to stay unchanged, got %#v", ops[0])
	}
}
