package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/texttags"
)

func targetPickerHeaderElements(stageLabel, question string) []map[string]any {
	elements := make([]map[string]any, 0, 2)
	if stageLabel = strings.TrimSpace(stageLabel); stageLabel != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": texttags.FormatNeutralTextTag(stageLabel),
		})
	}
	if question = strings.TrimSpace(question); question != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + texttags.RenderSystemInlineTags(question) + "**",
		})
	}
	return elements
}
