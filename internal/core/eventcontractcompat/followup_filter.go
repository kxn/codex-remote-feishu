package eventcontractcompat

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func FilterLegacyUIEventsByFollowupPolicy(events []control.UIEvent, policy control.FeishuFollowupPolicy) []control.UIEvent {
	if len(events) == 0 {
		return nil
	}
	policy = policy.Normalized()
	if policy.Empty() {
		return append([]control.UIEvent(nil), events...)
	}
	filtered := make([]control.UIEvent, 0, len(events))
	for _, event := range events {
		contractEvent := FromLegacyUIEvent(event)
		if policy.ShouldDropHandoffClass(string(contractEvent.Meta.Semantics.HandoffClass)) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}
