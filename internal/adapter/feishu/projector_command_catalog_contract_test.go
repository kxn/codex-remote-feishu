package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectCommandCatalogSummaryUsesPlainTextSections(t *testing.T) {
	projector := NewProjector()
	summary := "状态：`ok`\n下一步：[打开 Cron](https://example.com/cron)"
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuPageView{
		Title:           "Cron",
		SummarySections: summarySections(summary),
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if ops[0].CardBody != "" {
		t.Fatalf("expected summary fallback to avoid markdown body, got %#v", ops[0])
	}
	if !containsCardTextExact(ops[0].CardElements, summary) {
		t.Fatalf("expected summary fallback plain text block, got %#v", ops[0].CardElements)
	}
	for _, element := range ops[0].CardElements {
		if strings.Contains(markdownContent(element), summary) {
			t.Fatalf("expected summary fallback to avoid markdown echo, got %#v", ops[0].CardElements)
		}
	}
}

func TestProjectCommandCatalogEntryUsesPlainTextSections(t *testing.T) {
	projector := NewProjector()
	title := "标题 `x` [link](https://example.com)"
	command := "/do `x`"
	desc := "说明 [link](https://example.com)"
	example := "/do example"
	want := title + "\n命令：" + command + "\n" + desc + "\n例如：" + example
	ops := projector.Project("chat-1", commandCatalogEvent(control.FeishuPageView{
		Title: "测试目录",
		Sections: []control.CommandCatalogSection{{
			Title: "测试分组",
			Entries: []control.CommandCatalogEntry{{
				Title:       title,
				Commands:    []string{command},
				Description: desc,
				Examples:    []string{example},
			}},
		}},
	}))
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !containsCardTextExact(ops[0].CardElements, want) {
		t.Fatalf("expected entry fallback plain text block, got %#v", ops[0].CardElements)
	}
	for _, fragment := range []string{title, command, desc, example} {
		for _, element := range ops[0].CardElements {
			if strings.Contains(markdownContent(element), fragment) {
				t.Fatalf("expected entry fallback fragment %q to stay out of markdown, got %#v", fragment, ops[0].CardElements)
			}
		}
	}
}
