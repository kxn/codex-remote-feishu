package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestFilterFollowupEventsByPolicy(t *testing.T) {
	events := []eventcontract.Event{
		{
			Kind: eventcontract.EventNotice,
			Notice: &control.Notice{
				Code: "thread_selection_changed",
			},
			ThreadSelection: &control.ThreadSelectionChanged{
				ThreadID: "thread-1",
			},
		},
		{
			Kind: eventcontract.EventNotice,
			Notice: &control.Notice{
				Code: "generic_notice",
			},
		},
		{
			Kind:          eventcontract.EventFeishuSelectionView,
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
			Kind: eventcontract.EventNotice,
			Notice: &control.Notice{
				Code: "generic_notice",
			},
		},
		{
			Kind: eventcontract.EventFeishuPathPicker,
			PathPickerView: &control.FeishuPathPickerView{
				PickerID: "picker-1",
			},
		},
	}
	filtered := pathPickerFilteredFollowupEvents(events)
	if len(filtered) != 1 || filtered[0].Kind != eventcontract.EventFeishuPathPicker {
		t.Fatalf("unexpected path picker filtered followups: %#v", filtered)
	}
}
