package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/markdown"
)

const finalCardMarkdownTableLimit = 5

func finalReplyCardDocument(title, subtitle, body, themeKey string, extraElements []map[string]any) *cardDocument {
	components := make([]cardComponent, 0, len(extraElements)+1)
	if strings.TrimSpace(body) != "" {
		components = append(components, cardMarkdownComponent{Content: body})
	}
	for _, element := range extraElements {
		components = append(components, newRawCardComponent(element))
	}
	return newCardDocumentWithHeader(title, cardTextTagPlainText, subtitle, cardTextTagLarkMarkdown, themeKey, components...)
}

func renderFinalCardMarkdown(text string) string {
	segments := markdown.SplitFenceSegments(text)
	if len(segments) == 0 {
		return ""
	}
	var out strings.Builder
	for _, segment := range segments {
		if segment.Fenced {
			out.WriteString(segment.Text)
			continue
		}
		out.WriteString(renderFinalCardMarkdownInline(segment.Text))
	}
	return out.String()
}

func normalizeFinalCardSource(text string) string {
	segments := markdown.SplitFenceSegments(text)
	if len(segments) == 0 {
		return ""
	}
	var out strings.Builder
	tableCount := 0
	for _, segment := range segments {
		if segment.Fenced {
			out.WriteString(segment.Text)
			continue
		}
		out.WriteString(normalizeFinalCardMarkdownTables(segment.Text, &tableCount))
	}
	return out.String()
}

func normalizeFinalCardMarkdownTables(text string, tableCount *int) string {
	if text == "" {
		return ""
	}
	lines := strings.SplitAfter(text, "\n")
	if len(lines) == 0 {
		return text
	}
	var out strings.Builder
	for i := 0; i < len(lines); {
		if !startsFinalCardMarkdownTable(lines, i) {
			out.WriteString(lines[i])
			i++
			continue
		}
		end := endFinalCardMarkdownTable(lines, i)
		block := strings.Join(lines[i:end], "")
		*tableCount = *tableCount + 1
		if *tableCount <= finalCardMarkdownTableLimit {
			out.WriteString(block)
		} else {
			out.WriteString(renderOverflowFinalCardTable(block))
		}
		i = end
	}
	return out.String()
}

func startsFinalCardMarkdownTable(lines []string, index int) bool {
	if index < 0 || index+1 >= len(lines) {
		return false
	}
	return isFinalCardMarkdownTableHeaderLine(lines[index]) && isFinalCardMarkdownTableSeparatorLine(lines[index+1])
}

func endFinalCardMarkdownTable(lines []string, start int) int {
	end := start + 2
	for end < len(lines) && isFinalCardMarkdownTableRowLine(lines[end]) {
		end++
	}
	return end
}

func isFinalCardMarkdownTableHeaderLine(line string) bool {
	line = strings.TrimSpace(strings.TrimRight(line, "\n"))
	if line == "" || strings.Contains(line, "```") || strings.Contains(line, "~~~") {
		return false
	}
	return strings.Count(line, "|") >= 1
}

func isFinalCardMarkdownTableSeparatorLine(line string) bool {
	line = strings.TrimSpace(strings.TrimRight(line, "\n"))
	if line == "" || !strings.Contains(line, "|") || !strings.Contains(line, "-") {
		return false
	}
	if strings.HasPrefix(line, "|") {
		line = strings.TrimPrefix(line, "|")
	}
	if strings.HasSuffix(line, "|") {
		line = strings.TrimSuffix(line, "|")
	}
	parts := strings.Split(line, "|")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		part = strings.Trim(part, ":")
		if len(part) < 3 {
			return false
		}
		for _, ch := range part {
			if ch != '-' {
				return false
			}
		}
	}
	return true
}

func isFinalCardMarkdownTableRowLine(line string) bool {
	line = strings.TrimSpace(strings.TrimRight(line, "\n"))
	return line != "" && strings.Contains(line, "|")
}

func renderOverflowFinalCardTable(block string) string {
	hasTrailingNewline := strings.HasSuffix(block, "\n")
	block = strings.TrimRight(block, "\n")
	if block == "" {
		return ""
	}
	rendered := markdownFencedCodeBlock("text", block)
	if hasTrailingNewline {
		return rendered + "\n"
	}
	return rendered
}

func renderFinalCardMarkdownInline(text string) string {
	if text == "" {
		return ""
	}
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '`' {
			run := markdown.ConsecutiveByteRun(text, i, '`')
			close := markdown.ClosingBacktickRun(text, i+run, run)
			if close < 0 {
				out.WriteString(text[i:])
				break
			}
			out.WriteString(text[i : close+run])
			i = close + run
			continue
		}
		if text[i] == '[' {
			end, label, target, ok := markdown.ParseMarkdownLinkAt(text, i)
			if ok && shouldNeutralizeFinalMarkdownTarget(target) {
				out.WriteString(renderNeutralizedLocalMarkdownLink(label, target))
				i = end
				continue
			}
			if ok {
				out.WriteString(text[i:end])
				i = end
				continue
			}
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

func shouldNeutralizeFinalMarkdownTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">"))
	}
	if target == "" || strings.HasPrefix(target, "#") {
		return false
	}
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return false
	}
	return true
}

func renderNeutralizedLocalMarkdownLink(label, target string) string {
	label = strings.TrimSpace(label)
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "<") && strings.HasSuffix(target, ">") {
		target = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">"))
	}
	targetLiteral := markdownCodeSpan(target)
	switch {
	case label == "":
		return targetLiteral
	case label == target:
		return label
	default:
		return label + " (" + targetLiteral + ")"
	}
}

func markdownCodeSpan(text string) string {
	run := maxBacktickRun(text) + 1
	fence := strings.Repeat("`", run)
	return fence + text + fence
}

func maxBacktickRun(text string) int {
	maxRun := 0
	current := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			current++
			if current > maxRun {
				maxRun = current
			}
			continue
		}
		current = 0
	}
	return maxRun
}
