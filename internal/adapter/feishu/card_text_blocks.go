package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func cardPlainTextBlockElement(content string) map[string]any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": content,
		},
	}
}

func appendCardTextSections(elements []map[string]any, sections []control.FeishuCardTextSection) []map[string]any {
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		if normalized.Label != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + normalized.Label + "**",
			})
		}
		if block := cardPlainTextBlockElement(strings.Join(normalized.Lines, "\n")); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}
