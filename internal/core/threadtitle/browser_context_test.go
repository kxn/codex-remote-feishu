package threadtitle

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const browserContextFixture = "<in-app-browser-context source=\"ambient-ui-state\">\nBrowser context, not the user's request.\n</in-app-browser-context>"

func TestBrowserContextTitle(t *testing.T) {
	t.Parallel()
	request := browserContextFixture + "\n\n## My request:\n继续优化示例功能\n补充说明"
	tests := []struct {
		name   string
		thread state.ThreadRecord
		want   string
	}{
		{"renamed conversation", state.ThreadRecord{Name: "示例会话", FirstUserMessage: request}, "示例会话"},
		{"title contains browser context", state.ThreadRecord{Name: request}, "继续优化示例功能 补充说明"},
		{"first user message contains context", state.ThreadRecord{Name: "新会话", FirstUserMessage: request}, "继续优化示例功能"},
		{"last user message contains context", state.ThreadRecord{LastUserMessage: request}, "继续优化示例功能"},
		{"latest snippet retains priority", state.ThreadRecord{FirstUserMessage: request, LastUserMessage: browserContextFixture + "\n## My request:\n检查最新结果"}, "检查最新结果"},
		{"context only falls back", state.ThreadRecord{Name: browserContextFixture, FirstUserMessage: "修复登录问题"}, "修复登录问题"},
		{"multiple leading blocks", state.ThreadRecord{Name: browserContextFixture + "\n" + request}, "继续优化示例功能 补充说明"},
		{"CRLF and leading whitespace", state.ThreadRecord{FirstUserMessage: "\u3000 \t\r\n" + strings.ReplaceAll(request, "\n", "\r\n")}, "继续优化示例功能"},
		{"context after user text stays literal", state.ThreadRecord{Name: "分析这个标签 " + browserContextFixture}, "分析这个标签 " + normalizeText(browserContextFixture)},
		{"similar tag stays literal", state.ThreadRecord{Name: "<in-app-browser-context-example source=\"ambient-ui-state\">示例</in-app-browser-context-example>"}, "<in-app-browser-context-example source=\"ambient-ui-state\">示例</in-app-browser-context-example>"},
		{"request heading without context stays literal", state.ThreadRecord{Name: "## My request: 示例标题"}, "## My request: 示例标题"},
		{"near-match request heading stays literal", state.ThreadRecord{Name: browserContextFixture + "\n## My request:example"}, "## My request:example"},
		{"incomplete context stays literal", state.ThreadRecord{Name: "<in-app-browser-context source=\"ambient-ui-state\">未闭合"}, "<in-app-browser-context source=\"ambient-ui-state\">未闭合"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RawSemanticTitle(&tc.thread); got != tc.want {
				t.Fatalf("RawSemanticTitle() = %q, want %q", got, tc.want)
			}
		})
	}
	thread := &state.ThreadRecord{Name: request, CWD: filepath.Join(t.TempDir(), "sample-project")}
	if got := DisplayTitle(nil, thread, DefaultDisplayLimit); got != "sample-project · 继续优化示例功能 补充说明" {
		t.Fatalf("DisplayTitle() = %q", got)
	}
	if got := DisplayBody(&state.ThreadRecord{Name: browserContextFixture, FirstUserMessage: browserContextFixture, LastUserMessage: browserContextFixture}, DefaultDisplayLimit); got != UnnamedDisplayName {
		t.Fatalf("metadata-only DisplayBody() = %q", got)
	}
}

func TestBrowserContextOpeningTag(t *testing.T) {
	t.Parallel()
	for _, opening := range []string{
		"<in-app-browser-context source='ambient-ui-state'>",
		"<in-app-browser-context note='a > b' source=\"ambient-ui-state\">",
		"<in-app-browser-context source=\"ambient-ui-state\" note='&lt;/in-app-browser-context>'>",
	} {
		text := opening + "Unescaped browser text: a < b & c</in-app-browser-context>\n## My request:\n修复示例功能"
		if got := RawSemanticTitle(&state.ThreadRecord{Name: text}); got != "修复示例功能" {
			t.Fatalf("opening %q: RawSemanticTitle() = %q", opening, got)
		}
	}
	for _, text := range []string{
		"<in-app-browser-context>literal</in-app-browser-context>",
		"<in-app-browser-context source='manual'>literal</in-app-browser-context>",
		"<in-app-browser-context note='source=\"ambient-ui-state\"'>literal</in-app-browser-context>",
		"<in-app-browser-context source='manual' note='source=\"ambient-ui-state\"'>literal</in-app-browser-context>",
		"<in-app-browser-context data-source='ambient-ui-state'>literal</in-app-browser-context>",
		"<in-app-browser-context source='ambient-ui-state' source='manual'>literal</in-app-browser-context>",
		"<in-app-browser-context source='ambient-ui-state' broken>literal</in-app-browser-context>",
		"<in-app-browser-context source='ambient-ui-state'/>literal</in-app-browser-context>",
	} {
		if got := RawSemanticTitle(&state.ThreadRecord{Name: text}); got != text {
			t.Fatalf("literal %q became %q", text, got)
		}
	}
}

func TestBrowserContextStoredTitle(t *testing.T) {
	t.Parallel()
	ctx := Context{ThreadID: "019d56f0-de5e-7943-bc9a-18c42ef11acb", ThreadCWD: filepath.Join(t.TempDir(), "sample-project")}
	shortID := control.ShortenThreadID(ctx.ThreadID)
	request := browserContextFixture + "\n## My request:\n修复示例功能"
	for _, snapshot := range []string{
		request,
		"sample-project · " + request + " · " + shortID,
		browserContextFixture + "\n## My request:\nsample-project · 修复示例功能 · " + shortID,
	} {
		stored := StoredTitle(snapshot, ctx, nil)
		if stored != "修复示例功能" {
			t.Fatalf("StoredTitle(%q) = %q", snapshot, stored)
		}
		thread := &state.ThreadRecord{Name: stored, CWD: ctx.ThreadCWD}
		if got := DisplayTitle(nil, thread, DefaultDisplayLimit); got != "sample-project · 修复示例功能" {
			t.Fatalf("restored DisplayTitle() = %q", got)
		}
	}
	if got := StoredTitle(request, ctx, &state.ThreadRecord{Name: "自定义标题"}); got != "自定义标题" {
		t.Fatalf("renamed StoredTitle() = %q", got)
	}
}
