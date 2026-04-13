package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
)

func TestProjectUsageNoticePreservesQuotesInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "请求层把 `\"/api/admin/*\"` 和 `\"/api/setup/*\"` 统一转成本地路径。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&#34;") || strings.Contains(ops[0].CardBody, "&#39;") {
		t.Fatalf("expected inline code to preserve quotes, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		`<text_tag color='neutral'>"/api/admin/*"</text_tag>`,
		`<text_tag color='neutral'>"/api/setup/*"</text_tag>`,
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticePreservesAmpersandsInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "请先跑 `go test ./internal/app/daemon ./internal/core/orchestrator`，再执行 `cd web && npm test -- --run src/lib/api.test.ts`。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&amp;&amp;") {
		t.Fatalf("expected inline code to preserve &&, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>go test ./internal/app/daemon ./internal/core/orchestrator</text_tag>",
		"<text_tag color='neutral'>cd web && npm test -- --run src/lib/api.test.ts</text_tag>",
	) {
		t.Fatalf("unexpected usage notice body: %#v", ops[0].CardBody)
	}
}

func TestProjectUsageNoticeKeepsLiteralEntitiesEscapedInInlineTags(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind: control.UIEventNotice,
		Notice: &control.Notice{
			Code: "surface_override_usage",
			Text: "如果你就是要展示实体字面量，请写成 `&lt;text_tag&gt;`，不要把它当成真实标签。",
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if !strings.Contains(ops[0].CardBody, "<text_tag color='neutral'>&amp;lt;text_tag&amp;gt;</text_tag>") {
		t.Fatalf("expected literal entity form to stay escaped, got %#v", ops[0].CardBody)
	}
}

func TestProjectFinalAssistantBlockPreservesAngleBracketsInInlineCode(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:            control.UIEventBlockCommitted,
		SourceMessageID: "msg-inline-angle",
		Block: &render.Block{
			Kind:        render.BlockAssistantMarkdown,
			Text:        "请运行 `/model <模型> <推理强度>`，再检查 `a < b > c`。",
			ThreadID:    "thread-1",
			ThreadTitle: "droid · 修复登录流程",
			ThemeKey:    "thread-1",
			Final:       true,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if strings.Contains(ops[0].CardBody, "&gt;") || strings.Contains(ops[0].CardBody, "&lt;") {
		t.Fatalf("expected final inline code to preserve angle brackets, got %#v", ops[0].CardBody)
	}
	if !containsAll(ops[0].CardBody,
		"<text_tag color='neutral'>/model <模型> <推理强度></text_tag>",
		"<text_tag color='neutral'>a < b > c</text_tag>",
	) {
		t.Fatalf("unexpected final inline-tag body: %#v", ops[0].CardBody)
	}
}
