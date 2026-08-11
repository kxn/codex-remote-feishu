package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestFilterFollowupEventsByPolicy(t *testing.T) {
	events := []eventcontract.Event{
		{
			Kind: eventcontract.KindNotice,
			Notice: &control.Notice{
				Code: "thread_selection_changed",
			},
			ThreadSelection: &control.ThreadSelectionChanged{
				ThreadID: "thread-1",
			},
		},
		{
			Kind: eventcontract.KindNotice,
			Notice: &control.Notice{
				Code: "generic_notice",
			},
		},
		{
			Kind:          eventcontract.KindSelection,
			SelectionView: &control.FeishuSelectionView{},
		},
	}
	filtered := filterFollowupEventsByPolicy(events, control.FeishuFollowupPolicy{
		DropClasses: []control.FeishuFollowupHandoffClass{
			control.FeishuFollowupHandoffClassThreadSelection,
		},
	})
	if len(filtered) != 2 {
		t.Fatalf("expected two events after filtering thread-selection followups, got %#v", filtered)
	}
	if filtered[0].Notice == nil || filtered[0].Notice.Code != "generic_notice" {
		t.Fatalf("unexpected first filtered event: %#v", filtered[0])
	}
}

func TestPathPickerFilteredFollowupEventsDropsNoticeClasses(t *testing.T) {
	events := []eventcontract.Event{
		{
			Kind: eventcontract.KindNotice,
			Notice: &control.Notice{
				Code: "generic_notice",
			},
		},
		{
			Kind: eventcontract.KindPathPicker,
			PathPickerView: &control.FeishuPathPickerView{
				PickerID: "picker-1",
			},
		},
	}
	filtered := filterPickerFollowupEvents(events)
	if len(filtered) != 1 || filtered[0].Kind != eventcontract.KindPathPicker {
		t.Fatalf("unexpected path picker filtered followups: %#v", filtered)
	}
}

func TestPickerSharedHelpersClampCursorAndExtractNotice(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cursor      int
		optionCount int
		want        int
	}{
		{name: "empty", cursor: 4, optionCount: 0, want: 0},
		{name: "negative", cursor: -1, optionCount: 3, want: 0},
		{name: "in range", cursor: 1, optionCount: 3, want: 1},
		{name: "too large", cursor: 8, optionCount: 3, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizePickerDropdownCursor(tc.cursor, tc.optionCount); got != tc.want {
				t.Fatalf("normalizePickerDropdownCursor(%d, %d) = %d, want %d", tc.cursor, tc.optionCount, got, tc.want)
			}
		})
	}

	events := []eventcontract.Event{
		{Kind: eventcontract.KindNotice, Notice: &control.Notice{Text: "  "}},
		{Kind: eventcontract.KindSelection, SelectionView: &control.FeishuSelectionView{}},
		{Kind: eventcontract.KindNotice, Notice: &control.Notice{Text: "  first visible notice  "}},
	}
	if got := firstNoticeText(events); got != "first visible notice" {
		t.Fatalf("firstNoticeText() = %q, want first visible notice", got)
	}

	filtered := filterPickerFollowupEvents([]eventcontract.Event{
		{Kind: eventcontract.KindNotice, Notice: &control.Notice{Code: "generic_notice"}},
		{Kind: eventcontract.KindPathPicker, PathPickerView: &control.FeishuPathPickerView{PickerID: "picker-1"}},
	})
	if len(filtered) != 1 || filtered[0].Kind != eventcontract.KindPathPicker {
		t.Fatalf("filterPickerFollowupEvents() = %#v, want only path picker event", filtered)
	}
}
