package daemon

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestFilterThreadSelectionUIEventsDropsNoticeFamilyAnnouncements(t *testing.T) {
	events := []control.UIEvent{
		{
			Kind: control.UIEventNotice,
			Notice: &control.Notice{
				Code: "thread_selection_changed",
			},
			ThreadSelection: &control.ThreadSelectionChanged{
				ThreadID: "thread-1",
			},
		},
		{
			Kind: control.UIEventNotice,
			Notice: &control.Notice{
				Code: "some_other_notice",
			},
		},
		{
			Kind: control.UIEventThreadSelectionChange,
			ThreadSelection: &control.ThreadSelectionChanged{
				ThreadID: "thread-legacy",
			},
		},
	}

	filtered := filterThreadSelectionUIEvents(events)
	if len(filtered) != 1 {
		t.Fatalf("expected only non-thread-selection events to remain, got %#v", filtered)
	}
	if filtered[0].Notice == nil || filtered[0].Notice.Code != "some_other_notice" {
		t.Fatalf("unexpected filtered events: %#v", filtered)
	}
}
