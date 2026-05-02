package state

import "strings"

func NormalizeReasoningEffort(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}
