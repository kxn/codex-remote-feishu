package control

import "strings"

// FeishuCardTextSection is a small UI-facing section model used by existing
// Feishu cards when the adapter should own the final markdown/plain-text split.
type FeishuCardTextSection struct {
	Label string
	Lines []string
}

func (s FeishuCardTextSection) Normalized() FeishuCardTextSection {
	lines := make([]string, 0, len(s.Lines))
	for _, line := range s.Lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return FeishuCardTextSection{
		Label: strings.TrimSpace(s.Label),
		Lines: lines,
	}
}
