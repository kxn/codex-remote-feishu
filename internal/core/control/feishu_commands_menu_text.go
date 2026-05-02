package control

import "strings"

func normalizeModelMenuArgument(value string) (string, bool) {
	model := strings.TrimSpace(value)
	if model == "" {
		return "", false
	}
	return model, true
}

func normalizeReasoningMenuArgument(value string) (string, bool) {
	effort := strings.ToLower(strings.TrimSpace(value))
	switch effort {
	case "low", "medium", "high", "xhigh", "max", "clear":
		return effort, true
	default:
		return "", false
	}
}

func normalizeAccessMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "full", "confirm", "clear":
		return mode, true
	default:
		return "", false
	}
}

func normalizeModeMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "normal", "codex", "claude", "vscode":
		return mode, true
	default:
		return "", false
	}
}

func normalizeAutoWhipMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "on", "off":
		return mode, true
	default:
		return "", false
	}
}

func normalizeAutoContinueMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "on", "off":
		return mode, true
	default:
		return "", false
	}
}

func normalizeUpgradeMenuArgument(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "latest", "local":
		return mode, true
	case "track":
		return "track", true
	case "track_alpha", "track-alpha":
		return "track alpha", true
	case "track_beta", "track-beta":
		return "track beta", true
	case "track_production", "track-production":
		return "track production", true
	default:
		return "", false
	}
}
