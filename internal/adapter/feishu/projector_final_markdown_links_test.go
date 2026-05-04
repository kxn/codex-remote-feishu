package feishu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestProjectFinalAssistantBlockPreservesExplicitRemoteMarkdownLinks(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-remote-link",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "查看 [issue 227](https://github.com/kxn/codex-remote-feishu/issues/227)。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	want := "查看 [issue 227](https://github.com/kxn/codex-remote-feishu/issues/227)。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected final markdown body: %#v", ops[0])
	}
	elements := renderedMarkdownElementContents(t, ops[0])
	if len(elements) == 0 || elements[0] != want {
		t.Fatalf("unexpected rendered markdown elements: %#v", elements)
	}
}

func TestProjectFinalAssistantBlockNeutralizesUnsupportedLocalMarkdownLinks(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-local-link",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "先看 [Guide](./docs/guide.md:12)，再看 [RFC](https://example.com/rfc)。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	want := "先看 Guide (`./docs/guide.md:12`)，再看 [RFC](https://example.com/rfc)。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected final markdown body: %#v", ops[0])
	}
	if ops[0].FinalSourceBody() != "先看 [Guide](./docs/guide.md:12)，再看 [RFC](https://example.com/rfc)。" {
		t.Fatalf("expected final source body to retain pre-render markdown, got %#v", ops[0])
	}
	elements := renderedMarkdownElementContents(t, ops[0])
	if len(elements) == 0 || elements[0] != want {
		t.Fatalf("unexpected rendered markdown elements: %#v", elements)
	}
}

func TestProjectFinalAssistantBlockSkipsCodeWhenNormalizingLocalLinks(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-code-link",
		Block: &render.Block{
			Kind: render.BlockAssistantMarkdown,
			Text: "外部 [Guide](./docs/guide.md:12)\n\n" +
				"inline `[Inline](./docs/inline.md:3)`\n\n" +
				"```md\n[Keep](./docs/keep.md:8)\n```\n\n" +
				"最后 [RFC](https://example.com/rfc)。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	want := "外部 Guide (`./docs/guide.md:12`)\n\n" +
		"inline `[Inline](./docs/inline.md:3)`\n\n" +
		"```md\n[Keep](./docs/keep.md:8)\n```\n\n" +
		"最后 [RFC](https://example.com/rfc)。"
	if ops[0].CardBody != want {
		t.Fatalf("unexpected final markdown body: %#v", ops[0])
	}
	elements := renderedMarkdownElementContents(t, ops[0])
	if len(elements) == 0 || elements[0] != want {
		t.Fatalf("unexpected rendered markdown elements: %#v", elements)
	}
}

func TestProjectFinalAssistantBlockConvertsOverflowTablesToFencedText(t *testing.T) {
	projector := NewProjector()
	blocks := make([]string, 0, finalCardMarkdownTableLimit+1)
	for i := 1; i <= finalCardMarkdownTableLimit+1; i++ {
		blocks = append(blocks, testFinalMarkdownTableBlock(i))
	}
	text := strings.Join(blocks, "\n\n")
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-table-overflow",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        text,
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	source := ops[0].FinalSourceBody()
	if !strings.Contains(source, testFinalMarkdownTableBlock(finalCardMarkdownTableLimit)) {
		t.Fatalf("expected last in-budget table to stay raw, got %#v", source)
	}
	wantOverflow := "```text\n" + testFinalMarkdownTableBlock(finalCardMarkdownTableLimit+1) + "\n```"
	if !strings.Contains(source, wantOverflow) {
		t.Fatalf("expected overflow table to be rewritten as fenced text, got %#v", source)
	}
	if ops[0].CardBody != source {
		t.Fatalf("expected rendered body to match normalized source when no links are present, got source=%q body=%q", source, ops[0].CardBody)
	}
}

func TestProjectFinalAssistantBlockSkipsFencedTablesWhenBudgeting(t *testing.T) {
	projector := NewProjector()
	blocks := make([]string, 0, finalCardMarkdownTableLimit+2)
	for i := 1; i <= finalCardMarkdownTableLimit; i++ {
		blocks = append(blocks, testFinalMarkdownTableBlock(i))
	}
	blocks = append(blocks, "```md\n| keep | raw |\n| --- | --- |\n| inside | fence |\n```")
	blocks = append(blocks, testFinalMarkdownTableBlock(finalCardMarkdownTableLimit+1))
	text := strings.Join(blocks, "\n\n")
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:            eventcontract.KindBlockCommitted,
		SourceMessageID: "msg-table-fence",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        text,
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	source := ops[0].FinalSourceBody()
	if !strings.Contains(source, "```md\n| keep | raw |\n| --- | --- |\n| inside | fence |\n```") {
		t.Fatalf("expected original fenced table block to stay untouched, got %#v", source)
	}
	if !strings.Contains(source, testFinalMarkdownTableBlock(finalCardMarkdownTableLimit)) {
		t.Fatalf("expected in-budget raw table before fenced block to stay raw, got %#v", source)
	}
	wantOverflow := "```text\n" + testFinalMarkdownTableBlock(finalCardMarkdownTableLimit+1) + "\n```"
	if !strings.Contains(source, wantOverflow) {
		t.Fatalf("expected only post-budget raw table to be rewritten, got %#v", source)
	}
}

func testFinalMarkdownTableBlock(index int) string {
	return fmt.Sprintf("| h%d | v%d |\n| --- | --- |\n| r%d | c%d |", index, index, index, index)
}

func renderedMarkdownElementContents(t *testing.T, operation Operation) []string {
	t.Helper()
	payload := renderOperationCard(operation, operation.effectiveCardEnvelope())
	assertRenderedCardPayloadBasicInvariants(t, payload)
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	values := make([]string, 0, len(elements))
	for _, element := range elements {
		if cardStringValue(element["tag"]) != "markdown" {
			continue
		}
		values = append(values, cardStringValue(element["content"]))
	}
	return values
}
