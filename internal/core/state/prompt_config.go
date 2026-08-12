package state

import "strings"

func NormalizeReasoningEffort(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func NormalizeClaudeReasoningEffort(value string) string {
	effort := NormalizeReasoningEffort(value)
	switch effort {
	case "low", "medium", "high", "max":
		return effort
	default:
		return ""
	}
}

func NormalizeOpenCodeReasoningEffort(value string) string {
	effort := NormalizeReasoningEffort(value)
	switch effort {
	case "low", "medium", "high", "xhigh", "max":
		return effort
	default:
		return ""
	}
}
