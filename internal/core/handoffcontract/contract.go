package handoffcontract

import "strings"

type HandoffClass string

const (
	HandoffClassDefault         HandoffClass = ""
	HandoffClassNavigation      HandoffClass = "ui_navigation"
	HandoffClassNotice          HandoffClass = "notice"
	HandoffClassThreadSelection HandoffClass = "thread_selection"
	HandoffClassProcessDetail   HandoffClass = "process_detail"
	HandoffClassTerminalContent HandoffClass = "terminal_content"
)

type FollowupPolicy struct {
	DropClasses []HandoffClass
	KeepClasses []HandoffClass
}

func (policy FollowupPolicy) Normalized() FollowupPolicy {
	policy.DropClasses = normalizeHandoffClasses(policy.DropClasses)
	policy.KeepClasses = normalizeHandoffClasses(policy.KeepClasses)
	return policy
}

func (policy FollowupPolicy) Empty() bool {
	policy = policy.Normalized()
	return len(policy.DropClasses) == 0 && len(policy.KeepClasses) == 0
}

func (policy FollowupPolicy) ShouldDropHandoffClass(class string) bool {
	class = strings.TrimSpace(class)
	if class == "" {
		return false
	}
	policy = policy.Normalized()
	for _, keep := range policy.KeepClasses {
		if strings.TrimSpace(string(keep)) == class {
			return false
		}
	}
	for _, drop := range policy.DropClasses {
		if strings.TrimSpace(string(drop)) == class {
			return true
		}
	}
	return false
}

func normalizeHandoffClasses(classes []HandoffClass) []HandoffClass {
	if len(classes) == 0 {
		return nil
	}
	out := make([]HandoffClass, 0, len(classes))
	seen := map[HandoffClass]struct{}{}
	for _, class := range classes {
		normalized := normalizeHandoffClass(class)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeHandoffClass(class HandoffClass) HandoffClass {
	switch HandoffClass(strings.TrimSpace(string(class))) {
	case HandoffClassNotice,
		HandoffClassThreadSelection,
		HandoffClassNavigation,
		HandoffClassProcessDetail,
		HandoffClassTerminalContent:
		return HandoffClass(strings.TrimSpace(string(class)))
	default:
		return ""
	}
}
