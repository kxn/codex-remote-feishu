package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/upgradecontract"
)

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
	return upgradecontract.NormalizeMenuArgument(strings.TrimSpace(value))
}
