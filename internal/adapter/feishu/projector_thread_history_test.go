package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestProjectThreadHistoryLoadingCreatesPatchableDirectCard(t *testing.T) {
	projector := NewProjector()
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventFeishuThreadHistory,
		SurfaceSessionID: "surface-1",
		SourceMessageID:  "om-source-1",
		FeishuThreadHistoryView: &control.FeishuThreadHistoryView{
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
	ops := projector.Project("chat-1", control.UIEvent{
		Kind:             control.UIEventFeishuThreadHistory,
		SurfaceSessionID: "surface-1",
		FeishuThreadHistoryView: &control.FeishuThreadHistoryView{
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
