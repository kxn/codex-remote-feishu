package feishu

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestProjectThreadHistoryLoadingCreatesPatchableDirectCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.EventFeishuThreadHistory,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		ThreadHistoryView: &control.FeishuThreadHistoryView{
			PickerID:    "history-1",
			Title:       "历史记录",
			ThreadID:    "thread-1",
			ThreadLabel: "修复登录流程",
			Loading:     true,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one op, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationSendCard || op.ReplyToMessageID != "" || !op.CardUpdateMulti {
		t.Fatalf("expected patchable direct card, got %#v", op)
	}
}

func TestProjectThreadHistoryUpdatesExistingCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.ProjectEvent("chat-1", eventcontract.Event{
		Kind:             eventcontract.EventFeishuThreadHistory,
		SurfaceSessionID: "surface-1",
		ThreadHistoryView: &control.FeishuThreadHistoryView{
			PickerID:    "history-1",
			MessageID:   "om-history-1",
			Title:       "历史记录",
			ThreadID:    "thread-1",
			ThreadLabel: "修复登录流程",
			TurnOptions: []control.FeishuThreadHistoryTurnOption{
				{TurnID: "turn-1", Label: "#1 | completed | 修登录"},
			},
			TurnCount:  1,
			PageEnd:    1,
			TotalPages: 1,
		},
	})
	if len(ops) != 1 {
		t.Fatalf("expected one op, got %#v", ops)
	}
	op := ops[0]
	if op.Kind != OperationUpdateCard || op.MessageID != "om-history-1" || op.ReplyToMessageID != "" {
		t.Fatalf("expected update op, got %#v", op)
	}
}

func TestThreadHistoryListElementsUseHistoryCallbacks(t *testing.T) {
	elements := threadHistoryListElements(control.FeishuThreadHistoryView{
		PickerID:       "history-1",
		Page:           1,
		TotalPages:     3,
		SelectedTurnID: "turn-2",
		TurnOptions: []control.FeishuThreadHistoryTurnOption{
			{TurnID: "turn-2", Label: "#2 | completed | 第二轮", MetaText: "刚刚"},
			{TurnID: "turn-1", Label: "#1 | completed | 第一轮", MetaText: "1分前"},
		},
		Hint: "先在下拉里选中一轮，再查看详情。",
	}, "life-1")

	actions := cardActionsFromElements(elements)
	var sawDetail, sawPrev, sawNext bool
	for _, action := range actions {
		value := cardValueMap(action)
		switch value[cardActionPayloadKeyKind] {
		case cardActionKindHistoryDetail:
			sawDetail = true
		case cardActionKindHistoryPage:
			if _, ok := value[cardActionPayloadKeyPage]; !ok {
				sawPrev = true
			}
			if value[cardActionPayloadKeyPage] == 2 {
				sawNext = true
			}
		}
	}
	if !sawDetail || !sawPrev || !sawNext {
		t.Fatalf("expected history detail and pagination callbacks, got %#v", actions)
	}
}

func TestThreadHistoryDetailElementsKeepDynamicContentOutOfMarkdown(t *testing.T) {
	elements := threadHistoryDetailElements(control.FeishuThreadHistoryView{
		PickerID: "history-unsafe",
		Detail: &control.FeishuThreadHistoryTurnDetail{
			Ordinal:     3,
			Status:      "completed",
			TurnID:      "turn-unsafe",
			UpdatedText: "刚刚",
			ErrorText:   "错误详情：\n- 一条列表\n[本地链接](demo.md)",
			Inputs: []string{
				"# 用户输入\n- 列表项\n```bash\necho hi\n```",
			},
			Outputs: []string{
				"返回内容：\n[设计文档](docs/design.md)",
			},
		},
	}, "life-1")

	if len(elements) < 7 {
		t.Fatalf("expected summary, sections and buttons, got %#v", elements)
	}
	if !strings.Contains(markdownContent(elements[0]), "**第 3 轮**") {
		t.Fatalf("expected summary markdown first, got %#v", elements[0])
	}
	if got := plainTextContent(elements[2]); !containsAll(got, "错误详情：", "- 一条列表", "[本地链接](demo.md)") {
		t.Fatalf("expected error text to be rendered as plain text block, got %#v", elements[2])
	}
	if got := plainTextContent(elements[4]); !containsAll(got, "# 用户输入", "- 列表项", "```bash") {
		t.Fatalf("expected input entry to stay plain text, got %#v", elements[4])
	}
	if got := plainTextContent(elements[6]); !containsAll(got, "返回内容：", "[设计文档](docs/design.md)") {
		t.Fatalf("expected output entry to stay plain text, got %#v", elements[6])
	}
	if markdownContent(elements[4]) != "" || markdownContent(elements[6]) != "" {
		t.Fatalf("expected dynamic history entries to stop using markdown blocks, got %#v", elements)
	}
}

