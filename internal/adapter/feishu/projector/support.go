package projector

import (
	"html"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	cardThemeInfo  = "info"
	cardThemeError = "error"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cardPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func cardCallbackButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	buttonType = strings.TrimSpace(buttonType)
	if buttonType == "" {
		buttonType = "default"
	}
	button := map[string]any{
		"tag":      "button",
		"type":     buttonType,
		"text":     cardPlainText(label),
		"disabled": disabled,
	}
	if strings.TrimSpace(width) != "" {
		button["width"] = strings.TrimSpace(width)
	}
	if len(value) != 0 {
		button["behaviors"] = []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(value),
		}}
	}
	return button
}

func cardOpenURLButtonElement(label, buttonType, openURL string, disabled bool, width string) map[string]any {
	openURL = strings.TrimSpace(openURL)
	if openURL == "" {
		return nil
	}
	button := cardCallbackButtonElement(label, buttonType, nil, disabled, width)
	if len(button) == 0 {
		return nil
	}
	button["behaviors"] = []map[string]any{{
		"type":        "open_url",
		"default_url": openURL,
	}}
	return button
}

func cardFormSubmitButtonElement(label string, value map[string]any) map[string]any {
	button := cardFormActionButtonElement(label, "primary", value, false, "")
	if len(button) == 0 {
		return nil
	}
	return button
}

func cardFormActionButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	button := cardCallbackButtonElement(label, buttonType, value, disabled, width)
	if len(button) == 0 {
		return nil
	}
	button["name"] = "submit"
	button["form_action_type"] = "submit"
	return button
}

func cardButtonGroupElement(buttons []map[string]any) map[string]any {
	filtered := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		filtered = append(filtered, cloneCardMap(button))
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		columns := make([]map[string]any, 0, len(filtered))
		for _, button := range filtered {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "top",
				"elements":       []map[string]any{button},
			})
		}
		return map[string]any{
			"tag":                "column_set",
			"flex_mode":          "flow",
			"horizontal_spacing": "small",
			"columns":            columns,
		}
	}
}

func cardDividerElement() map[string]any {
	return map[string]any{
		"tag": "hr",
	}
}

func appendCardFooterButtonGroup(elements []map[string]any, buttons []map[string]any) []map[string]any {
	group := cardButtonGroupElement(buttons)
	if len(group) == 0 {
		return elements
	}
	if len(elements) != 0 {
		elements = append(elements, cardDividerElement())
	}
	elements = append(elements, group)
	return elements
}

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

func cloneCardMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, raw := range value {
		out[key] = cloneCardAny(raw)
	}
	return out
}

func cloneCardAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneCardMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardAny(item))
		}
		return out
	default:
		return typed
	}
}

func formatNeutralTextTag(text string) string {
	return "<text_tag color='neutral'>" + html.EscapeString(strings.TrimSpace(text)) + "</text_tag>"
}

func formatCommandTextTag(text string) string {
	text = html.EscapeString(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = restoreLiteralAmpersands(text)
	return "<text_tag color='neutral'>" + text + "</text_tag>"
}

func formatInlineCodeTextTag(text string) string {
	trimmed := strings.TrimSpace(text)
	escaped := html.EscapeString(trimmed)
	escaped = strings.ReplaceAll(escaped, "&lt;", "<")
	escaped = strings.ReplaceAll(escaped, "&gt;", ">")
	escaped = restoreLiteralAmpersands(escaped)
	escaped = strings.ReplaceAll(escaped, "&#34;", "\"")
	escaped = strings.ReplaceAll(escaped, "&#39;", "'")
	return "<text_tag color='neutral'>" + escaped + "</text_tag>"
}

func restoreLiteralAmpersands(text string) string {
	if !strings.Contains(text, "&amp;") {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for len(text) > 0 {
		if strings.HasPrefix(text, "&amp;") {
			suffix := text[len("&amp;"):]
			if startsHTMLLikeEntity(suffix) {
				out.WriteString("&amp;")
			} else {
				out.WriteByte('&')
			}
			text = suffix
			continue
		}
		out.WriteByte(text[0])
		text = text[1:]
	}
	return out.String()
}

func startsHTMLLikeEntity(text string) bool {
	if text == "" {
		return false
	}
	switch text[0] {
	case '#':
		return true
	case 'a', 'b', 'c', 'd', 'g', 'l', 'm', 'n', 'q', 'r', 't':
		return true
	default:
		return false
	}
}

func renderSystemInlineTags(text string) string {
	if !strings.Contains(text, "`") {
		return text
	}
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.Contains(line, "`") {
			continue
		}
		lines[i] = renderInlineTagsInLine(line)
	}
	return strings.Join(lines, "\n")
}

func renderInlineTagsInLine(line string) string {
	var out strings.Builder
	for len(line) > 0 {
		start := strings.IndexByte(line, '`')
		if start < 0 {
			out.WriteString(line)
			break
		}
		out.WriteString(line[:start])
		line = line[start+1:]
		end := strings.IndexByte(line, '`')
		if end < 0 {
			out.WriteByte('`')
			out.WriteString(line)
			break
		}
		token := strings.TrimSpace(line[:end])
		if token == "" {
			out.WriteString("``")
		} else {
			out.WriteString(formatInlineCodeTextTag(token))
		}
		line = line[end+1:]
	}
	return out.String()
}
