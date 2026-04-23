package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectSelectionPromptKeepsMarkdownMetacharactersInsidePlainTextDetailBlock(t *testing.T) {
	projector := NewProjector()
	view := control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread: &control.FeishuThreadSelectionView{
			Mode: control.FeishuThreadSelectionNormalScopedRecent,
			Entries: []control.FeishuThreadSelectionEntry{{
				ThreadID: "thread-1",
				Summary:  "修复 `登录`",
				Status:   "/data/dl/#demo\n- 列表项\n[本地链接](docs/demo.md)",
			}},
		},
	}
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:          eventcontract.KindSelection,
		SelectionView: &view,
		SelectionContext: &control.FeishuUISelectionContext{
			DTOOwner:   control.FeishuUIDTOwnerSelection,
			PromptKind: control.SelectionPromptUseThread,
		},
	})
	if len(ops) != 1 || ops[0].Kind != OperationSendCard {
		t.Fatalf("unexpected ops: %#v", ops)
	}
	if len(ops[0].CardElements) != 3 {
		t.Fatalf("unexpected selection elements: %#v", ops[0].CardElements)
	}
	detail := plainTextContent(ops[0].CardElements[2])
	if !containsAll(detail,
		"/data/dl/#demo",
		"- 列表项",
		"[本地链接](docs/demo.md)",
	) {
		t.Fatalf("expected selection detail to preserve raw dynamic text in plain_text, got %q", detail)
	}
	if markdownContent(ops[0].CardElements[2]) != "" {
		t.Fatalf("expected selection detail to stop using markdown element, got %#v", ops[0].CardElements[2])
	}
}
