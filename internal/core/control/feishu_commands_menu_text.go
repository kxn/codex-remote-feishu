package control

import "strings"

func buildMenuModelText(value string) (string, bool) {
	model := strings.TrimSpace(value)
	if model == "" {
		return "", false
	}
	return "/model " + model, true
}

func buildMenuReasoningText(value string) (string, bool) {
	effort := strings.ToLower(strings.TrimSpace(value))
	switch effort {
	case "low", "medium", "high", "xhigh", "clear":
		return "/reasoning " + effort, true
	default:
		return "", false
	}
}

func buildMenuAccessText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "full", "confirm", "clear":
		return "/access " + mode, true
	default:
		return "", false
	}
}

func buildMenuModeText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "normal", "vscode":
		return "/mode " + mode, true
	default:
		return "", false
	}
}

func buildMenuAutoWhipText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "on", "off":
		return "/autowhip " + mode, true
	default:
		return "", false
	}
}

func buildMenuAutoContinueText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "on", "off":
		return "/autocontinue " + mode, true
	default:
		return "", false
	}
}

func buildMenuUpgradeText(value string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "latest", "local":
		return "/upgrade " + mode, true
	case "track":
		return "/upgrade track", true
	case "track_alpha", "track-alpha":
		return "/upgrade track alpha", true
	case "track_beta", "track-beta":
		return "/upgrade track beta", true
	case "track_production", "track-production":
		return "/upgrade track production", true
	default:
		return "", false
	}
}
