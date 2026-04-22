package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontractcompat"
)

var dropNoticeFollowupPolicy = control.FeishuFollowupPolicy{
	DropClasses: []control.FeishuFollowupHandoffClass{
		control.FeishuFollowupHandoffClassNotice,
		control.FeishuFollowupHandoffClassThreadSelection,
	},
}

func filterFollowupEventsByPolicy(events []control.UIEvent, policy control.FeishuFollowupPolicy) []control.UIEvent {
	return eventcontractcompat.FilterLegacyUIEventsByFollowupPolicy(events, policy)
}
