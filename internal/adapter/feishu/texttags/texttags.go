package texttags

import (
	"html"
	"strings"
)

func FormatNeutralTextTag(text string) string {
	return "<text_tag color='neutral'>" + html.EscapeString(strings.TrimSpace(text)) + "</text_tag>"
}

func FormatCommandTextTag(text string) string {
	text = html.EscapeString(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = restoreLiteralAmpersands(text)
	return "<text_tag color='neutral'>" + text + "</text_tag>"
}

func FormatInlineCodeTextTag(text string) string {
	trimmed := strings.TrimSpace(text)
	escaped := html.EscapeString(trimmed)
	escaped = strings.ReplaceAll(escaped, "&lt;", "<")
	escaped = strings.ReplaceAll(escaped, "&gt;", ">")
	escaped = restoreLiteralAmpersands(escaped)
	escaped = strings.ReplaceAll(escaped, "&#34;", "\"")
	escaped = strings.ReplaceAll(escaped, "&#39;", "'")
	return "<text_tag color='neutral'>" + escaped + "</text_tag>"
}

func RenderSystemInlineTags(text string) string {
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
			out.WriteString(FormatInlineCodeTextTag(token))
		}
		line = line[end+1:]
	}
	return out.String()
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
	limit := strings.IndexByte(text, ';')
	if limit <= 0 {
		return false
	}
	token := text[:limit]
	if token == "" {
		return false
	}
	if token[0] == '#' {
		if len(token) == 1 {
			return false
		}
		for i := 1; i < len(token); i++ {
			ch := token[i]
			if i == 1 && (ch == 'x' || ch == 'X') {
				continue
			}
			if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') {
				continue
			}
			return false
		}
		return true
	}
	for i := 0; i < len(token); i++ {
		ch := token[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (i > 0 && ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}
