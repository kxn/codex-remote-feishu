package eventcontractcompat

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestFilterLegacyUIEventsByFollowupPolicy(t *testing.T) {
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
				Code: "generic_notice",
			},
		},
		{
			Kind:                control.UIEventFeishuSelectionView,
			FeishuSelectionView: &control.FeishuSelectionView{},
		},
	}
	policy := control.FeishuFollowupPolicy{
		DropClasses: []control.FeishuFollowupHandoffClass{
			control.FeishuFollowupHandoffClassNotice,
			control.FeishuFollowupHandoffClassThreadSelection,
		},
	}
	filtered := FilterLegacyUIEventsByFollowupPolicy(events, policy)
	if len(filtered) != 1 {
		t.Fatalf("expected one event to remain, got %#v", filtered)
	}
	if filtered[0].Kind != control.UIEventFeishuSelectionView {
		t.Fatalf("unexpected remaining event: %#v", filtered[0])
	}
}

func TestFilterLegacyUIEventsByFollowupPolicyEmptyPolicyKeepsEvents(t *testing.T) {
	events := []control.UIEvent{
		{Kind: control.UIEventNotice, Notice: &control.Notice{Code: "notice"}},
	}
	filtered := FilterLegacyUIEventsByFollowupPolicy(events, control.FeishuFollowupPolicy{})
	if len(filtered) != 1 || filtered[0].Notice == nil {
		t.Fatalf("expected all events to remain, got %#v", filtered)
	}
}
