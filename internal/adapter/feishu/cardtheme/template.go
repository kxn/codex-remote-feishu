package cardtheme

import "strings"

func Template(themeKey, fallback string) string {
	key := strings.ToLower(strings.TrimSpace(themeKey))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch {
	case key == "progress":
		return "wathet"
	case key == "plan", key == "final":
		return "blue"
	case key == "success", key == "approval":
		return "green"
	case key == "error" || strings.Contains(key, "error") || strings.Contains(key, "fail") || strings.Contains(key, "reject"):
		return "red"
	default:
		return "grey"
	}
}
