package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func normalizePickerDropdownCursor(cursor int, optionCount int) int {
	if optionCount <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= optionCount {
		return optionCount - 1
	}
	return cursor
}

func filterPickerFollowupEvents(events []eventcontract.Event) []eventcontract.Event {
	return filterFollowupEventsByPolicy(events, dropNoticeFollowupPolicy)
}

func firstNoticeText(events []eventcontract.Event) string {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		if text := strings.TrimSpace(event.Notice.Text); text != "" {
			return text
		}
	}
	return ""
}