func TestThreadHistoryDetailElementsKeepFooterButtonsVisibleWithLargeContent(t *testing.T) {
	longLine := strings.Repeat("输出片段 ", 80)
	elements := threadHistoryDetailElements(control.FeishuThreadHistoryView{
		PickerID: "history-large",
		Detail: &control.FeishuThreadHistoryTurnDetail{
			Ordinal:     7,
			Status:      "completed",
			TurnID:      "turn-large",
			UpdatedText: "刚刚",
			Inputs: []string{
				"第一条输入\n" + longLine,
				"第二条输入\n" + longLine,
				"第三条输入\n" + longLine,
			},
			Outputs: []string{
				"第一条输出\n" + longLine,
				"第二条输出\n" + longLine,
				"第三条输出\n" + longLine,
			},
			PrevTurnID: "turn-newer",
			NextTurnID: "turn-older",
			ReturnPage: 2,
		},
	}, "life-1")

	plainBlocks := 0
	for _, element := range elements {
		if plainTextContent(element) != "" {
			plainBlocks++
		}
	}
	if plainBlocks != 2 {
		t.Fatalf("expected aggregated input/output plain-text blocks, got %d in %#v", plainBlocks, elements)
	}
	actions := cardActionsFromElements(elements)
	var sawNewer, sawOlder, sawBack bool
	for _, action := range actions {
		switch cardStringValue(action["text"].(map[string]any)["content"]) {
		case "较新一轮":
			sawNewer = true
		case "较旧一轮":
			sawOlder = true
		case "返回列表":
			sawBack = true
		}
	}
	if !sawNewer || !sawOlder || !sawBack {
		t.Fatalf("expected footer navigation buttons to remain visible, got %#v / actions=%#v", elements, actions)
	}
}

func TestThreadHistoryElementsKeepSummaryVisibleWhileLoading(t *testing.T) {
	elements := threadHistoryElements(control.FeishuThreadHistoryView{
		PickerID:    "history-loading",
		ThreadID:    "thread-1",
		ThreadLabel: "修复登录流程",
		TurnCount:   3,
		Loading:     true,
	}, "life-1")

	if len(elements) < 3 {
		t.Fatalf("expected summary + divider + loading notice, got %#v", elements)
	}
	if !strings.Contains(markdownContent(elements[0]), "**当前会话**") {
		t.Fatalf("expected loading card to keep summary first, got %#v", elements[0])
	}
	if plainTextContent(elements[len(elements)-1]) == "" || !strings.Contains(plainTextContent(elements[len(elements)-1]), "正在读取历史") {
		t.Fatalf("expected loading text to move into notice area, got %#v", elements)
	}
}

func TestThreadHistoryElementsKeepSummaryVisibleOnError(t *testing.T) {
	elements := threadHistoryElements(control.FeishuThreadHistoryView{
		PickerID:    "history-error",
		ThreadID:    "thread-1",
		ThreadLabel: "修复登录流程",
		TurnCount:   3,
		NoticeText:  "这张历史卡片已经失效，请重新发送 /history。",
	}, "life-1")

	if len(elements) < 3 {
		t.Fatalf("expected summary + divider + error notice, got %#v", elements)
	}
	if !strings.Contains(markdownContent(elements[0]), "**当前会话**") {
		t.Fatalf("expected error card to keep summary first, got %#v", elements[0])
	}
	if plainTextContent(elements[len(elements)-1]) == "" || !strings.Contains(plainTextContent(elements[len(elements)-1]), "重新发送 /history") {
		t.Fatalf("expected error text to move into notice area, got %#v", elements)
	}
}
