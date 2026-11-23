package agentproto

import "strings"

// CodexModelGroup classifies known model names for native history compatibility.
// An empty name remains unknown; callers must not infer an automatic model.
func CodexModelGroup(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return ""
	}
	for _, prefix := range []string{"gpt", "o1", "o3", "o4", "codex-"} {
		if strings.HasPrefix(model, prefix) {
			return "gpt"
		}
	}
	return "non_gpt"
}
