package control

import "strings"

func commandOption(commandText, menuKey, value, label, description string) FeishuCommandOption {
	return FeishuCommandOption{
		Value:       value,
		Label:       label,
		Description: description,
		CommandText: commandText + " " + value,
		MenuKey:     menuKey + "_" + value,
	}
}

func normalizeVerboseMenuArgument(suffix string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(suffix))
	switch value {
	case "quiet", "normal", "verbose", "chatty":
		return value, true
	default:
		return "", false
	}
}

func normalizePlanMenuArgument(suffix string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(suffix))
	switch value {
	case "on", "off":
		return value, true
	default:
		return "", false
	}
}
